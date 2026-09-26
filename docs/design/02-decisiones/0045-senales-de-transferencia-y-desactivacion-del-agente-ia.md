# ADR-0045 — Las señales de transferencia: qué mide cada una, qué escribe, y qué le hace al AIAgent cuando dispara

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · **el párrafo de §2.6 sobre los cortes de entrada es una propuesta, pendiente del grupo** |
| **Fecha** | 2026-09-26 |
| **Etapa** | Construcción — Fase 9 (contexto dinámico y transferencia) |
| **Resuelve** | Cómo se evalúan `explicit_request` y `no_resolution`, dónde queda la frontera entre `no_resolution` y `frustration`, qué significa `signal_evaluations.threshold_used` para una señal que no tiene puntaje, qué evalúa un turno cortado, y qué pasa con el AIAgent cuando una transferencia ocurre |
| **Requisitos afectados** | F12 · F02 · F27 · F36 · OE3 · OE4 · OE6 |
| **Naturaleza** | **Lectura del equipo.** F12 nombra cuatro señales —*"turnos sin resolución, frustración, consultas fuera del dominio RAG, solicitud explícita de humano"*— y no define ninguna, ni dice con qué se detectan, ni qué le pasa al AIAgent después de transferir. Todo lo de §2 es decisión nuestra |
| **Enmienda** | [ADR-0042](0042-pipeline-de-decision-del-turno.md) §2.4 — **la fila `triage` → `out_of_domain` de la tabla** se amplía: el `triage` corta y delega por cualquier señal de F12 que dispare, no sólo por `out_of_domain` (§2.5). **La regla 3** (*"Sólo el `triage` delega"*) se sostiene: sigue siendo el único paso que delega. **La regla 4** (*"Un turno cortado evalúa una señal de cuatro"*) queda reemplazada por §2.6. **La consecuencia** *"Un turno generado no actúa sobre una propuesta de transferencia"* deja de valer · [ADR-0044](0044-senal-de-frustracion-diferida-a-la-fase-16.md) §2.1 — **la propuesta de frontera con `no_resolution` se cierra** tal como estaba propuesta (§2.3) · [07-secuencia-del-turno.md](../07-secuencia-del-turno.md) §1 — la transferencia, además de mover `conversations.status` y `operator_id`, **apaga el bot de esa conversación** (§2.7) |
| **Relacionada con** | [ADR-0014](0014-medicion-de-oe3-y-oe4.md), [ADR-0021](0021-atribucion-entre-senales-simultaneas.md), [ADR-0024](0024-unidad-y-alcance-de-oe6.md), [ADR-0035](0035-reglas-de-la-bandeja-manual.md), [ADR-0037](0037-toda-conversacion-nace-con-vendedor.md), [ADR-0038](0038-alta-baja-y-pausa-del-agente-ia.md), [ADR-0042](0042-pipeline-de-decision-del-turno.md), [ADR-0043](0043-prompt-injection-en-el-guardrail-de-entrada.md), [ADR-0044](0044-senal-de-frustracion-diferida-a-la-fase-16.md), [ADR-0046](0046-modo-sugerencia.md), [08-structure-rules.md](../08-structure-rules.md) |

---

## 1. Contexto

La Fase 8 dejó una señal de F12 construida —`out_of_domain`, que sale del `triage` y corta el turno ([ADR-0042](0042-pipeline-de-decision-del-turno.md), O1)— y [ADR-0044](0044-senal-de-frustracion-diferida-a-la-fase-16.md) difirió `frustration` a la Fase 16. Quedan dos: `explicit_request` y `no_resolution`. Antes de construirlas había cinco cosas sin decidir:

1. **Qué es un turno "sin resolución".** Ningún ADR lo define y el anteproyecto sólo dice *"turnos sin resolución"*, en plural.
2. **La frontera con `frustration`**, que ADR-0044 §2.1 dejó como propuesta porque el síntoma de las dos se parece.
3. **Con qué se detecta `explicit_request`.**
4. **Qué significa `threshold_used`.** El `triage` devuelve un veredicto categórico y hoy la columna guarda el umbral de la política vigente, o 0 si no hay, sin que nada se haya comparado contra él. Con señales numéricas, la misma columna tendría dos semánticas según `signal_type`.
5. **Qué hace el AIAgent después de transferir.** Hoy una transferencia pasa la conversación a `waiting` y nada más: el núcleo decide si responde mirando el modo y el estado del agente IA, no el estado de la conversación ([ADR-0038](0038-alta-baja-y-pausa-del-agente-ia.md), regla 2). **El mensaje siguiente de la clienta lo contesta el AIAgent**, en la conversación que acaba de pasarse a una persona.

