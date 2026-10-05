# ADR-0074 — Los pasos `retrieval` y `grounding` en el turno: el umbral, los tiempos y qué pasa cuando la búsqueda falla

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · el umbral provisorio en 0,5, fallar el paso si Voyage o Qdrant fallan, 5 s para cada una y 15 s para el turno entero, decididos por el grupo el 2026-10-04 · las reglas 4 a 11 son decisiones de construcción, tomadas al escribir `pipeline/retrieval.py` y `pipeline/grounding.py` · **reglas 2 y 5 enmendadas el 2026-10-05 por [ADR-0077](0077-busqueda-fallida-y-arranque-sin-busqueda.md)**: la búsqueda que falla deja su fila `retrieval` con `outcome = 'failed'`, y fuera de `development` y `test` el cerebro no arranca sin búsqueda |
| **Fecha** | 2026-10-04 |
| **Etapa** | Construcción — Fase 12 (conocimiento) |
| **Resuelve** | Lo que [ADR-0068](0068-recuperacion-y-grounding.md) y [ADR-0073](0073-busqueda-semantica.md) dejaron para el paso `retrieval`: el umbral inicial, los tiempos de la búsqueda dentro del turno y qué hace el turno si la búsqueda falla. Dibuja la rama del corte del `grounding` en [07-secuencia-del-turno.md](../07-secuencia-del-turno.md) §4 B.3 |
| **Requisitos afectados** | F06 · F12 · OE6 |
| **Enmienda** | [07-secuencia-del-turno.md](../07-secuencia-del-turno.md), **§7, decisión 3** — *"Tres"* reintentos deja de ser lo que pasa: con 15 s por intento y un presupuesto de 30 s (`TURN_BUDGET_MS`, ADR-0029) entran dos. La variable sigue en 3 · [07-secuencia-del-turno.md](../07-secuencia-del-turno.md), **§5** — el presupuesto del turno deja de estar *"en el orden de los diez segundos"*, por lo mismo |
| **Relacionada con** | [ADR-0016](0016-rastro-del-pipeline-de-respuesta.md), [ADR-0020](0020-idempotencia-de-los-mensajes-salientes.md), [ADR-0029](0029-el-acuse-del-webhook-no-espera-al-turno.md), [ADR-0042](0042-pipeline-de-decision-del-turno.md), [ADR-0045](0045-senales-de-transferencia-y-desactivacion-del-agente-ia.md), [ADR-0046](0046-modo-sugerencia.md), [ADR-0068](0068-recuperacion-y-grounding.md), [ADR-0073](0073-busqueda-semantica.md) |

---

## 1. Contexto

[ADR-0068](0068-recuperacion-y-grounding.md) fijó qué hacen los dos pasos: `retrieval` busca con el resumen y el bloque redactado y registra todo lo que encontró, y el `grounding` compara la mayor similitud con un umbral y, si no lo alcanza, corta el turno con la señal `out_of_domain`, un texto fijo y una transferencia. [ADR-0073](0073-busqueda-semantica.md) construyó la búsqueda. Para meter los dos pasos en el turno faltaban tres cosas:

