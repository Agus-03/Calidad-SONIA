# ADR-0073 — La búsqueda semántica: cuántos trozos, de qué documentos y qué se descarta antes de buscar

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · `top_k` provisorio en 3 y buscar sólo en documentos `indexed`, decididos por el grupo el 2026-10-04 · las reglas 3 a 7 son decisiones de construcción, tomadas al escribir `knowledge/search.py` |
| **Fecha** | 2026-10-04 |
| **Etapa** | Construcción — Fase 12 (conocimiento) |
| **Resuelve** | El valor inicial de `top_k`, que [ADR-0068](0068-recuperacion-y-grounding.md) dejó abierto, y qué es un *"trozo activo"* (regla 3 de ADR-0068) para la búsqueda |
| **Requisitos afectados** | F06 · F12 · OE6 |
| **Relacionada con** | [ADR-0010](0010-qdrant-como-base-vectorial.md), [ADR-0064](0064-ingesta-de-documentos.md), [ADR-0065](0065-fragmentacion-de-documentos.md), [ADR-0067](0067-modelo-de-embeddings-e-imagenes.md), [ADR-0068](0068-recuperacion-y-grounding.md), [ADR-0071](0071-worker-de-ingesta.md), [ADR-0072](0072-qdrant-en-el-compose.md) |

---

## 1. Contexto

F06 pide *"búsqueda semántica para enriquecer respuestas"*. [ADR-0068](0068-recuperacion-y-grounding.md) fijó qué hace el paso `retrieval`: traer los `top_k` trozos más parecidos de la colección del agente, sin umbral, sólo trozos activos, y no recuperar si el agente no tiene base o no tiene ningún documento en `indexed`. Dejó abiertos los valores de `top_k` y del umbral hasta la evaluación de [ADR-0065](0065-fragmentacion-de-documentos.md), regla 3.

Para construir la búsqueda faltaban tres cosas:

- **Un `top_k` con el que arrancar**, aunque la evaluación lo cambie después.
- **Qué documentos cuentan.** Un trozo puede estar vivo en `document_chunks` y su documento no estar en `indexed`: el que se está reprocesando, o el que falló después de subir sus vectores (ADR-0064, regla 7).
- **Qué pasa con los vectores que no se pueden comparar.** Si el modelo de embeddings cambia sin reindexar (ADR-0064, regla 11), la colección mezcla vectores de dos modelos, y Qdrant no lo detecta si la dimensión es la misma.

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **`top_k` arranca en 3**, en `rules/retrieval.yaml` versión 1. Es provisorio: lo valida la evaluación de [ADR-0065](0065-fragmentacion-de-documentos.md), regla 3. El umbral entra al archivo con el paso `grounding` |
| 2 | **Se busca sólo en documentos `indexed`**, no dados de baja y de una base no dada de baja. Es lo que la búsqueda entiende por *"trozo activo"*: el trozo no retirado de un documento en `indexed` |
| 3 | **El filtro se aplica en Qdrant, antes de elegir los `top_k`**: la búsqueda le pide a Qdrant los más parecidos *entre los documentos buscables*, por el `document_id` del *payload*. Un documento que no cuenta no ocupa lugares |
| 4 | **El texto se lee de `document_chunks` y se vuelve a verificar ahí.** Un punto cuyo trozo se retiró, o cuyo documento dejó `indexed` entre las dos lecturas, se descarta y no se reemplaza. El `rank` cuenta desde 1, en el orden de Qdrant, sobre lo que queda |
| 5 | **Un documento con trozos vivos de otro modelo de embeddings queda afuera**, y la búsqueda lo registra como advertencia. Si todos quedan afuera, la búsqueda corre y no encuentra nada (`outcome = empty`): no es *"el agente no recupera"* |
| 6 | **La pregunta se vectoriza como consulta** (`input_type = query`). Un texto vacío no se manda al proveedor y no encuentra nada |
| 7 | **La búsqueda devuelve tres cosas distintas**: nada (`None`) cuando el agente no recupera (regla 10 de ADR-0068); una lista vacía cuando buscó y no encontró; y los trozos con su similitud y su posición. Las fallas del proveedor y de Qdrant no se esconden: qué hace el turno con ellas lo decide el paso `retrieval` |

## 3. Justificación