Hay además un hueco que la Fase 8 dejó sin decir: **`out_of_domain` sólo escribe su `signal_evaluations` cuando corta.** Un turno `in_domain` no deja ninguna fila de esa señal, y [ADR-0014](0014-medicion-de-oe3-y-oe4.md) (c) pide una por cada señal evaluada, *"dispare o no"*.

## 2. Decisión

### 2.1 Las tres señales se evalúan en la misma llamada que el `triage`

**`explicit_request` y `no_resolution` se evalúan dentro de la llamada al LLM que ya hace el `triage`**, que desde esta fase devuelve tres veredictos en vez de uno. Las tres filas de `signal_evaluations` llevan como `classifier_version` la versión de `triage.yaml`, igual que `out_of_domain` hoy. **`out_of_domain` escribe su fila en todos los turnos que llegan al `triage`**, corte o no.

### 2.2 `no_resolution`: un juicio por turno y una racha

- **El LLM juzga si la respuesta anterior del AIAgent resolvió lo que la clienta preguntaba**, mirando esa respuesta y el bloque nuevo. Por eso el `triage` recibe también la última respuesta del AIAgent.
- **El `score` es la racha**: la cantidad de bloques seguidos, incluido éste, en que el juicio fue *"no resolvió"*. Si el juicio es *"resolvió"*, el `score` es 0.
- **La racha se corta** cuando el bloque anterior no tiene evaluación de `no_resolution` con la misma `classifier_version`, y **después de una transferencia**: una conversación que un Operator volvió a prender empieza de cero.
- **Se saltea cuando no hay respuesta anterior del AIAgent que juzgar**: el primer turno, o cuando lo último que salió fue de un Operator o un texto `system`. Queda anotada como salteada en el `detail_json` del `triage`, como `frustration` ([ADR-0044](0044-senal-de-frustracion-diferida-a-la-fase-16.md) §2.3).

### 2.3 La frontera con `frustration`

**Si la clienta se queja del AIAgent, es `frustration`; si sólo no avanza, es `no_resolution`.** Es la propuesta de [ADR-0044](0044-senal-de-frustracion-diferida-a-la-fase-16.md) §2.1, cerrada sin cambios: `no_resolution` mira lo que hizo el AIAgent y `frustration` lo que dice la clienta sobre eso. Cuando aplican las dos, el anotador escribe `frustration` en el `gold_value` de `transfer`, que es también la que gana en el orden de [ADR-0021](0021-atribucion-entre-senales-simultaneas.md).

### 2.4 `threshold_used` significa lo mismo en todas las filas

- **Una señal de sí/no se guarda como `score` 1 o 0, con `threshold_used = 1`.** Son `out_of_domain` y `explicit_request`. La columna dice siempre *"este puntaje se comparó contra este umbral"*, y para estas dos la comparación es trivial pero cierta.
- **`no_resolution` guarda la racha contra el umbral de su política**, un entero.
- **Sin política, el umbral sale de `rules/triage.yaml`**, que guarda uno por defecto por señal, como `frustration.yaml` en [ADR-0044](0044-senal-de-frustracion-diferida-a-la-fase-16.md) §2.2. Deja de escribirse 0.
- **`fired` no cambia de significado**: es *"cruzó el umbral **y** la política está encendida"*. Una señal que cruzó con la política apagada o ausente se escribe con `fired = false`, y el cruce se reconstruye con `score >= threshold_used`.
- **El ABM de `transfer_policies` rechaza un umbral distinto de 1 para las dos señales de sí/no**, para que lo guardado no diga algo que el turno no usa. **Diferido:** ese ABM no existe, y no se construye hasta que el grupo decida qué paquete del núcleo escribe la tabla ([struct.md](../../struct.md) §2.4). El Anexo C del anteproyecto pone `umbral_transferencia` dentro de la configuración del agente, así que la decisión se toma junto con el ABM de AIAgents. Mientras tanto, las políticas se cargan por seed.
- **`src/core/seeds/0001_demo.sql` lleva las filas de `transfer_policies`** del agente de demo, con 1 en las dos de sí/no. El valor de `no_resolution` en el seed es configuración de demo, no una medición.

### 2.5 Delega cualquier señal, y el corte es el del `triage`

**Si alguna de las tres dispara, el turno se corta en el `triage`**, igual que hoy lo corta `out_of_domain`: no se genera, a la clienta no se le dice nada (O1c) y el núcleo recibe la propuesta. La señal que se transfiere la elige el orden de [ADR-0021](0021-atribucion-entre-senales-simultaneas.md) entre las que dispararon. El `detail_json` del `triage` nombra la señal que cortó.

