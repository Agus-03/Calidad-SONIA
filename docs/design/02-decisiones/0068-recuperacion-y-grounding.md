# ADR-0068 — La recuperación y el grounding: con qué se busca, cuándo hay respaldo y qué pasa cuando no lo hay

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · buscar con el bloque redactado y el resumen del contexto vigente, decidir el grounding previo con un umbral de similitud, colgar `retrieval_logs` del paso `retrieval`, y que `not_grounded` escriba la señal `out_of_domain`, avise a la clienta con un texto fijo y transfiera, decididos por el grupo el 2026-10-03 · **el método del grounding de salida (O4) queda para la fase de evaluación** |
| **Fecha** | 2026-10-03 |
| **Etapa** | Construcción — Fase 12 (conocimiento) |
| **Resuelve** | O5 y O9 de [ADR-0042](0042-pipeline-de-decision-del-turno.md), y el pendiente de su §1.1. Fija cuándo se construye O4 |
| **Requisitos afectados** | F06 · F12 · OE6 |
| **Enmienda** | [ADR-0042](0042-pipeline-de-decision-del-turno.md), **O4** — *"Se construye: Fase 12"* deja de ser cierto: el grounding de salida se construye en la fase de evaluación, junto con la elección de su método (regla 11) · [ADR-0042](0042-pipeline-de-decision-del-turno.md), **§2.4, regla 3** — *"Sólo el `triage` delega"* deja de ser cierta: el corte del `grounding` también delega (regla 7) · El vínculo de `retrieval_logs`: **`generation_id` pasa a ser `pipeline_step_id`** (regla 6), en [DER/entidades.md](../DER/entidades.md), [DER/relaciones.md](../DER/relaciones.md) §3 y §4, [DER/guia-de-dibujo.md](../DER/guia-de-dibujo.md), [der-logico-scaffold.mmd](../../diagrams/der-logico-scaffold.mmd), el diagnóstico de OE6 de [05-consultas-de-metricas.sql](../05-consultas-de-metricas.sql) y §4.8 de [03-capa-prompts.md](../03-capa-prompts.md) |
| **Relacionada con** | [ADR-0010](0010-qdrant-como-base-vectorial.md), [ADR-0016](0016-rastro-del-pipeline-de-respuesta.md), [ADR-0024](0024-unidad-y-alcance-de-oe6.md), [ADR-0033](0033-secretos-de-bandejas-nullables-y-congelamiento-de-0001.md), [ADR-0042](0042-pipeline-de-decision-del-turno.md), [ADR-0045](0045-senales-de-transferencia-y-desactivacion-del-agente-ia.md), [ADR-0046](0046-modo-sugerencia.md), [ADR-0048](0048-contexto-dinamico-quien-lo-escribe-y-como-se-reescribe.md), [ADR-0065](0065-fragmentacion-de-documentos.md), [ADR-0067](0067-modelo-de-embeddings-e-imagenes.md) |

---

## 1. Contexto

F06 pide *"búsqueda semántica para enriquecer respuestas"* y F12, detectar *"consultas fuera del dominio RAG"* como señal de transferencia. [ADR-0016](0016-rastro-del-pipeline-de-respuesta.md) declaró los pasos `retrieval` y `grounding`, y [ADR-0042](0042-pipeline-de-decision-del-turno.md) los difirió a esta fase con cuatro puntos sin cerrar:

- **O4:** el grounding de salida, contra la alucinación. Está decidido que existe, y no cómo decide.
- **O5:** qué hace `not_grounded`. Está decidido que transfiere por una señal de F12, y no por cuál ni qué lee la clienta.
- **O9:** `retrieval_logs.generation_id` es FK a `generations`, así que un turno que recupera y corta antes de generar no tiene dónde registrar lo que recuperó.
- **§1.1:** F12 habla del *"dominio RAG"* y OE6 del *"dominio del negocio"*. ADR-0042 eligió el segundo para el `triage` y dejó dicho que, con el RAG construido, había que decidir qué pasa con el primero.

