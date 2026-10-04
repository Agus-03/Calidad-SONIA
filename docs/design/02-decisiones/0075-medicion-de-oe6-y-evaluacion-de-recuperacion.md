# ADR-0075 — OE6 con el RAG construido, y la evaluación de recuperación diferida

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · el `not_grounded` como escalada, los agentes sin base fuera de OE6, la evaluación de recuperación diferida hasta poder pagar Voyage, el anteproyecto como documento de prueba y sin comparación de solapamiento, decididos por el grupo el 2026-10-04 |
| **Fecha** | 2026-10-04 |
| **Etapa** | Construcción — Fase 12 (conocimiento) |
| **Resuelve** | Las dos consecuencias que [ADR-0068](0068-recuperacion-y-grounding.md) dejó para la medición de OE6, y cuándo y con qué se corre la evaluación de [ADR-0065](0065-fragmentacion-de-documentos.md), regla 3 |
| **Requisitos afectados** | OE6 · F06 · F12 |
| **Enmienda** | [ADR-0024](0024-unidad-y-alcance-de-oe6.md), **§2** — *"Sin escalada" = sin transferencia de tipo `out_of_domain`* deja de alcanzar: **un bloque que el `grounding` cortó por `not_grounded` también es una escalada**, haya transferencia o no. Y el alcance, que eran las conversaciones en modo autónomo, **excluye además a los agentes sin base de conocimiento** · [ADR-0065](0065-fragmentacion-de-documentos.md), **regla 3** — la evaluación no se corre *"sobre los documentos del cooperante"* ni *"compara el solapamiento 0 con al menos un valor mayor"*: se corre con el anteproyecto como documento de prueba, sin comparación de solapamiento, y cuando se pueda pagar Voyage · [ADR-0065](0065-fragmentacion-de-documentos.md), **regla 4** — deja de aplicar: el solapamiento queda en 0, y con eso las menciones al solapamiento y al cooperante de las reglas 6 y 8 y de sus consecuencias · [ADR-0067](0067-modelo-de-embeddings-e-imagenes.md), **regla 3** — la comparación de modelos usa la misma evaluación diferida, sobre el anteproyecto |
| **Relacionada con** | [ADR-0016](0016-rastro-del-pipeline-de-respuesta.md), [ADR-0024](0024-unidad-y-alcance-de-oe6.md), [ADR-0046](0046-modo-sugerencia.md), [ADR-0065](0065-fragmentacion-de-documentos.md), [ADR-0068](0068-recuperacion-y-grounding.md), [ADR-0073](0073-busqueda-semantica.md), [ADR-0074](0074-pasos-de-recuperacion-y-grounding.md) |

---

## 1. Contexto

OE6 pide *"que el 90% de las consultas dentro del dominio del negocio sean respondidas sin escalada al operador humano"*. [ADR-0024](0024-unidad-y-alcance-de-oe6.md) definió la consulta (el bloque) y la escalada (una transferencia por `out_of_domain`). Con el RAG construido, [ADR-0068](0068-recuperacion-y-grounding.md) dejó anotados dos agujeros de esa definición:

- **Con la política `out_of_domain` apagada**, un `not_grounded` corta el turno y avisa sin transferir. No hay `transfer_events`, y OE6 lo cuenta como respondido sin escalada aunque el agente no haya contestado.
- **Un agente sin base de conocimiento no recupera** (ADR-0068, regla 10). Ningún bloque suyo puede salir `not_grounded`, y OE6 le da 100 % por construcción.

Además, [ADR-0065](0065-fragmentacion-de-documentos.md), regla 3, pide validar el chunking con una evaluación de recuperación (*recall@k*) sobre los documentos del cooperante, comparando el solapamiento 0 con un valor mayor. Esa evaluación también es la que calibra el umbral y `top_k` ([ADR-0074](0074-pasos-de-recuperacion-y-grounding.md)). Para que dé resultados que signifiquen algo tiene que correr con Voyage real, y hoy no se puede pagar.

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **Un bloque que el `grounding` cortó por `not_grounded` es una escalada para OE6**, haya transferencia o no. El agente no contestó, y la política sólo decide si una persona se entera |
| 2 | **Los agentes sin base de conocimiento quedan fuera de la medición de OE6.** Se toma como agente con base el que, al cerrarse el bloque, tenía al menos un documento cargado y no dado de baja |
| 3 | **La evaluación de recuperación se difiere** hasta que se pueda pagar Voyage. Se construye y se corre entonces, con embeddings reales: con el doble los resultados no significan nada. Hasta ese momento el umbral queda en 0,5 y `top_k` en 3 |
| 4 | **El documento de prueba es el anteproyecto**, y las preguntas salen de él: unas que el anteproyecto responde y otras que no, que son las que calibran el umbral ([ADR-0068](0068-recuperacion-y-grounding.md), regla 5). No hace falta que sea un documento del cooperante, porque la evaluación prueba la recuperación y no el negocio |
| 5 | **No hay solapamiento, y por lo tanto no se compara.** `overlap_tokens` queda en 0 y la evaluación no lo varía |
| 6 | **Los criterios se fijan al armar las pruebas, antes de correrlas**: cómo se elige el umbral con los datos, y cuánta mejora de *recall@k* justifica cambiar un parámetro del chunking. Los parámetros siguen en `rules/`, versionados |