**`out_of_domain` sigue cortando aunque su política esté apagada** (ADR-0042, O1): es un veredicto sobre el dominio, no sólo una señal. `explicit_request` y `no_resolution` **no cortan si no disparan**, y con la política apagada no disparan: el administrador eligió no transferir por ellas, y el turno sigue y responde.

### 2.6 Qué evalúa un turno cortado

- **El corte del `triage` y los de salida evalúan las tres**, porque la llamada del `triage` ya corrió. Es lo que el equipo decidió para `explicit_request` y se cumple solo.
- **Propuesta, pendiente del grupo: los cortes de entrada no evalúan ninguna.** El `blocklist` y el `guardrail` de entrada ([ADR-0043](0043-prompt-injection-en-el-guardrail-de-entrada.md)) cortan **antes** del `triage`, y como §2.1 junta las tres señales en esa llamada, evaluarlas exigiría hacer la llamada igual. **Eso es mandarle al proveedor externo el texto que el corte existe para no procesar**, y en el caso de una inyección de prompt, darle al atacante exactamente la llamada que quería. Se anotan las tres como salteadas en el `detail_json` de la fila que cortó, como ya hace la regla 4 de ADR-0042. **Esto achica la decisión del equipo**, que era evaluar `explicit_request` en todo turno cortado: con §2.1, en estos dos cortes no se puede sin pagar lo anterior.

### 2.7 Una transferencia apaga el bot de esa conversación

**En la misma transacción que escribe `transfer_events`, el núcleo pone `conversations.mode = 'off'`**, por el agregado de `conversations/` ([08-structure-rules.md](../08-structure-rules.md), regla 1). La conversación sigue en `waiting` y con su vendedor ([ADR-0037](0037-toda-conversacion-nace-con-vendedor.md)). **La vuelve a prender un Operator**, con F27, como cualquier otro cambio de modo ([ADR-0035](0035-reglas-de-la-bandeja-manual.md)).

**Es el modo de la conversación y no `ai_agents.active`**: la pausa del agente IA apaga el bot en **todas** sus conversaciones ([ADR-0038](0038-alta-baja-y-pausa-del-agente-ia.md), regla 4), y una clienta mal atendida no puede dejar sin respuesta automática a las demás.

**Una señal que no produce `transfer_events` no apaga nada**: un `out_of_domain` con la política apagada deja la conversación en `waiting` y el bot prendido, como hoy. En modo `suggestion` no se transfiere y no se apaga nada ([ADR-0046](0046-modo-sugerencia.md)).

## 3. Justificación

**Por qué las tres en la llamada del `triage`.** Las tres miran el mismo bloque y se necesitan **antes** de generar: si la clienta pidió una persona, generar una respuesta es pagar por algo que no se va a mandar. Una llamada por señal sumaría dos llamadas al camino feliz, que es el más frecuente. Que compartan `classifier_version` es un costo real (§4), pero es el mismo que `out_of_domain` ya paga, y ninguna de las tres es el detector que mide OE4: ése es `frustration`, y por eso ADR-0044 le dio un instrumento propio.

**Por qué `no_resolution` es una racha y no un juicio suelto.** F12 dice *"turnos"*, en plural. Una sola respuesta que no resolvió es una respuesta mejorable; varias seguidas son un AIAgent que no puede con la conversación, que es cuando tiene sentido pasársela a una persona. La racha además hace que el umbral de la política sea un número con significado —*"después de cuántos intentos"*— y no un corte sobre una probabilidad.

**Por qué el LLM juzga y no una regla.** La alternativa barata era contar bloques seguidos con la misma intención en `detected_intents`. Se descartó por decisión del equipo: repetirse es también el síntoma de la frustración (ADR-0044), y una clienta que pregunta dos veces lo mismo porque le gustó la respuesta no es una conversación sin resolver.

**Por qué 1/0 con umbral 1.** Es la única de las dos salidas que no toca el esquema: la otra era que `threshold_used` admitiera `NULL` para las señales categóricas, y eso es una migración sobre una columna de la que ya hay filas. Con 1/0, la columna tiene **una** semántica —*"se comparó esto contra esto"*— y el barrido de umbral de [ADR-0014](0014-medicion-de-oe3-y-oe4.md) sigue siendo una consulta.

**Por qué el umbral por defecto en `triage.yaml`.** Sin política, escribir 0 hacía que la columna dijera *"se comparó contra 0"*, y para una racha eso es *"dispara siempre"*. Un default versionado es un número que alguien eligió y que entra en un `WHERE`, que es el argumento de la regla 9 de `08-structure-rules.md`.