- **El umbral con el que arrancar**, que ADR-0068 dejó para la evaluación de recuperación.
- **Qué hace el turno si Voyage o Qdrant no responden.** ADR-0073 decidió que esas fallas no se esconden, y dejó para este paso qué hace el turno con ellas.
- **Cuánto se espera.** El núcleo esperaba 10 s por el turno entero, y la búsqueda agrega dos llamadas de red antes de generar.

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **El umbral arranca en 0,5** de similitud coseno, en `rules/retrieval.yaml`. Es provisorio: lo calibra la evaluación de recuperación |
| 2 | **Si Voyage o Qdrant fallan, falla el paso y con él el turno**, como cuando falla el proveedor del `triage`: el cerebro responde `outcome = 'failed'` y conserva los pasos que sí decidieron. No se sigue sin documentos ni se transfiere *(Enmendada por [ADR-0077](0077-busqueda-fallida-y-arranque-sin-busqueda.md), regla 1: también escribe la fila de `retrieval`, con `outcome = 'failed'`, para que OE6 cuente el bloque como no respondido.)* |
| 3 | **El turno espera 5 s a Voyage y 5 s a Qdrant** (`RETRIEVAL_TIMEOUT_SECONDS`), **y el núcleo espera 15 s por el turno entero** (`BRAIN_TIMEOUT_MS`, antes 10 s). El `worker` sigue con sus 30 s. Cada tope es de la llamada entera, no de una fase de ella: los dos adaptadores envuelven la llamada en `asyncio.timeout`, como los del LLM |
| 4 | **Los dos pasos corren después de evaluar las señales del `triage`, fuera de `runner.INBOUND`.** Siguen siendo del tramo de entrada (ADR-0068, regla 1), pero no son entradas de esa tupla. Las posiciones siguen el orden de ADR-0068, regla 1: `triage` en la 3, `retrieval` en la 4 y `grounding` en la 5 |
| 5 | **Un agente sin base, un saludo o un cerebro sin búsqueda configurada no recuperan**: los dos pasos no escriben filas (ADR-0068, reglas 9 y 10) *(Enmendada por [ADR-0077](0077-busqueda-fallida-y-arranque-sin-busqueda.md), regla 3: un cerebro sin búsqueda configurada sólo existe en `development` y `test`; en cualquier otro `ENVIRONMENT` no arranca.)* |
| 6 | **El texto de búsqueda es el resumen seguido del bloque, con un tope de caracteres** (`max_query_chars`, 4000). Pasado el tope se conserva el final: se recorta primero el principio del resumen y recién después el del bloque |
| 7 | **`retrieval_logs` lo escribe la misma función que escribe el rastro**, colgado de la fila de `retrieval` y en la misma transacción. Al reintentar un bloque (ADR-0020), los logs viejos se borran junto con las filas de las que cuelgan, en una sola sentencia |
| 8 | **El `detail_json` de `retrieval` lleva `top_k`, el umbral, cuántos documentos quedaron afuera por modelo, cuántos puntos se descartaron, el largo del texto buscado y si el tope lo recortó.** Nunca el texto: lleva lo que escribió la clienta. El del `grounding` lleva el umbral y cuántos trozos lo alcanzaron |
| 9 | **El `score` del `grounding` es la mayor similitud, y queda vacío si la búsqueda no encontró nada.** La señal de ese caso vale 1. Una similitud exactamente igual al umbral es `grounded`, como pide ADR-0068, regla 4 (*"si ningún trozo lo alcanza"*). Su señal queda con `score` igual a `threshold_used` y `fired = false`: una consulta que lea una detección como `score >= threshold_used` la cuenta como detectada, y en `out_of_domain` hay que separar por `classifier_version` |
| 10 | **La fila de `signal_evaluations` del `grounding` se borra y se reescribe por bloque, señal y `classifier_version`**, y no sólo por bloque y señal. Si no, al escribirla borraría la que el `triage` ya escribió para `out_of_domain` en el mismo bloque |
| 11 | **El texto fijo del corte vive en `notices.yaml`, entrada `grounding`**, y el archivo sube a la versión 2. `retrieval.yaml` sigue en la versión 1, porque ningún turno buscó antes de que entrara el umbral |

## 3. Justificación

- **0,5 porque lo eligió el grupo** como punto de partida razonable para la similitud coseno de voyage-4, a calibrar con datos.
- **Fallar el paso** por el mismo argumento que la nota de ADR-0073: seguir sin documentos puede inventar un precio, y transferir haría que una falla nuestra contara como una pregunta que los documentos no responden, contra OE6. Fallar es lo que ya hace el turno cuando falla el proveedor del `triage`, y el núcleo ya sabe qué hacer con eso.
- **5 s por llamada y 15 s por turno porque lo decidió el grupo.** Con 10 s, dos llamadas más antes de generar dejaban muy poco margen.
- **Fuera del `runner` porque el contrato de un paso no alcanza.** Un paso recibe el texto y devuelve una fila, y estos dos necesitan el buscador y el resumen, se pasan los trozos entre sí y, para un agente sin base, no escriben fila. **Después de las señales** porque un turno que el `triage` ya delegó, por ejemplo por un pedido explícito de hablar con una persona, no tiene por qué pagar una búsqueda que nadie va a usar.
- **Conservar el final del texto** porque lo último que escribió la clienta es lo que se le está preguntando al agente, y el principio del resumen es lo más viejo del contexto. Voyage trunca solo si no se le pone un tope, y entonces la fila diría que se buscó algo que no se buscó.
- **Escribir los logs con el rastro** porque cuelgan de la fila del paso, y una fila sin sus logs sería un `hit` sin evidencia.
- **Borrar por `classifier_version`** porque ADR-0068, regla 7, decidió que la señal la escriben dos pasos en el mismo bloque, y el índice único de la tabla ya los distingue por esa columna.

