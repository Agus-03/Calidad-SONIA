# ADR-0037 — Toda conversación nace con un vendedor, y el vendedor maneja el bot de las suyas

| Campo | Valor |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 2026-09-13 |
| **Etapa** | Construcción — Fase 5 (bandeja manual mínima), antes del merge |
| **Resuelve** | Que ninguna conversación quede sin vendedor, a quién se le da cuando el cliente escribe, y quién prende y apaga el bot de una conversación asignada |
| **Requisitos afectados** | F02, F14, F16, F22, F26, F27, F35, F36 · OE6, OE7 · Anexo B |
| **Enmienda** | [ADR-0004](0004-asignacion-de-conversaciones.md) §2 — *"una conversación entrante llega sin operador asignado"* y *"no hay round-robin"* · [ADR-0035](0035-reglas-de-la-bandeja-manual.md), regla 10 — el modo reservado a `supervisor` y `admin` · ADR-0035, regla 15 — *"la única asignación que existe es la autoasignación de la regla 7"* |
| **Relacionada con** | [ADR-0009](0009-horario-de-atencion.md), [ADR-0020](0020-idempotencia-de-los-mensajes-salientes.md), [ADR-0025](0025-atribucion-por-operador.md), [ADR-0035](0035-reglas-de-la-bandeja-manual.md) |

---

## 1. Contexto

La revisión de diseño de la Fase 5 encontró un agujero en el argumento que sostenía la regla 10 de [ADR-0035](0035-reglas-de-la-bandeja-manual.md): reservar el modo del agente IA a `supervisor` y `admin` para que el vendedor no pudiera quedarse con una venta apagando el bot.

Con [ADR-0004](0004-asignacion-de-conversaciones.md), toda conversación nueva entraba al pool, y la que atendía sólo la IA seguía ahí, porque nadie la había respondido. El vendedor ve el pool (ADR-0035, regla 1), y responderlo una vez alcanza para tomar la conversación (regla 7). Con eso pasa a ser el único operador con salientes, y [ADR-0025](0025-atribucion-por-operador.md) le atribuye la venta. **Para quedarse con la venta no necesitaba apagar el bot.**

El equipo resolvió el problema desde otro lado: *"el bot está bueno para dar soporte o responder en horario no laboral, pero siempre debe estar asignado a un vendedor"*. Esa frase tenía dos lecturas:
- **a)** el bot no responde hasta que un vendedor toma la conversación;
- **b)** toda conversación se asigna a un vendedor al llegar, y el bot responde desde el primer mensaje.

El equipo eligió la **b**, *"para no complicarnos"*, y decidió además cuatro cosas: el criterio de reparto, quiénes entran, qué pasa si no hay nadie y quién maneja el bot.

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **Cuando el cliente escribe en una conversación sin vendedor, se le asigna uno**: primero **su vendedor anterior** —el de su conversación previa más reciente que tuvo vendedor—, si sigue pudiendo recibir; si no, **el vendedor que hace más tiempo que no recibe una**, con los que nunca recibieron primero. Una conversación previa que quedó en el pool no cuenta como previa: llegó cuando no había nadie, y no dice nada del vendedor del cliente. Para el turno cuenta **toda** conversación que el vendedor recibió, sea por turno, por cliente que vuelve o tomándola del pool |
| 2 | **Entran en el reparto sólo los `operator` activos y no dados de baja.** `supervisor` y `admin` no reciben conversaciones por reparto |
| 3 | **Si no hay ningún vendedor que pueda recibir, la conversación queda en el pool**, y rige lo de siempre: [ADR-0004](0004-asignacion-de-conversaciones.md) y las reglas 5, 7 y 8 de ADR-0035. Se vuelve a intentar la próxima vez que el cliente escriba |
| 4 | **El `operator` prende y apaga el bot de las conversaciones que tiene asignadas; `supervisor` y `admin`, el de cualquiera de la cuenta.** Una del pool se le rechaza al `operator` con 409 `conversation_not_assigned` (primero tiene que tomarla), y una ajena con 404, porque no la ve |
| 5 | **El reparto ocurre en la misma transacción del webhook que trajo el mensaje**, y nunca mueve una conversación que ya tiene vendedor. Queda en `conversation_assignments` sin *"asignado por"* —lo hizo el sistema— y con el motivo de la regla que eligió: `auto_assigned_to_previous_operator` o `auto_assigned_by_turn` |
| 6 | **El criterio no mira el horario.** Se decide igual a las 10 que a las 23 |
| 7 | **Tener vendedor no apaga el bot.** La conversación nace con el modo de siempre (autónomo si la bandeja tiene agente IA), y el bot responde desde el primer mensaje hasta que alguien lo apague |
| 8 | **Las dos consultas del reparto tienen su índice desde `0001`**: `idx_conversations_contact_id_created_at_id` sobre `conversations (contact_id, created_at, id)`, de las no borradas, para el vendedor anterior, e `idx_conversation_assignments_to_operator_id_created_at_id` sobre `conversation_assignments (to_operator_id, created_at, id)`, para el turno. Es una excepción declarada a la regla de [`migrations/README.md`](../../../src/core/migrations/README.md), como la de [ADR-0036](0036-paginacion-por-cursor-en-la-bandeja.md), regla 10, decidida por el equipo el 2026-09-13 |

