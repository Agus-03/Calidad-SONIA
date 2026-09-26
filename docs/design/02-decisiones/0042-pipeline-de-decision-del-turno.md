# ADR-0042 — El pipeline de decisión: qué corta un turno, qué le dice a la clienta y a quién se lo pasa

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · **decisión D3 y la tabla de §2.4 enmendadas por [ADR-0043](0043-prompt-injection-en-el-guardrail-de-entrada.md)**: el `guardrail` de entrada pasa a poder cortar, y sólo por una inyección de prompt. La redacción de ADR-0006 sigue sustituyendo y dejando seguir el turno, y la mitad de D3 que gobierna la corrida de salida no se toca |
| **Fecha** | 2026-09-25 · D3 enmendada el mismo día, al cerrar la fase |
| **Etapa** | Construcción — Fase 8 (pipeline de decisión) |
| **Resuelve** | Cuáles de los ocho pasos de [ADR-0016](0016-rastro-del-pipeline-de-respuesta.md) se construyen en esta fase y cuáles se difieren, qué corta un turno y qué no, qué lee la clienta cuando el pipeline la deja sin respuesta, y quién atiende lo que el AIAgent no contestó |
| **Requisitos afectados** | F01 (los seis componentes; que el `objective` sirva de dominio es lectura de este ADR — §1.1) · F02 · F12 · F29 (el texto fijo es un `messages` que no se puede borrar) · F34 · F36 · OE6 (el veredicto de `triage` es su denominador) |
| **Enmienda** | [ADR-0016](0016-rastro-del-pipeline-de-respuesta.md) — **`pipeline_steps.version` deja de nombrar un solo archivo.** En la corrida de salida del paso `blocklist` la columna escribe `<blocklist>+profanity<profanity>` —hoy `1+profanity1`—, porque ese paso decide con **dos** archivos versionados por separado, y quien los edita no es la misma persona: `blocklist.yaml` es la política del negocio y `profanity.yaml` es una guarda del equipo. La nota de [DER/entidades.md](../DER/entidades.md) para esa columna —*"Versión de la lista, la regla o el modelo que decidió"*— queda leída en plural para ese paso. **El argumento de la columna no cambia**: sigue siendo que ajustar una lista no puede invalidar en silencio las mediciones previas · [07-secuencia-del-turno.md](../07-secuencia-del-turno.md), **§4 B.1 paso 3** — *"**No escribe `messages`.** No hay respuesta automática"* — y **§4 B.2 paso 4** — *"No escribe `messages`"* — **dejan de ser ciertos**. Por decisión O2 cada punto de corte tiene un texto fijo, y el núcleo lo escribe como un mensaje con `sender_type = 'system'` y lo envía. La única excepción es el corte del `triage`, que sigue sin escribir nada (decisión O1c). **Lo que no cambia es el resto de las dos ramas**: `generations.message_id` queda NULL para siempre en B.2, y la respuesta que el pipeline rechazó no se envía nunca · [07-secuencia-del-turno.md](../07-secuencia-del-turno.md), **§1 — el bloque *Respuesta* del contrato** gana `notice_text`. Es el texto fijo de O2, y va en un campo propio y no en `completion_text` por el motivo que da §3 de este ADR · [ADR-0006](0006-anonimizacion-de-pii.md), §2 — *"Recibe el marcador: `[DNI]`, `[CUIT]`, `[CUIL]`, `[CBU]`"*: **son tres y no cuatro.** Por decisión D6, el patrón de once dígitos emite el marcador genérico `[CUIT/CUIL]`, que es la primera de las dos salidas que la propia nota de ADR-0006 dejó abiertas para el Paso 5. [03-capa-prompts.md](../03-capa-prompts.md) §4, que repite los cuatro marcadores, queda enmendado con él. **Lo que sigue abierto es la otra mitad de esa nota**: desambiguar CUIT de CUIL *al persistir*, donde el contexto suele indicarlo, no está construido y no lo decide este ADR |
| **Relacionada con** | [ADR-0006](0006-anonimizacion-de-pii.md), [ADR-0008](0008-ventana-de-24-horas.md), [ADR-0016](0016-rastro-del-pipeline-de-respuesta.md), [ADR-0020](0020-idempotencia-de-los-mensajes-salientes.md), [ADR-0021](0021-atribucion-entre-senales-simultaneas.md), [ADR-0024](0024-unidad-y-alcance-de-oe6.md), [ADR-0028](0028-cobertura-de-pruebas-por-servicio.md), [ADR-0041](0041-temporizador-del-turno-por-poller.md), [07-secuencia-del-turno.md](../07-secuencia-del-turno.md), [03-capa-prompts.md](../03-capa-prompts.md), [08-structure-rules.md](../08-structure-rules.md) |