**Falta además una decisión que ninguno de los cuatro nombra: con qué texto se busca.** En WhatsApp una clienta escribe *"¿y cuánto sale?"* después de haber preguntado por un departamento. Esa frase sola no se parece a ningún trozo de ningún documento, y la búsqueda no encuentra nada aunque la respuesta esté cargada.

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **`retrieval` y `grounding` corren en el tramo de entrada, después del `triage` y antes de la generación.** Con eso el rastro de un turno que no corta tiene ocho posiciones: `blocklist`, `guardrail`, `triage`, `retrieval`, `grounding`, `generation`, `blocklist`, `guardrail` |
| 2 | **Se busca con el texto del bloque, ya redactado, precedido por el `summary` del contexto vigente** (`conversation_contexts`, el que dejó el turno anterior). En el primer turno de una conversación no hay resumen, y se busca sólo con el bloque. **No se le pide a un LLM que reformule la pregunta** |
| 3 | **`retrieval` trae los `top_k` trozos más parecidos de la colección del agente ([ADR-0010](0010-qdrant-como-base-vectorial.md)), sin umbral, y los registra todos** en `retrieval_logs` con su similitud y su posición. Sólo considera trozos activos ([DER/relaciones.md](../DER/relaciones.md) §9). Su `outcome` es `hit` si volvió algún trozo y `empty` si no volvió ninguno |
| 4 | **El `grounding` previo decide con un umbral sobre la similitud**: si ningún trozo lo alcanza, el veredicto es `not_grounded`. Su `score` es la mayor similitud. **A la generación llegan sólo los trozos que alcanzan el umbral** |
| 5 | **`top_k` y el umbral viven en `rules/retrieval.yaml`**, versionados por la regla 9 de [08-structure-rules](../08-structure-rules.md). La `version` de las filas de los dos pasos es la de ese archivo, y los dos valores van en el `detail_json` del paso `retrieval`, como fijó [ADR-0016](0016-rastro-del-pipeline-de-respuesta.md) §2. **Los valida la evaluación de recuperación de [ADR-0065](0065-fragmentacion-de-documentos.md), regla 3**, que para el umbral necesita también preguntas cuya respuesta no está en los documentos |
| 6 | **`retrieval_logs` cuelga de la fila del paso `retrieval`** (`pipeline_step_id`, FK a `pipeline_steps`) y no de `generations`. Se cambia la columna editando `0001` mientras no haya una WABA real ([ADR-0033](0033-secretos-de-bandejas-nullables-y-congelamiento-de-0001.md)) |
| 7 | **`not_grounded` corta el turno y escribe una fila de `signal_evaluations` con la señal `out_of_domain`.** Su `score` es `1 −` la mayor similitud y su `threshold_used` es `1 −` el umbral, para que dispare en la misma dirección que las demás señales: más alto, más lejos de los documentos. Su `classifier_version` es `retrieval<version>` —hoy `retrieval1`—, que la distingue de la fila que el `triage` ya escribió para la misma señal y no choca con ella en el índice único. **La gobierna la misma política `out_of_domain` del agente**: si está encendida, el núcleo escribe `transfer_events` y apaga el modo de la conversación, como en [ADR-0045](0045-senales-de-transferencia-y-desactivacion-del-agente-ia.md) |
| 8 | **La clienta recibe un texto fijo, además de la transferencia.** El texto vive en `rules/notices.yaml`, entrada `grounding`, y no promete tiempos. En modo `suggestion` no se envía, como cualquier texto fijo ([ADR-0046](0046-modo-sugerencia.md)) |
| 9 | **Un saludo no recupera.** Si el `triage` marca `detail_json.greeting`, `retrieval` y `grounding` no corren y no escriben filas |
| 10 | **Un agente sin base de conocimiento, o sin ningún documento en `ready`, no recupera**: los dos pasos no corren y el turno sigue como antes de esta fase. F06 es opcional por agente ([DER/relaciones.md](../DER/relaciones.md) §4: `ai_agents` 1 — 0..1 `knowledge_bases`) |
| 11 | **El grounding de salida (O4) se construye en la fase de evaluación**, cuando se elija con datos cómo decide: un modelo que juzgue la respuesta contra los trozos o una comparación sin LLM. Hasta entonces no hay fila de `grounding` con `direction = 'outbound'` |
| 12 | **No hay reranking.** [ADR-0016](0016-rastro-del-pipeline-de-respuesta.md) lo dejó *"si hace falta"*, y lo mostraría la evaluación de la regla 5. `reranking.py` no se crea |

## 3. Justificación