## 3. Justificación

- **El `not_grounded` es una escalada porque el agente no contestó**, que es lo que OE6 mide. Que la política esté apagada decide si una persona se entera, no si el agente respondió. Sin esta regla, apagar la política mejoraría OE6.
- **Los agentes sin base salen porque no tienen cobertura de RAG que medir.** Medirlos sería sumar al numerador turnos que nunca pudieron escalar, y OE6 subiría por agregar agentes sin documentos.
- **"Tenía un documento cargado" y no "tenía un documento `indexed`"** porque el estado de un documento en un momento pasado no se guarda: `documents.status` es el de hoy. Es la aproximación que se puede escribir contra el modelo.
- **Diferir la evaluación porque no se puede pagar**, y correrla con el doble daría números que no dicen nada de voyage-4.
- **El anteproyecto porque es un PDF real con texto, y está a mano.** Sirve además para ver qué hace `pypdf` con los saltos de línea, la sospecha que quedó abierta en la revisión del `worker`.
- **Sin solapamiento porque lo decidió el grupo.** Los parámetros siguen en `rules/` (ADR-0065, regla 9), así que si algún día se quiere, se agrega y se versiona.

## 4. Consecuencias

### Positivas

- OE6 deja de mejorar al apagar la política `out_of_domain` o al agregar agentes sin documentos.
- La consulta de OE6 de [05-consultas-de-metricas.sql](../05-consultas-de-metricas.sql) se corre contra el DDL real en un test de integración, que la lee del documento mismo: si deja de ser ejecutable, o deja de contar lo que dice esta regla, falla.

### Negativas / costos asumidos

- **El sistema opera con un umbral y un `top_k` sin calibrar** hasta que se corra la evaluación. Es el costo que ADR-0068 ya anotó, y dura más.
- **El chunking nunca se valida con datos antes de operar.** Los 500 tokens, la equivalencia de 4 caracteres por token y los separadores son los de partida.
- **Un corpus de unas 20 páginas hace fácil la *recall@k***: con pocos trozos, casi cualquier búsqueda trae el bueno entre los primeros, y el número sale más alto que con una base real.
- **El anteproyecto no se parece a los documentos de una inmobiliaria**, ni sus preguntas a las de una clienta. Una buena *recall@k* sobre la tesina no garantiza lo mismo sobre fichas de propiedades o listas de precios.
- **Sin comparación de solapamiento, no se mide lo que se pierde en los cortes.** Una respuesta partida entre dos trozos se recupera a medias, y nada lo cuantifica.
- **"Agente con base" es una aproximación.** Un documento que quedó `failed` cuenta como cargado, y su agente entra a la medición aunque no haya podido recuperar nada de él.
- **OE6 deja de ser comparable** con cualquier medición hecha con la definición de ADR-0024 sola. Como todavía no hay datos de operación, no se pierde ninguna.
- **Un bloque `not_grounded` sin transferencia cuenta como escalada aunque nadie lo haya visto**: la medición dice que fue a una persona, y con la política apagada no fue.
- **Contradice dos reglas aceptadas de ADR-0065**, que pedían el documento del cooperante y la comparación de solapamiento. Se registran como enmienda.

### Lo que queda abierto

| Qué | Estado |
|---|---|
| **Construir y correr la evaluación de recuperación** | Cuando se pueda pagar Voyage, en `src/test/evals/` |
| **El conjunto de preguntas sobre el anteproyecto** | Lo escribe el grupo al armar la evaluación |
| **Los criterios de la regla 6** | Se fijan al armar la evaluación, antes de correrla |

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Contar sólo las transferencias, como ADR-0024** | Con la política apagada, un agente que no contesta nada mejora OE6 |
| **Medir también a los agentes sin base** | Dan 100 % por construcción y arrastran el promedio |
| **Correr la evaluación con el doble de embeddings** | Gratis, pero sus números no dicen nada de voyage-4 |
| **Esperar documentos del cooperante** | La evaluación prueba la recuperación, y el anteproyecto alcanza para eso |
| **Comparar el solapamiento 0 con 50 tokens** | Obliga a implementar el solapamiento, y el grupo decidió no tenerlo |