---

## 1. Contexto

[ADR-0016](0016-rastro-del-pipeline-de-respuesta.md) declaró un pipeline de ocho pasos —*blocked-list → guardrail → triage → RAG → grounding (con reranking si hace falta) → blocked-list → guardrail → respuesta*— y la tabla donde se registra, `pipeline_steps`. **De los ocho, el catálogo modelaba uno y el código ejecutaba uno**: la generación. `pipeline_steps` existía desde la migración `0001` y **ninguna fila se había escrito nunca**.

Eso deja tres cosas sin resolver, y ninguna es un detalle de implementación:

1. **Contra qué compara el `triage`.** Es el paso del que sale el denominador de OE6, y la base de conocimiento —de donde saldría el dominio— no existe hasta la Fase 12.
2. **Qué pasa con un turno que el pipeline corta.** [07-secuencia-del-turno.md](../07-secuencia-del-turno.md) §4 describe las dos ramas de corte con la misma regla: el paso 3 de B.1 dice *"**No escribe `messages`.** No hay respuesta automática"* y el paso 4 de B.2 dice *"No escribe `messages`"*. Escrito así, una clienta que dice algo que el filtro rechaza **no recibe nada**, y consume la ventana de 24 horas de [ADR-0008](0008-ventana-de-24-horas.md) sin saber si su mensaje llegó.
3. **Quién atiende lo que el AIAgent no contesta.** F12 declara la transferencia por señales y [ADR-0024](0024-unidad-y-alcance-de-oe6.md) define *"escalada"* como una transferencia, pero nada decía si el veredicto del `triage` es una de esas señales o sólo evidencia.

Hay además una decisión de alcance que no se podía seguir postergando. La conversación de diseño de esta fase abrió **trece preguntas**, que se numeraron O1 a O10 —con O1b, O1c y O6b derivadas de las suyas— y que están transcriptas una por una en la tabla de §2.3, con lo que se decidió y en qué fase se construye. **Cinco se construyen acá** (O1, O1b, O1c, O2 y O6b) y una sexta se decide acá con ADR propio (O3); el resto son puertas que conviene cerrar ahora, porque cada una que queda abierta es código que se escribe sobre una lectura que nadie confirmó.

### 1.1. Qué es "el dominio": una contradicción del anteproyecto que este ADR no resuelve

**F12 y OE6 no hablan del mismo dominio, y hay que decirlo antes de que el código elija por omisión.** F12 pide detectar *"consultas fuera del **dominio RAG**"*; OE6 pide que *"el 90 % de las consultas dentro del **dominio del negocio** sean respondidas sin escalada"*. Son dos cosas: el dominio del RAG es lo que hay cargado en la base de conocimiento, y el dominio del negocio es de qué se ocupa la inmobiliaria —que la incluye y la excede—.

La decisión D1 resuelve **por el segundo** —el `objective` que escribió el administrador— y escribe la señal del primero, `signal_evaluations.signal_type = 'out_of_domain'`. El motivo inmediato es que la base de conocimiento no existe hasta la Fase 12, así que el dominio del RAG no es evaluable todavía. **Pero conviene no presentarlo como una consecuencia de ese calendario:** incluso con la base cargada, medir OE6 contra el dominio del RAG haría que un documento faltante se lea como una consulta fuera del negocio, que es lo contrario de lo que OE6 quiere contar.