## 4. Consecuencias

### Positivas

- El agente contesta con sus documentos, y lo que no responden va a una persona con un aviso a la clienta.
- Todo turno que recuperó deja en `retrieval_logs` todas las similitudes, también las que no alcanzaron el umbral: el umbral se puede barrer después sin volver a buscar.
- Un turno delegado por el `triage` no paga la búsqueda.

### Negativas / costos asumidos

- **15 s no cubren el peor caso.** El `triage` (hasta 8 s), Voyage (5 s), Qdrant (5 s) y la generación (8 s) suman 26 s. Un turno lento en todo corta por tiempo en el núcleo y se reintenta (ADR-0020), con lo que paga de nuevo todo lo anterior.
- **Con 15 s por intento, el presupuesto del turno de 30 s (ADR-0029) alcanza para dos intentos y no para tres.** La cuenta de ADR-0029 —*"hasta 10 s por intento, con hasta tres intentos"*— queda corta.
- **Una caída de Voyage o de Qdrant deja a todos los agentes con base sin responder** mientras dura, aunque la pregunta no necesitara documentos.
- **El umbral es provisorio**, y equivocarse duele en las dos direcciones: bajo, deja pasar trozos que no responden; alto, transfiere preguntas que los documentos sí responden. Ninguno de los dos errores se ve sin la evaluación.
- **Un cerebro sin búsqueda configurada contesta sin documentos**, y sólo lo dice el log de arranque. Un agente con base no se entera. *(Desde [ADR-0077](0077-busqueda-fallida-y-arranque-sin-busqueda.md), sólo en `development` y `test`.)*
- **El umbral de la política `out_of_domain` no afecta al `grounding`.** Su corte lo decide el umbral de `retrieval.yaml`, y de la política sólo cuenta si está encendida. Un administrador que mueva ese umbral mueve el del `triage` y no el del RAG.
- **En modo sugerencia, un `not_grounded` deja al Operator sin borrador** para ese turno: no hay generación, y el núcleo no envía el texto ni transfiere (ADR-0046).
- **Cada turno que recupera suma dos filas a `pipeline_steps` y hasta `top_k` a `retrieval_logs`**, que crecen con cada bloque y no se borran.
- **Los dos pasos no son entradas del `runner`**: el orden del tramo de entrada ya no vive sólo en `runner.INBOUND`, y quien lo lea tiene que leer también la ruta del turno.
- **El texto de búsqueda pierde el principio del resumen** cuando es muy largo, y la búsqueda se sesga a lo último que se habló.
- **El recorte por caracteres no es por tokens.** 4000 caracteres son unos 1000 tokens con la equivalencia de ADR-0071, muy por debajo del límite de Voyage, pero es una estimación.
- **No hay un caso del *smoke* que recorra un turno con documentos.** El agente del *seed* lo comparten todos los casos, y darle una base haría que los demás recuperen. El *smoke* prueba que el cerebro arranca con la búsqueda y que, para un agente sin base, la lectura corre contra el DDL real; el resto lo prueban los tests de la ruta con un buscador doblado y los de integración contra la base y un Qdrant reales.

### Lo que queda abierto

| Qué | Estado |
|---|---|
| **Calibrar el umbral y `top_k`** | La evaluación de recuperación, diferida por [ADR-0075](0075-medicion-de-oe6-y-evaluacion-de-recuperacion.md) |
| **El presupuesto del turno (`TURN_BUDGET_MS`) frente a los 15 s por intento** | Se revisa con la operación real: hoy alcanza para dos intentos |

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Seguir sin documentos si la búsqueda falla** | El agente puede inventar justo lo que la base debía responder |
| **Transferir si la búsqueda falla** | Cuenta una falla nuestra como una pregunta que los documentos no responden, y ensucia OE6 |
| **Los dos pasos como entradas del `runner`** | El contrato de un paso no lleva el buscador ni el resumen, ni deja que un agente sin base no escriba fila |
| **Buscar antes de evaluar las señales** | Un turno que el `triage` delega pagaría una búsqueda que nadie usa |
| **Dejar que Voyage trunque** | La fila diría que se buscó un texto que el proveedor no leyó entero |
| **Mantener 10 s en el núcleo** | Dos llamadas más antes de generar dejaban muy poco margen; el grupo eligió 15 |
