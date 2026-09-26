# ADR-0048 — El contexto dinámico: quién escribe cada tabla y cómo se reescribe en cada turno

| Campo | Valor |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 2026-09-26 |
| **Etapa** | Construcción — Fase 9 (contexto dinámico y transferencia) |
| **Resuelve** | Qué paquete escribe `conversation_contexts` y `conversation_context_versions` —las dos filas que [struct.md](../../struct.md) §2.4 dejaba sin dueño—, qué produce el reescritor de F04 y qué no, en qué turnos corre, qué pasa cuando falla, y dónde queda la evidencia de un cambio de intención ([ADR-0023](0023-definicion-de-cambio-de-intencion.md)) |
| **Requisitos afectados** | F04 · F11 · OE2 · OE4 |
| **Naturaleza** | F04 dice *"El motor actualiza el contexto dinámico en cada turno: inyecta objeciones, intención y datos detectados del cliente"*. **No dice quién lo guarda, con qué se reescribe ni qué pasa si falla**, y los documentos de diseño se contradecían en el primer punto (§1). Todo lo de §2 es decisión del equipo |
| **Enmienda** | [08-structure-rules.md](../08-structure-rules.md), **regla 3** — `conversation_context_versions` entra en la lista de tablas de evidencia que el cerebro puede escribir · [07-secuencia-del-turno.md](../07-secuencia-del-turno.md), **§0** — `conversation_contexts` pasa de la lista del cerebro a la del núcleo; **el diagrama de §2**, **el diagrama de §3** y **§4 B.2, paso 5** — el núcleo copia la versión a `conversation_contexts` en la transacción que cierra el turno · [03-capa-prompts.md](../03-capa-prompts.md), **Regla 3, tercer punto** — la reescritura que falla **no se reintenta dentro del turno** |
| **Relacionada con** | [ADR-0006](0006-anonimizacion-de-pii.md), [ADR-0017](0017-decomposicion-en-despliegues-y-lenguajes.md), [ADR-0020](0020-idempotencia-de-los-mensajes-salientes.md), [ADR-0023](0023-definicion-de-cambio-de-intencion.md), [ADR-0041](0041-temporizador-del-turno-por-poller.md), [ADR-0045](0045-senales-de-transferencia-y-desactivacion-del-agente-ia.md), [ADR-0046](0046-modo-sugerencia.md), [ADR-0047](0047-extraccion-de-datos-del-contacto.md), [DER/entidades.md](../DER/entidades.md) |

---

## 1. Contexto

Las dos tablas de F04 existen desde la migración inicial y nadie las escribía. Tres documentos decían tres cosas distintas sobre quién debía hacerlo:

- **[07-secuencia-del-turno.md](../07-secuencia-del-turno.md) §0** ponía las dos en la lista del **cerebro**, y el diagrama de §2 dibujaba al cerebro insertando las versiones.
- **La regla 3 de [08-structure-rules.md](../08-structure-rules.md)** no incluye ninguna de las dos entre las tablas de evidencia que el cerebro puede escribir.
- **[struct.md](../../struct.md) §2.4** las dejaba **sin dueño**: *"el cerebro produce el resumen y el núcleo lo persiste; falta decir cuál paquete del núcleo"*.

Hay además una restricción del esquema que ninguno de los tres mencionaba: **`generations.conversation_context_version_id` es una FK**, y la generación la escribe el cerebro antes de responderle al núcleo. La versión tiene que existir antes que la generación.

Quedaban abiertas también cuatro cosas que F04 no dice: qué columnas llena un modelo y cuáles no, en qué turnos se reescribe, qué pasa si la reescritura falla, y dónde queda la evidencia del *cambio de intención* que [ADR-0023](0023-definicion-de-cambio-de-intencion.md) le asignó al reescritor.

## 2. Decisión

### 2.1 Las versiones las escribe el cerebro; el contexto vigente, el núcleo

- **`conversation_context_versions` la escribe el cerebro, como evidencia.** Es el registro inmutable de qué produjo cada reescritura, con qué modelo y en cuánto tiempo, igual que `generations` lo es de cada respuesta. Se escribe **en la misma transacción que la generación y antes que ella**, que la apunta. La regla 3 de `08-structure-rules` pasa a nombrarla.
- **`conversation_contexts` la escribe el núcleo, en `turn/`**, dentro de la transacción que cierra el turno: la del mensaje saliente, la del corte después de generar y la de la sugerencia. **Copia con SQL la versión a la que apunta la generación**, sin que el contrato del salto cambie: el núcleo ya recibe el `generation_id`.
- **La copia sólo avanza.** Si la fila vigente ya tiene un `turn_number` igual o mayor, no se toca. Es lo que impide que una generación que se quedó con el contexto anterior (§2.4) rebobine la fila.
- **El contenido de la versión usa como claves las columnas de `conversation_contexts`** —`summary`, `primary_intent`, `objections_json`, `urgency_level`, `budget_signal`, `extracted_data_json`— más `intent_changed`, que la tabla del contexto vigente no tiene.