**Lo que este ADR hace es elegir, no reconciliar.** El día que exista el RAG hay que decidir si la señal `out_of_domain` se sigue evaluando contra el `objective` —y entonces su nombre miente respecto de la letra de F12— o si se parte en dos. Queda registrado como pendiente y no resuelto acá.

---

## 2. Decisión

### 2.1 El pipeline que corre hoy

De los ocho pasos de ADR-0016 se construyeron **cinco**, en dos tramos alrededor de la generación:

```
entrada:  blocklist → guardrail → triage
          generation
salida:   blocklist → guardrail
```

`position` cuenta a través de los dos tramos y de la generación, así que el rastro de un turno se lee en el orden en que pasaron las cosas: `1..6` cuando nada corta. **`retrieval` y `grounding` no se crean en esta fase**: necesitan Qdrant, embeddings y `retrieval_logs`, y se construyen en la Fase 12 (RAG). El árbol ya tenía escrita la regla que se aplica —*"`reranking.py` no se crea hasta que exista"*, [docs/struct.md](../../struct.md)— y se aplicó igual a los otros dos.

### 2.2 Las seis decisiones de construcción

| # | Decisión | Dónde vive |
|---|---|---|
| **D1** | **El `triage` compara contra el componente `objective` del AIAgent**, que es uno de los seis que enumera F01 y que el turno ya tiene cargado para el ensamblado. **F01 no dice que el objetivo defina el dominio: eso es lectura de este ADR** (§1.1). `triage.yaml` guarda la instrucción y las dos etiquetas, **no** una lista de dominios: el dominio es configuración del administrador y no código | `pipeline/triage.py`, `pipeline/step.py` |
| **D2** | **En esta fase, `retrieval` y `grounding` no escriben ninguna fila**, ni real ni *placeholder*. Una fila constante `grounded` la leería como verdadera la consulta de diagnóstico de OE6, que agrupa por `ps_grounding.outcome`. **Es una decisión acotada a la Fase 8 y no una enmienda a [ADR-0016](0016-rastro-del-pipeline-de-respuesta.md) §4**, que declara a `retrieval` como paso *"para que el rastro se pueda leer completo y en orden"*: la Fase 12 los escribe, sin tener que enmendar nada | — (por ausencia) |
| **D3** | **Las dos corridas del `guardrail` encuentran lo mismo y hacen lo opuesto.** A la entrada sustituye el dato y deja seguir el turno (`modified`); a la salida **rechaza la respuesta entera** (`blocked`) y nunca la reescribe, porque ese texto ya pasó por el proveedor externo y reescribirlo no protegería nada de lo que le importa a [ADR-0006](0006-anonimizacion-de-pii.md) | `pipeline/guardrail.py` |
| **D4** | **`blocklist.yaml` se despliega con el mecanismo completo y la lista vacía.** Los términos de prueba viven en los tests. Es la política de quien maneja el negocio y no del equipo | `pipeline/blocklist.py`, `rules/blocklist.yaml` |
| **D5** | **El paso `generation` escribe su fila**, con `outcome` en `ok`/`failed`, y su `version` sale de `prompt.rules_version`: lo que decidió cómo se preguntó esa generación es el ensamblado, y el modelo que respondió ya es una columna de `generations` | `api/routes/turns.py` |
| **D6** | **El CUIT/CUIL se redacta con el marcador genérico `[CUIT/CUIL]`**, que es la primera de las dos salidas que la nota de ADR-0006 había dejado abiertas | `rules/guardrails.yaml` |

### 2.3 Las decisiones de alcance