## 3. Justificación

- **Cierra el agujero del pool sin tocar la atribución.** Si toda conversación nace con dueño, en el pool ya no hay conversaciones que atiende la IA esperando a que otro vendedor las tome: sólo queda lo que llegó cuando no había nadie para recibirlo. Lo que no cierra es que el vendedor desplace al bot en sus propias conversaciones (§4).
- **La lectura b es la que no le quita nada al bot.** La a callaba al bot justo en el caso que el equipo quería cubrir: un cliente nuevo que escribe a las 23 h no tenía respuesta hasta que alguien lo tomara. Además dejaba sin efecto el modo autónomo de F02 hasta que alguien tomara la conversación, que es la lectura de F02 con la que ADR-0004 (§3) defendió que el agente IA puede operar sin intervención humana.
- **El vendedor anterior primero, porque en ventas el cliente tiene "su" vendedor.** Mira sólo al último que tuvo: si ése ya no puede recibir, decide el turno y no un vendedor más viejo, que el cliente probablemente ya no recuerda. Una conversación intermedia que se resolvió en el pool no lo desplaza, porque no la eligió nadie: llegó cuando no había vendedores activos.
- **Por turno y no por carga, porque es una sola consulta y no hay que definir qué es "carga".** Es el mismo argumento con el que ADR-0004 descartó el balanceo. El turno sale de la historia que ya se guarda (`conversation_assignments`), sin columnas ni configuración nuevas. Cuenta toda conversación recibida y no sólo las del turno, porque el turno reparte trabajo, y un cliente que vuelve también es trabajo.
- **Sólo los `operator`, porque el reparto es trabajo de venta.** Si entraran `supervisor` y `admin`, les tocarían conversaciones por turno como a cualquier vendedor.
- **El pool como respaldo no tira nada de lo construido.** Las reglas de tomar el pool respondiendo siguen siendo correctas para el caso que queda, y ya están probadas.
- **La regla 4 se sigue de la 1.** Con B, el vendedor es dueño de la conversación desde el primer mensaje, y el argumento de atribución de ADR-0035 §3.1 deja de sostener la regla 10: la venta la cuenta ADR-0025 por sus mensajes, apague o no el bot, así que reservarle el interruptor no le impide acreditársela. El incentivo que ese argumento temía sigue en pie, y se declara en §4. Queda el argumento de medición, y no alcanza para lo que costaba: con el modo reservado, **el cliente recibiría dos voces en todas las conversaciones** en las que el vendedor quiera contestar, hasta que un supervisor apague el bot. De paso el sistema vuelve al texto de F27 y del Anexo B, que le dan al operador ese interruptor.
- **Del pool no, porque todavía no es suya.** El vendedor ve el pool para poder tomarlo; si pudiera apagar el bot de una conversación sin tomarla, la dejaría callada y sin dueño.
- **En la transacción del webhook, para que la conversación no exista nunca sin el vendedor que le correspondía.** La asignación usa el mismo `UPDATE … WHERE operator_id IS NULL` que la toma manual, así que si un vendedor la responde desde el pool en ese mismo momento, gana uno solo y el otro no escribe nada.
- **Sin *"asignado por"*, porque no la asignó nadie.** El motivo es lo que dice que fue el sistema y con qué regla, y es lo que F26 va a leer como historia.
- **La regla 6 respeta [ADR-0009](0009-horario-de-atencion.md)**, que dejó el horario de atención fuera de alcance.
- **Los índices de la regla 8 entran ya, por el argumento de ADR-0036.** Las dos consultas existen, corren con cada conversación nueva y dentro de la transacción del webhook, y después de congelar `0001` no se pueden construir con `CONCURRENTLY`.