### 2.2 Qué produce el modelo y qué no

| Columna | Quién la decide |
|---|---|
| `summary` | El reescritor (`rules/rewriter.yaml`). Obligatorio: una respuesta sin resumen no es una reescritura |
| `objections_json` | El reescritor. Una lista de textos cortos, cinco como máximo |
| `urgency_level` | El reescritor, en una escala de **1 a 3** (sin apuro, pronto, ya). Fuera de la escala, `NULL` |
| `budget_signal` | El reescritor, en texto libre y corto; `NULL` si la clienta no dijo nada |
| `primary_intent` | **No es del modelo.** Es la etiqueta de mayor `confidence` en los `detected_intents` del bloque, que es como la define [DER/entidades.md](../DER/entidades.md); ante un empate, la que se dijo primero. Si F11 no leyó nada, queda la anterior |
| `extracted_data_json` | **No es del reescritor.** Se arrastra sin cambios; hoy está vacío (§4) |

**`intent_changed`** es la lectura del sistema sobre [ADR-0023](0023-definicion-de-cambio-de-intencion.md): el reescritor dice si el bloque deja sin valor el contexto anterior, y **cuando lo dice escribe el resumen y las objeciones de cero**. Queda sólo en la versión, que es donde OE4 lo compara contra las anotaciones.

El reescritor recibe el contexto anterior y el texto del bloque **ya redactado** ([ADR-0006](0006-anonimizacion-de-pii.md)), usa el modelo del AIAgent con temperatura 0 y su prompt es un archivo versionado de `rules/` (regla 9).

### 2.3 Se reescribe en los turnos que llegan a generar

- **Corre después del `triage` y antes del ensamblado**, que es el orden de `07-secuencia` §2, y **en paralelo con el clasificador de intenciones**: ninguno lee el resultado del otro, y así el turno espera la llamada más lenta y no la suma de las dos. `primary_intent` se completa cuando terminan las dos.
- **Un turno cortado antes de generar no reescribe nada**: ni el de la lista de bloqueo, ni el del *guardrail* de entrada, ni el del `triage` ([ADR-0045](0045-senales-de-transferencia-y-desactivacion-del-agente-ia.md)). No hay respuesta a la cual darle el contexto.
- **Un turno cortado después de generar sí lo hace**, y el núcleo copia su versión igual: lo que escribió la clienta se leyó, aunque la respuesta no salga.
- El contexto entra en la sección `[CONTEXTO DE LA CONVERSACIÓN]` del mensaje `user`, que la Regla 1 de `03-capa-prompts` ya tenía declarada y vacía. Una línea sin valor no se imprime.

### 2.4 Si la reescritura falla, se responde con el contexto que había, sin reintentar

- **No se reintenta dentro del turno.** El núcleo espera diez segundos por todo el turno y el proveedor tiene ocho por llamada: un segundo intento no entra. **El reintento es el bloque siguiente.** Esto enmienda el tercer punto de la Regla 3 de `03-capa-prompts`, que decía *"se reintenta un número acotado de veces"*.
- **La generación apunta a la versión del contexto vigente**, la que el núcleo copió la última vez. En el primer turno de una conversación, a ninguna (`NULL`).
- **No se escribe versión**, y la falla queda en el log (`context not rewritten`).
- **La falla no tiene fila en `pipeline_steps`**: la enumeración `PipelineStep` no tiene un valor para la reescritura, y agregarlo es una migración que F04 no necesita. La evidencia de una reescritura es su fila de versión. **Una falla se cuenta igual**: es una generación cuya versión tiene un `turn_number` distinto del de su bloque, o ninguna.

## 3. Justificación

**Por qué las versiones son del cerebro.** Por la FK: si las escribiera el núcleo, el cerebro tendría que dejar la generación sin versión y el núcleo completarla después, lo que suma un segundo escritor a otra columna de `generations`. Y porque son evidencia en el sentido exacto de `07-secuencia` §0: *lo que el cerebro observó y produjo*, inmutable, que es lo que OE2 y OE4 miden.

**Por qué el contexto vigente es del núcleo.** Es **estado de la conversación**, no evidencia: se sobrescribe en cada bloque y es lo que el turno siguiente toma como punto de partida. Es la misma línea que [ADR-0047](0047-extraccion-de-datos-del-contacto.md) trazó para `contact_attributes` —*el cerebro escribe evidencia y el núcleo escribe hechos*— y tiene una consecuencia práctica: el contexto vigente es **el del último turno que terminó**, porque se escribe en la transacción que lo cierra. Un turno que el núcleo abandona no adelanta el contexto del siguiente.