| # | Pregunta | Decisión | Se construye |
|---|---|---|---|
| **O1** | ¿`out_of_domain` corta el turno? | **Sí, lo corta**, y el turno va a una persona. Es lo contrario de lo que se había recomendado — ver §3 | Fase 8 |
| **O1b** | Un saludo no es `in_domain` ni `out_of_domain`, y el dominio de `triage.outcome` sólo admite esos dos | **No se agrega un tercer valor**, también al revés de lo recomendado (ver §5). Un saludo es `in_domain` como todo lo que este negocio atiende, y se marca en `detail_json.greeting`, que es lo que le permite a la medición de OE6 dejar los saludos afuera de su denominador | Fase 8 |
| **O1c** | Ante `out_of_domain`: ¿texto fijo, o se lo pasamos a una persona? | **Se lo pasamos a una persona, y a la clienta no se le dice nada**; la recomendación era hacer las dos cosas (ver §3). Es el único corte que no tiene texto, y `rules/notices.yaml` **se niega a cargar** una entrada para el `triage` | Fase 8 |
| **O2** | ¿La clienta recibe una respuesta fija en cada corte, o silencio? | **Respuesta fija por punto de corte**, versionada en `rules/notices.yaml`. El silencio consume la ventana de 24 h de [ADR-0008](0008-ventana-de-24-horas.md) sin que la clienta sepa si llegó su mensaje | Fase 8 |
| **O3** | ¿El guardrail de entrada detecta *prompt injection*? | **Sí, y con ADR propio.** Hoy no hay ninguna capa que lo mire, y es la razón por la que `guardrail → blocked` existe en el dominio de `outcome` y quedó inalcanzable a la entrada | ADR aparte, **esta fase** |
| **O4** | ¿Segunda corrida de `grounding` después de generar, contra alucinación? | **Sí.** El grounding previo no puede detectar una alucinación; éste sí, y entra sin DDL como `direction = 'outbound'`. **Exige enmendar ADR-0016** cuando se construya | Fase 12 |
| **O5** | ¿Qué hace `not_grounded`? | **Transferencia por señal de F12**, no un corte mudo. Es como [ADR-0024](0024-unidad-y-alcance-de-oe6.md) define la escalada | Fase 12 |
| **O6** | ¿El guardrail de salida valida tono? | **No.** Bloquear por tono vuelve circular OE2 y OE7: sólo se mediría lo ya aprobado. El tono se juzga después, en `tone_evaluations` | — (puerta cerrada) |
| **O6b** | Las malas palabras en la salida | **Sí, y como datos**: viven en `rules/profanity.yaml`, un archivo **aparte** de `blocklist.yaml`, con su propia `version`, y sólo los lee la corrida de salida. La recomendación era llenar la lista vacía de `blocklist.yaml` (ver §3) | Fase 8 |
| **O7** | ¿Un orquestador que delega a varios RAG, o un *Search Service* aparte? | **No.** F01 configura *un* agente y ADR-0017 fijó dos desplegables. Es alcance nuevo sin requisito que lo sostenga | — (puerta cerrada) |
| **O8** | ¿Job de indexación periódica? | **Sí, pero sólo para reindexar** por cambio de modelo de embeddings. La ingesta por evento de F06 no se toca | Fase 12 |
| **O9** | `retrieval_logs.generation_id` es FK a `generations`: un turno que recupera y corta por `not_grounded` no tiene dónde registrar lo recuperado | **Queda anotado acá como consecuencia conocida y se resuelve en la Fase 12**, junto con O5: son el mismo problema visto de dos lados | Fase 12 |
| **O10** | [07-secuencia-del-turno.md](../07-secuencia-del-turno.md) §0 lista `contact_attributes` entre lo que escribe el cerebro, y la regla 3 de [08-structure-rules.md](../08-structure-rules.md) lo prohíbe | **Queda registrada como contradicción abierta.** Es anterior a esta fase y bloquea el *checkpoint* de la regla 4 de [03-capa-prompts.md](../03-capa-prompts.md), que es quien debería redactar antes que el guardrail | Sin resolver |

### 2.4 Lo que escribe cada corte

| Corte | A la clienta | Evidencia | Conversación |
|---|---|---|---|
| `blocklist` de entrada | Texto fijo, como mensaje `system` | `pipeline_steps` hasta el corte. **Sin generación** | `waiting` · `processed_at` puesto |
| `triage` → `out_of_domain` | **Nada** (O1c) | `pipeline_steps` + una fila en `signal_evaluations` con la señal `out_of_domain`, y un `transfer_events` si la política del agente la tiene encendida. **Las otras tres señales de F12 no se evalúan, y el corte lo registra** — ver la regla 4 de abajo | `waiting` · `processed_at` puesto |
| `blocklist` o `guardrail` de salida | Texto fijo, como mensaje `system` | `pipeline_steps` completos + `generations` con `status = 'blocked'` y `message_id` NULL para siempre | `waiting` · `processed_at` puesto |