## 4. Consecuencias

### Positivas

- Toda conversación tiene un responsable desde el primer mensaje, y `conversation_assignments` la cuenta desde ahí.
- Desaparece el camino "tomar el pool para quedarse con la venta": el pool sólo tiene lo que nadie pudo recibir.
- **Se revierte la desviación de F27 que había declarado ADR-0035.** El operador vuelve a manejar el bot de sus conversaciones, que es lo que dicen el requisito y el Anexo B. La enmienda de C01 en [`00-convenciones.md`](../00-convenciones.md) queda revertida.
- El vendedor que quiere contestar puede callar al bot él mismo, sin pedírselo a un supervisor.

### Negativas / costos asumidos

- **Revierte dos alternativas que ADR-0004 había descartado**: la asignación al crear la conversación y el reparto por turnos. ADR-0004 descartó el round-robin porque, con un equipo chico, resolvía un problema que no existía, y la asignación obligatoria en el alta porque *"contradice F02: una conversación en modo autónomo no necesita operador"*. El agujero de ADR-0035 mostró que el primer problema sí existía. El segundo argumento no alcanza a esta decisión: la asignación no es obligatoria —si no hay nadie, queda el pool (regla 3)— y tener vendedor no le quita autonomía al bot (regla 7). El cambio se declara en vez de disimularlo.
- **El turno no sabe quién está trabajando.** Un vendedor de vacaciones o enfermo sigue recibiendo conversaciones mientras esté activo. El equipo tiene que desactivarlo, y **hoy eso es SQL a mano**: la gestión de operadores de F18 todavía no existe. Desactivarlo además sólo lo saca del reparto futuro: sus conversaciones abiertas siguen a su nombre, porque el reparto corre sólo sobre las que no tienen vendedor (regla 5), y **hasta la Fase 11 quedan trabadas**: el `supervisor` las ve, pero no puede responderlas ni reasignarlas (ADR-0035, reglas 5 y 15). La única salida para ellas también es SQL a mano. Antes eso le pasaba sólo a lo que alguien tomaba a mano; ahora le puede pasar a cualquier conversación.
- **El vendedor puede desplazar al bot en sus conversaciones.** ADR-0035 §3.1 temía que el vendedor *"se acredita conversiones que la IA quizás habría cerrado"*. Con toda conversación a su nombre, le alcanza con escribir una vez en una que venía llevando el bot: ADR-0025 se la acredita, porque es el operador con más salientes, y la conversación sale del numerador autónomo de OE7 (≥ 75 %). Antes tenía que ir a buscarla al pool; ahora está en su bandeja. Esta decisión no lo mitiga.
- **El numerador de F35 se separa de su denominador en el caso común.** F14 dice *"El operador marca una conversión de venta"*, y `sales.operator_id` es obligatorio: en una conversación que cerró el bot, quien marca la venta es, naturalmente, su vendedor asignado. La venta suma a su numerador, pero la conversación no entra en su denominador, porque ADR-0025 cuenta mensajes y el vendedor no mandó ninguno, y la tasa puede superar el 100 %. Es la trampa que describe ADR-0025 §1. ADR-0037 no la crea, pero la vuelve lo común.
- **Dos mensajes que llegan a la vez pueden darle conversaciones al mismo vendedor.** El turno se lee y después se escribe, sin bloquear: los dos webhooks ven al mismo vendedor como "el que más esperó". Es un desbalance leve y se asume.
- **El vendedor anterior gana aunque esté cargado.** El cliente vuelve a su vendedor aunque ése tenga muchas más conversaciones abiertas que el resto.
- **Vuelve el problema de medición.** Cada vendedor decide en cuáles de sus conversaciones responde el bot, así que la población que miden F36 y OE6 depende de criterios individuales. **Nadie registra quién cambió el modo ni cuándo**: si hace falta explicar esa población, hoy no hay rastro.
- **`conversations.operator_id` deja de distinguir lo que atendió la IA.** Ahora toda conversación tiene vendedor, aunque el bot haya hecho todo el trabajo. ADR-0025 describía esa columna como *"en modo autónomo es nulo"*, y eso ya no es cierto: acreditar por `operator_id` (el "último asignado" que ADR-0025 descartó) le daría al vendedor el trabajo del bot. Refuerza la elección de ADR-0025 de contar los mensajes de cada operador. Cualquier métrica de F16 que cuente conversaciones asignadas por vendedor va a incluir las que resolvió la IA.
- **Lo que queda en el pool espera a que el cliente vuelva a escribir.** Si una conversación llega de noche sin vendedores activos, se asigna con el próximo mensaje del cliente, no cuando entra el primer vendedor.
- **Dos índices más, entrados antes de una consulta lenta** (regla 8). Es la misma excepción que ADR-0036, con el mismo costo: cada escritura en esas tablas los actualiza, y si el volumen real nunca los necesita, sobran. Y cambiar `0001` obliga a cada integrante a recrear su base.