- **Buscar con el resumen porque es el contexto que ya existe y ya se paga.** El reescritor de [ADR-0048](0048-contexto-dinamico-quien-lo-escribe-y-como-se-reescribe.md) corre en cada turno generado, así que el resumen está en la base antes de recuperar. Con él, *"¿y cuánto sale?"* se busca junto a *"busca un departamento de 2 ambientes en Jesús María"*. Reformular con un LLM resolvería lo mismo con una llamada más sobre el camino feliz, que ADR-0042 ya anotó como el costo del `triage`.
- **Se busca sobre el texto redactado** porque la Regla 3 de [03-capa-prompts.md](../03-capa-prompts.md) pone la consulta del RAG entre lo que trabaja sólo sobre texto ya redactado, y porque el texto de búsqueda sale hacia el proveedor de embeddings.
- **`retrieval` no filtra por umbral para que la evidencia sirva para moverlo.** Es H-23, la razón de ser de ADR-0016: si el paso guardara sólo lo que pasó el corte, una evaluación no podría preguntar *"¿qué habría pasado con un umbral más bajo?"* sin volver a correr la búsqueda. Con todas las similitudes registradas, barrer el umbral es una consulta.
- **Un umbral y no un LLM para el grounding previo** porque es gratis, no agrega latencia y da siempre el mismo veredicto para la misma búsqueda. Lo que no ve —un trozo parecido que no responde— es justamente lo que tiene que ver el grounding de salida.
- **Colgar `retrieval_logs` de `pipeline_steps` por el mismo argumento con el que ADR-0016 colgó `pipeline_steps` de `message_batches`**: *"colgarla de la respuesta sería no poder registrar precisamente los casos en que no hubo respuesta"*. La fila del paso `retrieval` existe en todo turno que recuperó, haya generado o no, y desde ella se llega al bloque y a la generación.
- **`out_of_domain` porque es la letra de F12**: *"consultas fuera del dominio RAG"* es exactamente un `not_grounded`. Y es lo que [ADR-0024](0024-unidad-y-alcance-de-oe6.md) necesita: *"sin escalada"* es *"sin transferencia de tipo `out_of_domain`"* porque OE6 mide la cobertura del RAG. Con esta regla, **OE6 empieza a discriminar**, que es lo que ADR-0042 anunció que pasaría cuando existiera O5.
- **Con eso se separan los dos dominios de §1.1 sin tocar la enumeración.** El `triage` juzga el dominio del negocio y da el denominador de OE6; el `grounding` juzga el del RAG y da la escalada. La señal es la misma porque F12 nombra una sola, y la distingue la `classifier_version` del paso que la escribió.
- **El texto fijo porque el silencio consume la ventana de 24 horas** sin que la clienta sepa si su mensaje llegó, que es el argumento de O2. La diferencia con el corte del `triage`, que es mudo (O1c), es lo que la clienta preguntó: allá, algo que este negocio no atiende; acá, algo que el negocio atiende y el agente no supo contestar.
- **Un saludo no recupera porque no tiene nada que buscar.** Un *"hola"* no se parece a ningún trozo y saldría `not_grounded`: cada saludo terminaría en la bandeja de un vendedor, que es el costo que O1b eliminó.
- **Un agente sin base no recupera porque transferirlo todo apagaría al agente.** Un administrador que no usa F06 tendría un bot que pasa cada pregunta a una persona.
- **El método del grounding de salida se elige en la fase de evaluación** porque elegirlo ahora es elegirlo de memoria. Lo que distingue a un juez LLM de una comparación sin LLM es cuántas alucinaciones atrapa y cuántas respuestas buenas rechaza, y eso se mide con los conjuntos que esa fase construye.

## 4. Consecuencias

### Positivas

- **OE6 deja de dar 100 % por construcción**: una pregunta del negocio que los documentos no responden ahora es una escalada.
- **Todo turno que recuperó queda auditable**, también el que cortó antes de generar, que es el que más interesa mirar.
- **El umbral se puede barrer offline** con lo que ya está en `retrieval_logs`.
- **La fase no agrega ninguna llamada al LLM por turno**: el texto de búsqueda reusa el resumen y el grounding previo es una comparación.
- **F12 cubre por fin la letra de su tercera señal**, sin un valor nuevo en `TransferSignalType` ni migración de la enumeración.

### Negativas / costos asumidos