Tres reglas que atraviesan la tabla:

1. **El texto fijo lo escribe el núcleo, no el cerebro**, y lo escribe con `sender_type = 'system'`: es el tercer brazo del *check* de `messages` (H-09). Llamarlo `ai_agent` sería ponerle palabras en la boca a un modelo que no las produjo, y eso toca directamente lo que mide OE2.
2. **Un corte no es una falla.** `processed_at` queda puesto y `error_message` en NULL: el turno se procesó y su resultado fue no responder. Lo que distingue un turno que nadie terminó (§5) sigue siendo `processed_at` NULL.
3. **Sólo el `triage` delega.** Una lista de bloqueo que rechazó un término no es una pregunta que otra persona pueda responder mejor: es un mensaje que este negocio no toma, y O2 ya lo contesta con un texto.
4. **Un turno cortado evalúa una señal de cuatro, y anota las tres que no evaluó en el `detail_json` de la fila del paso que cortó.** Es la salida que [ADR-0014](0014-medicion-de-oe3-y-oe4.md) dejó escrita para este caso —*"si en algún momento se decide cortocircuitar, hay que registrar explícitamente cuáles se saltearon"*—, así que **no hay nada que enmendarle**. No se escriben tres filas de `signal_evaluations` marcadas como salteadas porque `score`, `threshold_used` y `classifier_version` son `NOT NULL`: una señal que nadie evaluó no tiene ninguno de los tres, y ponerle números llenaría de ficciones la tabla que leen OE3 y OE4. Y la regla de ADR-0014 (c) se sostiene tal como está escrita: *"una fila por cada señal de F12 **evaluada**"*.

---

## 3. Justificación

**Por qué el `triage` corta, contra la recomendación.** La recomendación era que no cortara —que fuera un veredicto que se registra y cambia el prompt— y su único argumento era un costo: *"si corta, un «hola» termina en la bandeja de un operador"*. **O1b eliminó ese costo**: un saludo es `in_domain` y nunca sale por esta rama. Sin ese argumento, no cortar significa que el AIAgent **contesta igual** una pregunta que acaba de clasificar como ajena al negocio, con un prompt armado para otra cosa. Eso es exactamente lo que F12 quiere evitar y lo que OE6 mide.

**Por qué ese corte es silencioso y los otros dos no.** Los tres cortes no son la misma situación. En los dos de lista y guardrail, **hay algo que el sistema se niega a hacer**, y decirlo es lo mínimo: la clienta escribió y la ventana de 24 h corre. En el del `triage`, la pregunta **va a ser contestada**, por una persona, en la misma conversación: un texto automático que anuncia la derivación compite con la respuesta real que está llegando y le agrega una promesa de tiempo que nadie puede sostener. El silencio acá no es abandono, es no interrumpir.

**Por qué las malas palabras van en su propio archivo.** La recomendación de O6b era llenar la lista `outbound.terms` de `blocklist.yaml`, que D4 deja vacía. Son dos cosas con dos dueños: esa lista es la política de quien maneja el negocio, y ésta es una guarda del equipo, porque un insulto en una respuesta es una exposición legal y no una cuestión de gusto. Mezclarlas esconde el día en que alguien escribe un término nuestro en el archivo del negocio. Tener dos archivos obliga a que `pipeline_steps.version` nombre a los dos —de ahí la enmienda—, y ese es precisamente el argumento de la columna: cargar una palabra cambia lo que el paso decide, así que tiene que mover la versión.

**Por qué la comparación es por palabra completa.** El paso normaliza mayúsculas y acentos y compara con *lookarounds* de límite de palabra: `culo` no coincide dentro de `músculo` ni de `vínculo`, y sí coincide en `¡CULO!`, porque la puntuación no es una letra. Sin esa exactitud, una entrada corta amordaza al AIAgent en respuestas legítimas —y el costo de un falso positivo no es un filtro que no filtró: es un turno que termina **después** de haber pagado la generación, con la clienta sin respuesta.