**Por qué `turn/`.** Es el paquete que conduce la transacción de cierre y el único del núcleo que ya escribe en una tabla de evidencia (`generations.message_id`). Ponerlo en `conversations/` agrandaría el agregado compartido con una tabla que sólo el turno toca.

**Por qué copiar con SQL y no mandar el contexto en la respuesta.** El contenido ya está en la base, escrito por el cerebro en la misma transacción que la generación. Mandarlo de nuevo en el contrato lo duplica, y abre la posibilidad de que la copia y la versión digan cosas distintas.

**Por qué `primary_intent` sale de F11.** El DER ya lo definía así, y un segundo modelo decidiéndolo daría dos respuestas a la misma pregunta en el mismo turno.

**Por qué no reintentar.** Regla 3 pide que la falla no bloquee a la clienta. Un reintento que no entra en el presupuesto del turno hace exactamente eso: el núcleo corta a los diez segundos y la clienta se queda sin respuesta, que es peor que responder con el contexto del bloque anterior.

## 4. Consecuencias

### Positivas

- **Las cuatro tablas de §2.4 de `struct.md` quedan en dos**: las dos de F04 tienen dueño.
- **F04 queda construido de punta a punta**, con cada reescritura versionada, que es lo que OE2 necesita para evaluar el mecanismo.
- **OE4 tiene de dónde leer la lectura del sistema** sobre el cambio de intención, sin agregar columnas.
- **El contrato del salto no cambia**, y el cerebro sigue sin escribir nada que no sea evidencia.
- **La latencia agregada es la diferencia entre dos llamadas y no una llamada entera**, porque corre en paralelo con F11.

### Negativas / costos asumidos

- **Una llamada más al proveedor por turno generado**, con su costo. No queda en `generations.cost`, que es de la respuesta: el costo de reescribir **no se registra en ninguna parte**.
- **El SQL del núcleo depende de los nombres de las claves que escribe el cerebro.** Si `rules/rewriter.yaml` o el código cambian una clave, la copia deja la columna en `NULL` **sin fallar**. Lo cubre un test de integración, no el tipo.
- **Es la segunda enmienda a la regla 3** en la misma fase, después de la que dejó en el núcleo `contact_attributes`. La lista de evidencia crece.
- **Un turno transferido no actualiza el contexto.** El Operator que recibe la conversación ve el contexto del último bloque que se respondió, no el del pedido de una persona.
- **Un turno que falla después de reescribir pierde la reescritura**: la versión se escribe con la generación y una generación que falla no se escribe. El bloque siguiente reescribe desde el contexto anterior, sin haber visto ese bloque.
- **Sin reintento, un tropiezo del proveedor deja el turno con el contexto anterior.** Es el costo que la Regla 3 ya asumía para cuando se agotaran los reintentos, y ahora se paga en la primera falla.
- **Un reintento del núcleo sobre un 5xx ([ADR-0020](0020-idempotencia-de-los-mensajes-salientes.md)) deja dos versiones del mismo turno**, porque las versiones no se borran. Se toma la más nueva.
- **La escala de urgencia de 1 a 3 es una lectura del equipo.** El anteproyecto nombra la columna y ninguna escala.
- **`extracted_data_json` queda vacío.** ADR-0047 mandó los datos del contacto a `contact_attributes` y lo comercial a las otras columnas de este contexto, así que hoy F04 no tiene nada que poner ahí. La columna existe y no se llena.
- **`primary_intent` se apoya en una `confidence` que el propio modelo declara**, sin calibrar.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **El cerebro escribe las dos tablas**, como decía `07-secuencia` §0 | Es la más simple, pero `conversation_contexts` es estado de la conversación, y el cerebro escribiría el punto de partida del turno siguiente aunque el núcleo abandonara este |
| **El núcleo escribe las dos**, como sugería `struct.md` §2.4 | Choca con la FK: la generación tendría que escribirse sin versión y completarse después, con un segundo escritor sobre otra columna de `generations` |
| **El contexto viaja en la respuesta del turno** | Duplica en el contrato lo que ya está en la base, y la copia y la versión pueden no coincidir |
| **Un valor `rewrite` en `PipelineStep`** | Es una migración sobre un `CHECK` con filas, y la fila de versión ya dice modelo, latencia y resultado |
| **Reintentar una vez** | No entra en los diez segundos del turno: el núcleo corta y la clienta se queda sin respuesta |
| **Que el reescritor decida también `primary_intent`** | Dos modelos respondiendo la misma pregunta en el mismo turno, y el DER ya la define por `detected_intents` |