- **Tres porque lo eligió el grupo**, y porque cada trozo que llega a la generación es prompt que se paga en cada turno. Con trozos de hasta 500 tokens, tres son unos 1500 tokens de contexto, y la evaluación dirá si alcanzan.
- **Sólo `indexed` porque es lo que razona la regla 10 de ADR-0068**, y porque un documento que se está reprocesando tiene, durante un rato, trozos nuevos sin todos sus vectores. Buscar ahí sería buscar en algo a medio escribir.
- **Filtrar en Qdrant y no después** porque filtrar después hace que un documento que no cuenta ocupe lugares de los `top_k`: con tres lugares, un solo documento a medio reprocesar podría dejar la búsqueda vacía. Filtrar antes da siempre los tres mejores de lo que sí cuenta.
- **Volver a verificar en la base** porque Qdrant y PostgreSQL no se escriben en la misma transacción (ADR-0064, regla 7), y la base es la fuente de verdad (ADR-0010). El texto tiene que salir de la base de todos modos: Qdrant no guarda copia (ADR-0071).
- **Dejar afuera los vectores de otro modelo** porque compararlos da similitudes sin sentido, y el umbral del `grounding` las tomaría por buenas. Registrarlo como advertencia hace visible un cambio de modelo sin reindexar.
- **Que todos afuera sea `empty` y no `None`** porque el agente sí tiene una base: tratarlo como un agente sin base lo haría contestar sin documentos sin que nada lo marque. Con `empty`, el `grounding` da `not_grounded` y la pregunta va a una persona.

## 4. Consecuencias

### Positivas

- La búsqueda nunca devuelve un trozo de un documento que no está en `indexed`, ni de uno dado de baja que el barrido todavía no limpió.
- Un cambio de modelo sin reindexar se ve en los logs en lugar de degradar las respuestas en silencio.
- Un agente sin base ni siquiera manda la pregunta al proveedor de embeddings.

### Negativas / costos asumidos

- **Tres trozos pueden no alcanzar.** Una pregunta cuya respuesta cruza más de tres trozos llega a la generación incompleta, y sin solapamiento (ADR-0065) es más probable. Lo mide la evaluación.
- **Un documento que se reprocesa desaparece de la búsqueda mientras dura**, y uno que falló después de subir sus vectores pierde también sus trozos válidos. Las preguntas que sólo él respondía terminan en `not_grounded` y van a una persona.
- **Un cambio de modelo sin reindexar transfiere todo.** Si todos los documentos quedaron con el modelo viejo, cada pregunta sale `not_grounded` hasta que se reindexe.
- **El filtro de Qdrant no tiene índice.** La colección se crea sin índice sobre `document_id` en el *payload*, así que Qdrant filtra recorriendo. Con bases de hasta 100 MB no se nota; con muchas más, sí. Crear el índice es un cambio del `worker`.
- **Un punto descartado en la segunda lectura no se reemplaza**: en esa ventana vuelven menos de `top_k`.
- **El `rank` no es la posición cruda de Qdrant** cuando se descartó un punto: es la posición entre los que quedaron.
- **La búsqueda suma latencia al turno**: dos lecturas de la base, una llamada al proveedor de embeddings y una a Qdrant, dentro de los 10 segundos que espera el núcleo.

### Lo que queda abierto

| Qué | Estado |
|---|---|
| **El umbral del `grounding`** | Se fija con el paso `grounding` y se valida en la evaluación de ADR-0065, regla 3 |
| **Los *timeouts* del proveedor y de Qdrant dentro del turno, y qué hace el turno si fallan** | Se deciden con el paso `retrieval`, que es el que llama a la búsqueda |

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **`top_k` 5** | Más contexto por turno a pagar antes de saber si hace falta. El grupo eligió 3 |
| **Buscar en cualquier documento no dado de baja** | Incluye el documento que se está reprocesando, con trozos a medio vectorizar, y el que falló, contra lo que razona la regla 10 de ADR-0068 |
| **Filtrar después de buscar** | Los documentos que no cuentan ocupan lugares de los `top_k`, y la búsqueda puede volver vacía aunque haya trozos buenos |
| **Pedirle a Qdrant más de `top_k` y recortar** | Cubre el mismo caso que el filtro con un número arbitrario de sobra, que puede no alcanzar |
| **Buscar también en los documentos de otro modelo** | Compara vectores que no son comparables, y el `grounding` toma por buenas similitudes sin sentido |
| **Tratar *"todos de otro modelo"* como *"el agente no recupera"*** | El agente contesta sin documentos y nada lo marca |