**Por qué la generación escribe su propia fila (D5).** Sin ella el rastro salta de la posición 3 a la 4 por encima de lo único del turno que costó dinero, y un turno que se cayó generando no dice dónde se cayó. La `version` sale del ensamblado y no de un archivo de reglas propio, porque no existe tal archivo: lo que decidió cómo se preguntó es el prompt.

**Por qué el texto fijo viaja en su propio campo del contrato.** `TurnResponse` gana `notice_text` y no reusa `completion_text`. §4 B.2 no exige nada sobre el contrato —dice que el núcleo no escribe `messages` y que `message_id` queda NULL—, así que **lo que sigue es lectura de este ADR y no una cita suya**: si una generación rechazada nunca puede llegar a la clienta, el contrato es el lugar donde eso se garantiza. Si el texto fijo viajara en el mismo campo, la diferencia entre "enviá esto" y "no envíes esto" quedaría en la disciplina del núcleo en vez de en el contrato. `notice_text` en NULL es el silencio de O1c, y es la única forma en que ese silencio es un valor y no un olvido.

---

## 4. Consecuencias

### Positivas

- **`pipeline_steps` deja de ser una tabla vacía.** Un turno completo escribe seis filas con su latencia, su veredicto y su versión, y el denominador de OE6 —el veredicto de `triage`— existe por primera vez como dato.
- **Los tres cortes son auditables y distinguibles entre sí**, y distinguibles de un turno fallido: la combinación de `processed_at`, `error_message` y la presencia o ausencia de `generations` dice cuál de los cuatro fue.
- **El dominio del agente es configuración y no código** (D1): cambiar de qué habla el AIAgent es editar su `objective`, sin desplegar.
- **La lista de malas palabras se carga sin tocar Python** y sin tocar la política del negocio, y cada carga queda fechada en la evidencia por la versión compuesta.
- **Una pregunta fuera del dominio llega a una persona con la evidencia que explica por qué**, que es lo que F36 cuenta y lo que [ADR-0024](0024-unidad-y-alcance-de-oe6.md) necesita para medir *"sin escalada"*.

### Negativas / costos asumidos