### Lo que queda abierto

| Qué | Por qué no se resuelve acá |
|---|---|
| **Ausencias y disponibilidad del vendedor** | Hoy se resuelven desactivándolo. Un mecanismo propio, y la reasignación de lo que quedó trabado, van con F26 en la **Fase 11** |
| **Registrar los cambios de modo** | Explicaría la población de F36 y OE6. Necesita decidir dónde vive ese registro ([ADR-0007](0007-alcance-del-registro-de-auditoria.md) acota la auditoría a la configuración del agente IA) |
| **Balanceo por carga** | Si el equipo crece y el turno se queda corto. Primero hay que definir qué es "carga" |
| **Que el vendedor desplace al bot** | Mitigarlo exige decidir qué cuenta como intervención de un vendedor en una conversación del bot, y eso pertenece a la regla de atribución (H-25) y a cómo se mide OE7: **Paso 11** |
| **El numerador de F35 cuando la venta la marca el vendedor asignado** | Se decide al construir F14 y F35, junto con H-25 |

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **a) El bot espera a que un vendedor tome la conversación** | Deja sin respuesta a un cliente nuevo fuera de horario, que es el caso para el que el equipo quiere al bot. Contradice F02 |
| **Sostener el pool de ADR-0004 y declarar el agujero** | Deja abierta, a la vista de todos, una forma de quedarse con ventas ajenas |
| **Balanceo por carga** | Hay que definir qué cuenta como carga y mantenerlo. El turno alcanza para un equipo chico |
| **Un vendedor fijo por número** | Con un solo número, todas las conversaciones le caen a la misma persona |
| **Repartir según el horario** | Reabre [ADR-0009](0009-horario-de-atencion.md) |
| **Que `supervisor` y `admin` entren en el reparto** | Les llegarían conversaciones de venta por turno, y ése no es su trabajo |
| **Mantener el modo sólo para `supervisor` y `admin`** | Con toda conversación asignada, habría dos voces en cada una en la que el vendedor quiera contestar. Y el argumento de atribución que lo sostenía ya no aplica |