**Por qué apagar el bot.** Transferir es decir *"esto lo atiende una persona"*. Si el AIAgent sigue contestando, la clienta recibe dos voces en la misma conversación, y la persona que llega a atenderla encuentra respuestas que no escribió. El modo de la conversación ya existe, lo ve el vendedor en la bandeja (F22) y lo cambia con F27: no hace falta un estado nuevo.

**Por qué cortar en vez de generar y después transferir.** Es lo que ADR-0042 §3 ya argumentó para `out_of_domain`: un texto automático compite con la respuesta de la persona que va a llegar. Con `explicit_request` el argumento es más fuerte, porque la clienta lo pidió.

## 4. Consecuencias

### Positivas

- **F12 tiene tres de sus cuatro señales**, y la cuarta está anotada como salteada en cada turno.
- **`threshold_used` tiene una sola semántica**, y el pendiente de la Fase 8 se cierra sin migración.
- **El seed tiene `transfer_policies`**, así que el corte del `triage` en el *stack* de demo escribe su `transfer_events`.
- **Una transferencia deja a la clienta con una sola voz**, y el vendedor ve en la bandeja que el bot se apagó.
- **Transferir no cuesta una generación.**

### Negativas / costos asumidos

- **Tres señales comparten `classifier_version`.** Editar el prompt del `triage` para mejorar `out_of_domain` mueve también `explicit_request` y `no_resolution`, y las evaluaciones de antes y de después dejan de ser comparables en las tres. Quien itere sobre una tiene que saber que invalida las otras dos.
- **La llamada del `triage` crece.** Recibe además la respuesta anterior del AIAgent y devuelve tres veredictos, así que un error de formato del modelo tira las tres señales juntas.
- **El administrador no puede ajustar el umbral de las señales de sí/no.** Lo que se puede mover es el prompt, que cambia la versión.
- **Mientras el ABM esté diferido, la regla de §2.4 no la hace cumplir nadie.** Un umbral sólo se cambia por SQL, y un 2 cargado en `explicit_request` hace que esa señal no dispare nunca, sin error.
- **`no_resolution` no se evalúa en modo `suggestion` ni después de que respondió un Operator**, porque no hay respuesta del AIAgent que juzgar. Es la misma forma de *"salteada"* que ADR-0044 ya asumió, con el mismo costo: sólo se ve leyendo `pipeline_steps`.
- **La racha no sobrevive a un cambio de prompt.** El día que cambia `triage.yaml`, todas las rachas en curso vuelven a cero.
- **Un turno cortado por `explicit_request` o `no_resolution` entra al denominador de OE6 como `in_domain` y sin escalada**, porque [ADR-0024](0024-unidad-y-alcance-de-oe6.md) cuenta como escalada sólo `out_of_domain`. **Ese bloque no fue respondido**, y OE6 dice *"respondidas sin escalada"*. Es la lectura benévola que ADR-0024 ya declaró para la frustración, extendida ahora a bloques que el AIAgent ni siquiera contestó. Hay que decirlo en el informe.
- **Una conversación transferida y que nadie atiende queda sin ninguna respuesta automática** hasta que un Operator la vuelva a prender.
- **Si §2.6 se acepta, un pedido de persona en un mensaje que la lista de bloqueo rechaza no se ve**: la clienta recibe el texto fijo del corte y la señal no queda evaluada.
- **La frontera de §2.3 depende del anotador**, y ADR-0021 §3 queda con el argumento debilitado que ADR-0044 ya declaró: el orden no cambia.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **`no_resolution` por repetición de intención en `detected_intents`** | Gratis y determinista, pero confunde repetirse con no ser resuelto, y es el síntoma de `frustration`. Lo descartó el equipo |
| **Una llamada propia por señal** | Dos llamadas más por turno sobre el camino feliz, a cambio de versiones independientes |
| **`explicit_request` por una lista de frases** | Gratis, pero *"¿hay alguien ahí?"* o *"pasame con el dueño"* no están en ninguna lista |
| **`threshold_used` `NULL` para las señales categóricas** | Dice la verdad sin convención, pero es una migración sobre una columna con filas, y obliga a toda consulta a tratar dos casos |
| **Generar y transferir en el mismo turno** | El AIAgent le contesta a una clienta que pidió una persona, y la generación se paga para nada |
| **Apagar el bot con `ai_agents.active`** | Apaga todas las conversaciones del agente IA por una sola |
| **Que el turno no responda mientras la conversación está en `waiting`** | No toca el modo, pero crea un segundo interruptor que el vendedor no ve ni puede prender, y que se confunde con los cortes que también dejan `waiting` sin transferir |
| **Evaluar las señales en los cortes de entrada con una llamada aparte** | Le manda al proveedor el texto que el corte rechazó, incluida la inyección que ADR-0043 corta |