- **El rastro de esta fase tiene un agujero visible** donde deberían estar `retrieval` y `grounding`: dos pasos declarados en ADR-0016 sin ninguna fila, entre el `triage` y la generación. Hay que conocer la fase para saber por qué. Es el costo de D2, agravado por haber movido los dos módulos enteros a la Fase 12: D2 decidía que existieran **con el contrato del paso y sin registrar**, y ni eso existe.
- **Un agente con el `objective` vacío no tiene dominio, y el paso deja pasar.** Se eligió pasar antes que rechazar porque lo contrario callaría al agente entero, pero significa que un administrador que no escribió su objetivo **no tiene triage**, y sus turnos entran al denominador de OE6 sin que nada lo señale.
- **Una llamada más al proveedor por turno** (el `triage`), sobre el camino feliz, que es el más frecuente. Sale del presupuesto de latencia del turno y del costo por turno, que **son criterios del equipo y no una métrica del anteproyecto**: OE2 mide consistencia de tono, y ninguno de los siete objetivos pone un número al costo.
- **Toda pregunta fuera del dominio ocupa a una persona.** Un chiste al bot genera trabajo humano, y nada limita cuántas veces. Y hay un efecto de segundo orden que conviene decir: [ADR-0024](0024-unidad-y-alcance-de-oe6.md) saca del denominador de OE6 *"los turnos posteriores a una escalada"*, así que **un solo chiste fuera de dominio saca de la medición todo el resto de esa conversación**.
- **OE6 va a dar 100 % hasta la Fase 12, y no porque el AIAgent resuelva todo.** La unidad de OE6 es el bloque ([ADR-0024](0024-unidad-y-alcance-de-oe6.md)), su denominador son los bloques con veredicto `in_domain`, y *"sin escalada"* está definido ahí como *"sin transferencia de tipo `out_of_domain`"*. Con O1, **el único productor de una transferencia `out_of_domain` es un bloque cuyo veredicto fue `out_of_domain`**, que por definición no está en el denominador: numerador y denominador coinciden por construcción. La métrica empieza a discriminar cuando exista O5 —`not_grounded` como transferencia, Fase 12—. **Esta fase habilita el denominador de OE6, no la métrica**, y presentarla como habilitada sería la circularidad que ADR-0016 quiso evitar.
- **El texto fijo pone `conversations.first_response_at` si no se lo impide.** Es un saliente que Meta aceptó, así que sin una regla explícita una conversación que nadie contestó reportaría un tiempo de respuesta de segundos (H-12, F34). **Se decidió que el texto fijo no cuenta como respuesta**: el núcleo no toca esa columna cuando el emisor es `system`, y la consulta F34.b lo excluye de su agrupación. El costo es que F34 deja de contar esas conversaciones entre las respondidas, que en sentido literal es lo que son.
- **El texto fijo no se puede corregir ni borrar** (F29, C09): es una fila de `messages`, y F29 registra la limitación de Meta de que un mensaje enviado no se elimina. Un texto mal redactado que ya salió queda en el hilo de esa clienta para siempre, y lo único que se arregla es el archivo, para la próxima.
- **Las tres señales que un turno cortado no evalúa quedan anotadas en un `jsonb` de otra tabla.** Cumple con [ADR-0014](0014-medicion-de-oe3-y-oe4.md), pero una consulta que quiera saber a qué bloques les falta qué evaluación tiene que leer `pipeline_steps` y no sólo `signal_evaluations`, y quien mire sólo la segunda no distingue *"salteada"* de *"todavía sin clasificar"*. **Y hay algo peor que conviene dejar dicho antes de la Fase 9:** `frustration` y `explicit_request` **son evaluables** sobre el mensaje de la clienta sin que el turno haya respondido —sólo `no_resolution` no lo es—, así que saltearlas deja sin evaluación de frustración justamente a los bloques donde el AIAgent le dijo que no a la clienta, que son los más probables de traerla. Es el falso negativo que ADR-0014 existe para vigilar, reabierto en una rebanada. **Hay que decidirlo al construir las tres señales.**
- **El texto fijo le confirma a quien sondea el sistema que hay un filtro.** Los tres textos se escribieron con la misma extensión de promesa a propósito, para que lo que lee la clienta no revele **cuál** de nuestros controles se disparó, pero que existe un control es inevitable decirlo.
- **En producción la lista de bloqueo del negocio no bloquea nada** hasta que alguien la llene (D4), y eso tiene que estar dicho en el informe: el mecanismo está probado, la política está vacía.
- **Las derivaciones de una mala palabra necesitan su propia línea**, y las evasiones no se detectan: `c u l o` y `cu1o` pasan. Es una lista, no un clasificador, y se eligió sabiendo eso.
- **Dos pasos del mismo turno pueden reportar el mismo `version` refiriéndose a archivos distintos** (D5): el `guardrail` nombra `guardrails.yaml` y la generación nombra el ensamblado, y los dos pueden decir `1`.
- **Un móvil argentino con prefijo 9 se redacta con el nombre equivocado** (D6): el patrón de CUIT/CUIL lo alcanza, así que el marcador dice `[CUIT/CUIL]` donde había un teléfono. El dato queda protegido; la etiqueta miente.
- **Un dato personal puede quedar textual en `generations.completion_text` de una respuesta bloqueada.** Se decidió que se queda: el argumento de ADR-0006 es sobre `prompt_rendered`, que lleva lo que escribió la clienta, y el guardrail de entrada ya lo redactó; a `completion_text` un dato sólo llega por un componente de prompt —el CBU de la propia inmobiliaria— o por alucinación, y ninguno de los dos es el dato de la clienta. Redactarlo destruiría la única razón por la que §4.2 conserva la fila. **El costo real:** si el *checkpoint* de la regla 4 tiene un falso negativo **y** el modelo repite el dato, ese valor queda en claro en la tabla. Es lo que ADR-0006 pide medir, no prometer.
- **Un turno generado no actúa sobre una propuesta de transferencia.** Hoy sólo el corte del `triage` la produce; las otras tres señales de F12 y la atribución de [ADR-0021](0021-atribucion-entre-senales-simultaneas.md) son la Fase 9.
- **Una señal apagada o sin configurar escribe la evidencia y no propone nada**, y la conversación cae igual a `waiting`. La delegación de O1c se sostiene; lo que se pierde es la fila de `transfer_events` que F36 cuenta.
- **Un paso que empiece a cortar en la Fase 12 y no tenga entrada en `notices.yaml` se queda callado sin avisar.** El archivo declara qué pasos no cortan hoy, pero nada falla al agregar un corte nuevo y olvidar su texto.
- **El *smoke* sólo alcanza uno de los tres cortes.** Con los archivos que se despliegan, `blocklist.yaml` está vacío (D4) y el `triage` doblado siempre responde `in_domain`, así que el único corte que un *stack* doblado puede recorrer es el de la lista de salida. Los otros dos están cubiertos por tests unitarios y **nunca se ejecutaron de punta a punta**.
- **El *checkpoint* de la regla 4 sigue sin existir** (O10), así que la red del guardrail de entrada es hoy lo único que sustituye algo, y todo mensaje con un número de documento sale de ahí como `modified`. Es un agujero y no un diseño.