- **Hasta la fase de evaluación no hay nada que detecte una alucinación.** Un trozo que pasó el umbral y una respuesta que inventa un precio que no está en él llegan a la clienta. Es el riesgo que O4 quería cubrir, y queda abierto durante toda la operación anterior a esa fase.
- **El umbral es provisional hasta la evaluación de la regla 5**, y equivocarse duele en las dos direcciones: bajo, deja pasar trozos que no responden; alto, transfiere preguntas que los documentos sí responden. Ninguno de los dos errores se ve sin la evaluación.
- **Parecido no es respuesta.** Un trozo sobre el mismo departamento puede superar el umbral sin decir el dato que se preguntó, y el grounding previo lo da por bueno.
- **Todo lo del negocio que no está en los documentos va a una persona.** *"¿Puedo ir a verlo el martes?"* no está en ningún PDF. Es lo que ADR-0024 quiere medir, pero significa que la base tiene que cubrir las preguntas frecuentes para que el agente sirva.
- **El resumen es del turno anterior y puede arrastrar el tema viejo.** Si la clienta cambia de tema, la búsqueda se sesga hacia lo que hablaba antes, hasta que el reescritor lo actualice.
- **Cada bloque que recupera tiene dos filas `out_of_domain` en `signal_evaluations`**: la del `triage`, que no disparó, y la del `grounding`. Una consulta que cuente evaluaciones de esa señal por bloque cuenta dos, y un barrido de umbral que no separe por `classifier_version` mezcla una confianza del clasificador con una distancia a los documentos, que no están en la misma escala.
- **El prefijo `retrieval` de `classifier_version` es una convención en un `varchar`**, como el `1+profanity1` de ADR-0042: nada en el esquema la hace cumplir.
- **Los dos cortes que delegan le dicen cosas distintas a la clienta.** El del `triage` calla y el del `grounding` avisa. El argumento de O1c —un texto automático compite con la respuesta de la persona que está atendiendo— vale también acá, y el grupo eligió avisar igual. El texto sin promesa de tiempo lo atenúa, no lo elimina.
- **La política `out_of_domain` es una sola para los dos pasos.** No se puede transferir lo que el RAG no responde sin transferir también lo que el `triage` juzga fuera del negocio, ni al revés.
- **Con la política apagada, `not_grounded` corta y avisa sin transferir, y OE6 lo cuenta como respondido sin escalada** aunque el agente no haya contestado. Es el mismo agujero que ya tiene el corte del `triage`, y se agranda.
- **La trazabilidad de OE2 suma un salto.** Para saber qué trozos usó una generación hay que pasar por el bloque y la fila del paso `retrieval`: la cadena de cuatro saltos de [DER/relaciones.md](../DER/relaciones.md) §3 pasa a cinco.
- **Para un agente sin base, OE6 vuelve a dar 100 % por construcción.** Ningún bloque puede escalar por `not_grounded`, y la medición tiene que excluirlo o declararlo.
- **Un falso `greeting` del `triage` deja una pregunta sin recuperar**: el agente la contesta sin documentos y nada lo marca.
- **Cambia una columna de `0001`**, y con ella el código y los tests que escriben `retrieval_logs`.

### Lo que queda abierto

| Qué | Estado |
|---|---|
| **El método del grounding de salida** (regla 11) | La fase de evaluación |
| **Los valores iniciales de `top_k` y del umbral** | La evaluación de [ADR-0065](0065-fragmentacion-de-documentos.md), regla 3, con preguntas sin respuesta en los documentos |
| **La rama del corte del `grounding` en [07-secuencia-del-turno.md](../07-secuencia-del-turno.md) §4** | Se dibuja al construir los dos pasos |

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Buscar sólo con el último bloque** | *"¿Y cuánto sale?"* no encuentra nada aunque la respuesta esté cargada, y el agente transferiría preguntas que sabe responder |
| **Que un LLM reformule la pregunta antes de buscar** | Resuelve lo mismo que el resumen con una llamada más por turno, sobre el camino feliz |
| **Filtrar por umbral dentro de `retrieval`** | `retrieval_logs` guardaría sólo lo que pasó el corte, y el umbral no se podría barrer sin volver a buscar: es H-23 |
| **Que un LLM decida el grounding previo** | Una llamada más por turno y un veredicto que puede cambiar para la misma búsqueda. Lo que vería de más le toca al grounding de salida |
| **Dejar `retrieval_logs.generation_id` y hacerlo nullable** | Un turno cortado registraría trozos sueltos, sin nada que los ate a su turno salvo la fecha |
| **Que `not_grounded` escriba `no_resolution`** | Deja la letra de F12 sin la señal que nombra, mezcla el RAG con una racha de turnos sin resolver ([ADR-0045](0045-senales-de-transferencia-y-desactivacion-del-agente-ia.md)) y no descuenta en OE6, que es lo que ADR-0024 quiere medir |
| **Un valor nuevo en `TransferSignalType`** | F12 ya nombra la señal, y un valor nuevo obliga a tocar la enumeración, `0001` y la consulta de OE6 para decir lo mismo |
| **Transferir sin avisar**, como el corte del `triage` | La clienta queda en silencio mientras corre la ventana de 24 horas (O2) |
| **Avisar sin transferir** | La pregunta queda sin respuesta, nadie se entera y OE6 la cuenta como respondida |
| **Elegir ahora el método del grounding de salida** | Sería elegirlo de memoria: lo que los distingue se mide con los conjuntos de la fase de evaluación |
| **Recuperar también en los saludos** | Cada saludo saldría `not_grounded` y terminaría en la bandeja de un vendedor |
| **Que un agente sin base transfiera todo** | Apaga al agente para quien no usa F06, que es opcional |