---

## 5. Alternativas descartadas

| Alternativa | Por qué no |
|---|---|
| **Un tercer valor `social` en `triage.outcome`** para los saludos | Era la recomendación de O1b, y exigía enmendar ADR-0016 y el DER con sus tres registros antes de escribir una línea. `detail_json.greeting` deja OE6 igual de honesto —la medición excluye los saludos por esa marca— sin tocar un dominio del que ya dependen consultas escritas. El costo asumido es que la exclusión vive en la consulta y no en el dato: una medición que se olvide del filtro infla OE6 y nada la frena. |
| **Texto fijo también para el corte del `triage`** | Era la recomendación de O1c: texto fijo inmediato **y** transferencia, para que la clienta no quede muda y el caso no se pierda. Se descartó la primera mitad, porque compite con la respuesta de la persona que ya está atendiendo y agrega una promesa de tiempo que nadie sostiene. |
| **Llenar `outbound.terms` de `blocklist.yaml`** en vez de un archivo aparte | Mezcla la política del negocio con una guarda del equipo, y esconde el día en que alguien escribe un término nuestro en el archivo del negocio. Se pagó con una enmienda a ADR-0016. |
| **Filas *placeholder* de `retrieval` y `grounding`** | Una fila constante `grounded` la lee como verdadera la consulta de diagnóstico de OE6. Un agujero visible es preferible a una fila que miente. |
| **Un `guardrail` de salida que devuelva `modified`** | Reescribir una respuesta que ya pasó por el proveedor externo no protege nada de lo que le importa a ADR-0006 —el proveedor ya vio el dato— y sólo dejaría un marcador donde estaba el número de la clienta. Además, una respuesta modificada rompe la tasa de edición de F02. |
| **Validar tono en el `guardrail` de salida** (O6) | Bloquear por tono vuelve circular OE2 y OE7: sólo se mediría lo que ya fue aprobado por el mismo criterio. Es el mismo error que ADR-0016 rechazó para OE6. |
| **Un orquestador sobre varios RAG, o un *Search Service* aparte** (O7) | F01 configura *un* agente y ADR-0017 fijó dos desplegables. Alcance nuevo sin requisito que lo sostenga. |
| **Escribir el texto fijo como `sender_type = 'ai_agent'`** | Le atribuiría al modelo un texto que no produjo, justo en la tabla desde la que se mide su desempeño (OE2). El *check* de `messages` ya admitía el tercer brazo, así que no costó DDL. |
| **Reusar `completion_text` para el texto fijo** | Una respuesta rechazada no puede ser enviable por quien llama, que es lectura de este ADR sobre §4 B.2 y no una cita suya. En un solo campo, esa garantía dependería de la disciplina del núcleo en vez del contrato. |
