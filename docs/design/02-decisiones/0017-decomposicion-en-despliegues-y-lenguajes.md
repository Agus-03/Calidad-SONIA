# ADR-0017 — Decomposición en despliegues y reparto de lenguajes

| Campo | Valor |
|---|---|
| **Estado** | **Aceptada** — firmada por los tres autores el 2026-09-05 · (k) acotada por [ADR-0040](0040-previsualizacion-del-prompt.md), regla 5: la previsualización no se registra en `audit_logs` · **§3, §4, §5 y §6 enmendadas por [ADR-0041](0041-temporizador-del-turno-por-poller.md) (2026-09-20)**: el timer de F10 deja de ser *"estado en memoria del proceso"* y pasa a una columna con un poller, así que **deja de valer como argumento para elegir Go** —§3, y el descarte del monolito Python en §5—, deja de ser el motivo por el que el núcleo no escala horizontalmente —§4, donde el techo real pasa a ser el fan-out de WebSockets de F21—, y `turn/` ya no es *"time.Timer + canales"* —§6—. **La decisión no se revierte**: Go sigue por el gateway, F21 y el interés declarado del equipo · **la fila F07 de §6 enmendada por [ADR-0061](0061-respuestas-rapidas-en-el-prompt.md) (2026-10-02)**: el catálogo lo lee `prompts/repository.py` y lo ensambla `prompts/assembler.py`, no `pipeline/generation.py` · **las filas F05, F21 y F26 de §6 enmendadas por [ADR-0063](0063-paquetes-por-razon-de-cambio.md) (2026-10-02)**: F05 vive en `reasoning/`, el fan-out de F21 en `realtime/` y las notas de F26 en `notes/` |
| **Fecha** | 2026-08-16 · **revisada 2026-08-22** (ver §0) · **firmada 2026-09-05** |
| **Etapa** | Diseño — antes del Paso 7 (nivel físico) |
| **Resuelve** | La sección 8.4 deja el lenguaje sin decidir (*"Python o GO"*, *"Go o Python"*) y 8.5 no dice si los módulos son procesos separados |
| **Requisitos afectados** | Transversal · F06 (ingesta) · F03 (previsualización) · OE1 · OE2 (cadena de trazabilidad) · OE5 (cobertura) |
| **Naturaleza** | Decisión de ingeniería del equipo dentro del espacio que 8.4 dejó abierto. **Matiza el texto del anteproyecto** en los puntos (b) y (d) |

---

## 0. Revisión del 2026-08-22 — qué cambió y por qué

La versión del 16/08 se escribió **antes** de recorrer los 39 requisitos uno por uno. Ese recorrido posterior encontró un hueco y tres decisiones que faltaban. Se corrige acá, **antes de firmar**, y no en un ADR nuevo: la regla del [README](README.md) —*"un ADR no se edita cuando se cambia de opinión"*— protege el historial de las decisiones **revertidas**, y ésta nunca llegó a aceptarse. Un equipo que tiene que leer dos documentos para acordar una sola decisión acuerda peor.

| Qué cambió | Motivo |
|---|---|
| **(b) pasa de dos desplegables a tres** | El frontend es un artefacto real y estaba contado como cero. Y faltaba el modo `worker` |
| **Se agrega (g) — el trabajador de ingesta** | **Era un hueco:** la versión anterior ubicaba *"pipeline RAG"* en el cerebro sin decir cómo entra un documento de 100 MB. Con dos procesos sincrónicos, **F06 no tenía dónde ocurrir** |
| **Se agrega (h) — el ensamblador único** | Sin declararlo, la reimplementación en Go aparece sola ("son seis textos concatenados") y **OE1 pasaría a medir un render distinto del que ve el administrador** |
| **Se agrega (i) — tres reglas de la base compartida** | La §1 de la versión anterior **detectaba** que `conversations` y `messages` las escriben los tres módulos, y después no resolvía quién las escribe con dos servicios. Detectar sin resolver deja la condición de carrera escrita y sin dueño |
| **Se agrega (j) — el nombre de la arquitectura** | Hace falta para el informe y para la defensa |
| **Se agrega el §6 — reparto por requisito** | El punto (d) afirmaba que el Módulo 2 se parte. Ahora está contado, para los 39 |

**Lo que no cambió:** la frontera Go/Python, la base única, el reparto del Módulo 2 y todas las alternativas descartadas. La decisión de fondo se sostuvo al recorrer los requisitos; lo que faltaba era completitud.

### Segunda pasada del 22/08 — lo que encontró el Paso 8

El recorrido de requisitos revisó **qué** hace cada servicio. Dibujar la secuencia del turno ([07-secuencia-del-turno.md](../07-secuencia-del-turno.md)) revisó **en qué orden y quién escribe cada fila**, y encontró tres cosas más — dos de ellas en el punto (i), que es el que sostiene la base compartida.

| Qué cambió | Motivo |
|---|---|
| **(i).2 pasa de "una tabla, un servicio" a "una columna, un servicio"** | La regla anterior era **falsa contra el propio diagrama de secuencia**: el núcleo tiene que escribir `generations.message_id` —enlaza el mensaje que acaba de crear— y los campos de revisión de F02. Con la regla literal, o `message_id` queda NULL para siempre (y se rompen la tasa de edición de F02 y la cadena de OE1), o la aceptación de una sugerencia se rutea por el cerebro, **con lo que una caída del cerebro impediría a un operador enviar una sugerencia que ya tiene en pantalla** |
| **(i).2 declara el tercer escritor** | El instrumental de evaluación escribe `conversation_annotations` y `tone_evaluations`, y no figuraba. Una lista cerrada a la que le falta un escritor no es una lista: o las anotaciones de OE3/OE4 no tienen quién las escriba, o la regla no rige |
| **(i).3 afirmaba algo falso** | Decía que `generations.status` *"ya tiene los valores necesarios"*. No los tenía: le faltaba `blocked`, para la respuesta que frena el guardrail de salida de ADR-0016. Es **H-26**, hallazgo del Paso 8 |

> **Que las dos pasadas encontraran cosas distintas no es casualidad.** Recorrer requisitos contesta *"¿está todo asignado?"*; dibujar la secuencia contesta *"¿el orden de las escrituras es consistente?"*. Son preguntas distintas y ninguna cubre a la otra — la primera encontró un proceso que faltaba (la ingesta), la segunda encontró una regla que no se podía cumplir. **Conviene hacer las dos antes de firmar**, y ésa es la razón por la que este ADR estuvo en Propuesta durante dos revisiones, hasta firmarse el 2026-09-05.

---

## 1. Contexto

El anteproyecto fija la arquitectura funcional y deja abierta la de despliegue.

**Sección 8.5** describe *"tres **módulos integrados** con responsabilidades claramente separadas"* —Gateway, Módulo 2 (agregador y clasificador), Módulo 1 (motor de prompts), Módulo 3 (plataforma operativa)— más *"Persistencia (PostgreSQL + Vector DB): almacena **todos** los estados del sistema"*.

**Sección 8.4** asigna lenguajes sin decidirlos: *"Python o GO"* para los Módulos 1 y 2, *"Go (Golang) o Python"* para el Módulo 3, con la justificación *"goroutines para concurrencia masiva de webhooks y WebSockets sin overhead de threading"*.

**El anteproyecto mezcla tres preguntas distintas que conviene separar:**

1. ¿Cómo se reparten las **responsabilidades**? → 8.5 ya la responde: tres módulos.
2. ¿Cuántos **procesos desplegables** hay? → sin responder.
3. ¿En qué **lenguaje** se escribe cada cosa? → sin responder.

Responder (2) y (3) copiando la respuesta de (1) es lo natural, y es lo que este ADR argumenta que no conviene.

### 1.1. Lo que aparece al mirar quién escribe cada tabla

| Tabla | Módulos que escriben |
|---|---|
| `message_batches`, `detected_intents`, `signal_evaluations`, `transfer_events` | M2 |
| `conversation_contexts`, `generations`, `retrieval_logs`, `pipeline_steps` | M1 |
| `contacts`, `operators`, `labels`, `sales`, configuración de agentes | M3 |
| **`conversations`** | **M2** (estado, transferencia) · **M3** (asignación, modo, destacado) · **M1** (actividad) |
| **`messages`** | **Gateway/M2** (entrantes) · **M1** (respuesta de la IA) · **M3** (respuesta del operador) |

**Las dos tablas del núcleo las escriben los tres módulos.** Es la señal clásica de que la frontera modular no sigue al dato: el agregado central es compartido. **El punto (i) lo resuelve entre los dos servicios**; la versión anterior de este ADR lo dejaba señalado y abierto.

> **Y reaparece un nivel más abajo (2026-09-03).** Dentro del núcleo, `conversations` y `messages` las escriben **tres paquetes**: `turn/` (el mensaje entrante y la respuesta de la IA), `inbox/` (la del operador, más `starred`, `mode` y `operator_id`) y `whatsapp/` (los webhooks que mueven `messages.status`). La regla *"un paquete = las tablas que escribe"* no alcanza cuando tres lo hacen. **Se resuelve declarando al agregado compartido como paquete propio, `conversations/`**, dueño de las dos tablas; los otros tres escriben por su repositorio. La regla queda **un paquete = las tablas de las que es dueño**, que es el mismo movimiento que (i).2 hizo al pasar de *una tabla, un servicio* a *una columna, un servicio*: el enunciado se vuelve más estricto y verdadero en lugar de más simple y falso.
>
> **Lo que hace posible cumplirla es una transacción que cruce paquetes.** [07-secuencia-del-turno.md](../07-secuencia-del-turno.md) exige que `message_batches.processed_at` se escriba **en la misma transacción** que el mensaje saliente, `generations.message_id` y `transfer_events` — si no, un reinicio del núcleo no puede distinguir un turno terminado de uno que nunca se procesó. Por eso los repositorios reciben un `Executor` de `platform/db` que abstrae el pool y `pgx.Tx`, y **la transacción la abre quien conduce el turno, no el repositorio**. Esto no toca (i).3: **ninguna transacción cruza el borde entre los dos servicios.**

### 1.2. La inferencia está en los tres módulos, no en uno

| Módulo | Requisitos que llaman a un modelo |
|---|---|
| Módulo 1 | F04 (reescritura), F06 (búsqueda semántica), F08 (sugerencia de reformulación) |
| Módulo 2 | F11 (chunking por intenciones), F12 (clasificador de transferencia) |
| Módulo 3 | **F31** — *"El LLM extrae datos del usuario durante la conversación"* |

F31 está listado en el Anexo A bajo *"Módulo 3 — Plataforma operativa"*. **Asignar un lenguaje por módulo no tiene respuesta correcta, sea cual sea la asignación**: la división temática y la división por tipo de trabajo son perpendiculares. No es que 8.4 haya elegido mal; es que la pregunta no se puede contestar en esos términos.

Hay una segunda señal en el propio texto: en 7.2, **OE1 a OE5 llevan etiqueta `[Módulo N]` y OE6 y OE7 no la llevan.** Son justamente los dos objetivos que cruzan módulos. El anteproyecto dejó de etiquetar exactamente donde su propia división deja de cerrar.

### 1.3. Y el Módulo 2 son varias cosas distintas

| Dentro de M2 | Qué clase de problema es |
|---|---|
| Timer adaptativo y agregación (F10) | **Concurrencia con estado**: muchos temporizadores vivos que se reinician con cada mensaje entrante |
| Clasificadores de frustración, intención y transferencia (F11, F12) | **Inferencia de modelos** — 8.4 nombra scikit-learn y XGBoost |
| Transcripción de audio (§8.5) | Inferencia de modelos |
| Puntaje, ventas, origen y gráficas (F13–F16) | **ABM y consulta agregada — sin nada de IA** |

La costura real del sistema no es M1 / M2 / M3: es **código que habla con modelos** contra **código que habla con usuarios y con la base**. La frontera de 8.4 la cruza justo por el medio.

### 1.4. La asimetría entre volumen y aporte

Del recorrido de los 39 requisitos (§6):

| Lenguaje | Requisitos | Objetivos que sostiene |
|---|---|---|
| **Go** | 26 de 39 (~67 %) | **OE5** + la mitad de OE3 (F10) |
| **Python** | 7 de 39 (~18 %) | **OE1, OE2, OE4, OE6** + la otra mitad de OE3 |
| Partidos entre ambos | 4 | OE7 |
| Documentales (F29, F39) | 2 | — |

**Go concentra el volumen de código; Python concentra el aporte.** Siete requisitos sostienen cinco de los siete objetivos y las tres contribuciones originales de 6.3. No desmerece al núcleo —OE5 exige la plataforma completa con cobertura ≥ 80 %, que es trabajo pesado y evaluable— pero **es un desbalance del reparto entre las tres personas y conviene hablarlo ahora, no en marzo**.

## 2. Decisión

**(a) Los tres módulos son *bounded contexts*, no servicios.** Se mantienen como paquetes con fronteras explícitas y se presentan así en el informe, tal como los describe 8.5.

**(b) Tres artefactos desplegables y uno offline:**

| Artefacto | Lenguaje | Contiene | ¿Se despliega? |
|---|---|---|---|
| **Núcleo operativo** | **Go** | Gateway (webhooks, HMAC-SHA256) · timer adaptativo y agregación · fan-out por WebSockets · ABM y consultas de bandeja, contactos, etiquetas, recordatorios, ventas y métricas | Sí |
| **Cerebro** | **Python** | Composición y render de prompts · pipeline RAG y reranking · llamadas al LLM · clasificadores de F11/F12 · transcripción de audio | Sí — **en dos modos de arranque**, `api` y `worker`, ver (g) |
| **Frontend** | *A DEFINIR* | Bandeja en tiempo real, panel de administración, previsualización de F03 | Sí |
| **Instrumental de evaluación** | **Python** | LLM-as-judge, conjuntos de 200 y 500 conversaciones, barridos de umbral, números del informe | **No.** Se corre cuando hace falta medir |

**(c) Un único PostgreSQL, compartido por ambos servicios.** Es lo que ya dice 8.5, y es innegociable — ver §3. Las reglas que lo hacen sostenible están en (i).

**(d) El Módulo 2 se reparte entre los dos servicios:** el timer va al núcleo (Go), los clasificadores al cerebro (Python). Es la desviación explícita respecto del reparto por módulo de 8.4.

**(e) El cerebro se expone como API interna** (HTTP + JSON, con contrato OpenAPI que sirve además de entregable de la Fase 3). No hay cola de mensajes: el protocolo MQTT está fuera de alcance por 7.3.

> **El webhook acusa recibo antes de procesar.** Meta espera una respuesta en segundos y el pipeline de [ADR-0016](0016-rastro-del-pipeline-de-respuesta.md) son ocho pasos con varias llamadas a LLM. El núcleo responde el webhook de inmediato, procesa aparte y envía la respuesta por la API de salida. F10 ya lo obliga de todos modos: el timer no puede correr dentro de la petición del webhook.

**(f) El frontend sigue *A DEFINIR*.** Este ADR no elige la tecnología; continúa como pendiente heredado de 8.4. Lo que sí decide es **contarlo como desplegable**, que la versión anterior omitía.

**(g) La ingesta de F06 corre en el modo `worker` del cerebro.** Mismo repositorio, mismo código, mismo contenedor, **segundo punto de entrada** — no un cuarto artefacto.

- **La cola ya existe y no hace falta crearla:** `documents.status` (`pending · processing · indexed · failed`, [ADR-0005](0005-base-de-conocimiento.md)) es literalmente el ciclo de vida de un trabajo en segundo plano. El worker toma trabajo con `SELECT … WHERE status='pending' … FOR UPDATE SKIP LOCKED`.
- **Se agrega `documents.locked_at`** (timestamptz, nullable) para recuperar documentos que quedan en `processing` si el worker muere. Sin esa columna, un documento colgado no vuelve nunca.
- **No se crea una tabla `jobs`.** Sería una segunda representación del mismo estado.

**(h) El ensamblador del prompt tiene una sola implementación, y vive en Python** (`brain/prompts/`). El núcleo **no lo reimplementa**: la previsualización de F03 es una llamada del núcleo al cerebro (`POST /prompts/preview`), la misma ruta que usa el turno real.

> Es lo que protege a OE1. Si el ABM de componentes vive en Go, la tentación de concatenar los seis textos ahí es concreta — y dos implementaciones del mismo render divergen en semanas. **OE1 mediría entonces un ensamblador mientras el administrador previsualiza otro, y el 100 % pasaría a medir algo que nadie ve.** Python es además el lado que necesita el tokenizador para el presupuesto de tokens de [03-capa-prompts.md](../03-capa-prompts.md).

**(i) Tres reglas para la base compartida.** Sin ellas, dos servicios sobre una base es una condición de carrera con dos dueños.

1. **Un solo dueño del DDL: el núcleo.** Las migraciones viven en `core/migrations/`. **El cerebro nunca hace DDL**, sólo DML. El instrumental de evaluación tampoco: como el cerebro, es DML únicamente.
2. **Una columna, un servicio que escribe.** La regla **restringe escrituras, no lecturas**: los tres leen todo el esquema —el cerebro no podría armar un prompt sin leer `messages`—, y lo que está repartido es quién escribe qué.

   | | Tablas |
   |---|---|
   | **Escribe el núcleo (Go)** | `conversations` · `messages` · `conversation_assignments` · `transfer_events` · `contacts` · `operators` · `labels` · `sales` · `reminders` · `quick_replies` · `inboxes` · `webhook_events` · `meta_templates` · `message_batches` · `audit_logs` · el ABM de `ai_agents`, `prompt_components`, `prompt_component_versions` y `transfer_policies` |
   | **Escribe el cerebro (Python)** | Sólo sus tablas de **evidencia**: `generations` · `generation_components` · `pipeline_steps` · `retrieval_logs` · `detected_intents` · `signal_evaluations` · `conversation_contexts` (+ versiones) · `contact_attributes` · `documents` · `document_chunks` |
   | **Escribe el instrumental de evaluación (Python, offline)** | `conversation_annotations` · `tone_evaluations` |

   **El cerebro nunca escribe `conversations`, `messages` ni `contact_attributes`.** No persiste la respuesta que genera: **la devuelve, y el núcleo la persiste y la envía**. Igual con la transferencia — el cerebro *propone* (escribe `signal_evaluations`), el núcleo *ejecuta* (escribe `transfer_events` y mueve `conversations.status`). Así la persistencia y el envío a Meta quedan del mismo lado, que es lo único que evita el peor error posible: un mensaje guardado que nunca se envió, o enviado dos veces.

   **Y lo mismo con F31 (2026-09-03).** El LLM extrae datos del contacto, pero `contact_attributes` **no es evidencia: es dato del contacto**. El cerebro devuelve los atributos extraídos con su `AttributeSource = llm` y `contacts/` los persiste. La alternativa —que el cerebro escriba— costaba una excepción a esta regla a cambio de ahorrar un campo en la respuesta del turno. **La regla vale más que el campo.**

   **Por qué el instrumental está separado, y no es un detalle de despliegue.** `conversation_annotations.gold_value` es la verdad de terreno contra la que se mide la precisión del clasificador (OE3, OE4), y `tone_evaluations` es donde vive OE2. Que su **único escritor sea un proceso offline que no se despliega** es la garantía estructural de que **el sistema evaluado no puede escribir su propia nota**. Es una propiedad de diseño, no una casualidad de organización.

   **La excepción: `generations`, por traspaso.** El núcleo tiene que escribir en la tabla del cerebro, porque enlaza el mensaje que acaba de crear y registra el desenlace de la revisión de F02. La regla que lo ordena es temporal:

   > **El cerebro es dueño de la fila hasta que responde. Desde ese instante, la fila es del núcleo.**

   | Escribe el cerebro (en el `INSERT`, una sola vez) | Escribe el núcleo (después, nunca antes) |
   |---|---|
   | Todas las columnas de contenido y evidencia, más `status` inicial (`auto_sent`, `pending_review` o `blocked`) | `message_id` · `status` **sólo desde `pending_review`** a `accepted`/`edited`/`rejected` · `reviewed_by_operator_id` · `reviewed_at` |

   **No hay concurrencia: hay un traspaso.** El cerebro escribe una vez y nunca vuelve a una generación sobre la que ya contestó; el núcleo escribe recién después. El propósito original de la regla —que dos servicios no se pisen— queda intacto, y el enunciado pasa a ser **más estricto y verdadero** en lugar de más simple y falso.

   **La excepción se enumera, no se autoriza.** Vale sólo donde un servicio produce una fila que el otro debe completar **porque únicamente él conoce el desenlace**. Hoy hay **un solo caso declarado** —`generations`—. Cualquier segundo caso requiere una decisión nueva, no esta regla.

   > **`documents` dejó de ser un caso abierto (2026-09-03).** La versión anterior lo listaba como el segundo, porque `uploaded_by_operator_id` es NOT NULL y el worker toma trabajo con `FOR UPDATE SKIP LOCKED` —patrón que presupone un productor distinto del consumidor— y no estaba decidido quién recibía la subida. **Con el núcleo recibiéndola, no hay excepción:** Go crea la fila completa, incluido el operador que subió, y el `worker` sólo actualiza `status`, `locked_at` y `error_message`. Es una columna, un servicio, sin traspaso de propiedad — el productor nunca deja campos que el consumidor deba completar por conocer algo que él no conoce.
   >
   > **Lo que sigue pendiente de H-31 no es esta regla, son dos atributos.** `documents` no tiene **dónde** están los bytes: registra `filename`, `mime_type`, `size_bytes` y `checksum`, y ninguna dirección. Al firmarse este ADR hay que agregar al catálogo `documents.object_key` (varchar — la clave en el almacén de objetos, que es lo único con que el `worker` puede bajar el archivo) y `documents.locked_at` (timestamptz nullable — para el `SKIP LOCKED`). **Son dos atributos, no una entidad: los contadores del modelo no se mueven.**
3. **Ninguna transacción cruza el borde.** No hay transacciones distribuidas. Cuando el cerebro escribe evidencia y el envío falla después, el estado se representa en `generations.status` **y en `messages.status`**, que son cosas distintas: el primero describe el **filtrado** (¿pasó sin revisión humana, la revisó alguien, la frenó el sistema?), el segundo la **entrega**. Una generación `auto_sent` enlazada a un mensaje `failed` es una fila coherente.

   > **Corrección del Paso 8.** La versión anterior decía que `generations.status` *"ya tiene los valores necesarios"*. **No los tenía:** faltaba `blocked`, para la respuesta que frena el guardrail de salida de [ADR-0016](0016-rastro-del-pipeline-de-respuesta.md). Es **H-26**, y el valor se agregó al catálogo.

**(j) Nombre de la arquitectura**, para el informe y la defensa:

> SONIA adopta una **arquitectura basada en servicios** (*service-based architecture*): dos servicios de grano grueso separados **por ecosistema tecnológico, no por dominio ni por escala**, sobre una única base PostgreSQL compartida, con los tres módulos de la sección 8.5 como *bounded contexts* internos. Cada servicio se organiza **por capas dentro de cada contexto**, con puertos y adaptadores en las cuatro fronteras donde el reemplazo está a la vista: proveedor de LLM (8.4), base vectorial (8.4), canal de WhatsApp (Twilio Sandbox → API oficial de Meta, Anexo D) y almacén de objetos (proveedor sin decidir).

Dos nombres que **no** deben usarse:

- **"Microservicios"** — porque comparten base. Invita a *"¿por qué comparten base de datos?"* sin respuesta buena.
- **"Monolito distribuido"** — es un término **peyorativo**: nombra el antipatrón de microservicios que fallaron en ser independientes. La diferencia con lo nuestro es que acá está **elegido a propósito, con el motivo escrito y con condición de reversión documentada** (§4). Conviene tener esa respuesta preparada.

> *"Arquitectura basada en servicios" es una categoría con nombre propio en la taxonomía de Richards & Ford (`Fundamentals of Software Architecture`), y su rasgo definitorio es exactamente la base compartida. **Verificar la cita antes de incorporarla al informe:** la bibliografía del anteproyecto todavía no tiene ninguna referencia de arquitectura.*

**(k) Autenticación entre los dos servicios** — *agregado el 2026-09-03*.

Separar en dos desplegables crea una superficie que el monolito no tenía: **el cerebro escucha en un puerto**. Y ese proceso tiene la clave del proveedor de LLM, puede renderizar el prompt completo de cualquier agente (F03 —voz de marca y reglas incluidas, que son propiedad intelectual de la cooperante) y **escribe las tablas de evidencia de las que se leen OE1 a OE7**. El §(i) resuelve que dos servicios no se pisen; no dice nada sobre un tercero.

**La decisión: secreto compartido en cabecera *más* aislamiento de red. Las dos cosas.**

| Capa | Qué hace |
|---|---|
| **Aislamiento de red** | El puerto del cerebro **no se publica al host** en `docker-compose.yml`; sólo es alcanzable desde la red interna del compose |
| **Secreto compartido** | El núcleo manda `X-Internal-Token` en cada petición; una dependencia de FastAPI lo valida y rechaza con `401`. Viene de `BRAIN_INTERNAL_TOKEN` y **se valida al arrancar** (regla 9): si falta, ninguno de los dos procesos levanta |

**Es unidireccional, y eso es lo que la mantiene simple.** El núcleo llama al cerebro; el cerebro **devuelve** y nunca llama de vuelta —es la regla (i)—, así que no hace falta que el cerebro se autentique ante nadie. Y **la identidad del operador no viaja**: el núcleo autentica a la persona, llama al cerebro como sí mismo y registra en `audit_logs`, que es su tabla. El cerebro no necesita saber quién previsualizó.

| Alternativa | Motivo del descarte |
|---|---|
| **Sólo aislamiento de red** | Es gratis y hace la mayor parte del trabajo, pero depende de que nadie publique el puerto por error. Para OE7 —despliegue en producción real— eso es una promesa, no un control |
| **mTLS** | Certificados, renovación y distribución para dos procesos de una PYME. Costo desproporcionado |
| **JWT firmado por el núcleo** | Su ventaja es propagar identidad con expiración. **Acá no hay identidad que propagar**, así que se paga gestión de claves y sincronía de reloj a cambio de nada |

**Consecuencia negativa, declarada:** rotar el secreto obliga a reiniciar los dos procesos. Con dos desplegables y una cooperante es aceptable; conviene decirlo antes de que aparezca como sorpresa.

**Dónde se implementa:** `platform/httpclient/` y `brainclient/` lo agregan del lado Go; una dependencia en `brain/api/` lo valida del lado Python; `BRAIN_INTERNAL_TOKEN` entra en `.env.example` y en las dos configuraciones.

> **Queda abierto —y es más fuerte que esto—: el rol de Postgres del cerebro.** Con el HTTP cerrado, el cerebro **sigue teniendo credenciales de base**, y la regla (i) —*"escribe sólo tablas de evidencia"*— es hoy una **convención**: nada impide que un `INSERT` en `messages` funcione. Un rol sin `INSERT`/`UPDATE` sobre `conversations`, `messages` y `contact_attributes` la convertiría en un `GRANT` de la migración 0001, y la base rechazaría la violación en vez de que se descubra con datos productivos encima. Es la misma lógica por la que el instrumental de evaluación está separado: **una propiedad de diseño vale más cuando la garantiza la estructura que cuando la garantiza la disciplina.** Requiere decisión del equipo.

## 3. Justificación

- **La cadena de trazabilidad de OE2 no puede volverse un join distribuido.** `message → generation → generation_components → prompt_component_version → conversation_context_version → retrieval_logs` cruza los tres módulos, y es *"lo que hace reproducible a OE2"* según [relaciones.md §3](../DER/relaciones.md). Con base por servicio deja de ser una consulta y pasa a ser consistencia eventual entre procesos. No se rompería "la base": se rompería el aporte original que declara la sección 6.3.

- **Los módulos no tienen volumen independiente.** Cada turno atraviesa Gateway → M2 → M1 → M3 **en orden y de forma sincrónica**, porque el cliente está esperando en WhatsApp. Los volúmenes están acoplados 1 a 1: no se puede escalar M1 sin escalar M2. Un pipeline donde toda petición recorre todos los componentes es el caso donde separar deployables agrega latencia y modos de falla **sin comprar independencia**.

- **La frontera propuesta sí separa cosas que difieren de verdad.** El ecosistema de LLM, embeddings y evaluación vive en Python; el manejo de conexiones WebSocket y de temporizadores con estado es más limpio en Go. Esa diferencia es real; la que hay entre M1 y M2 no lo es.

- **La ingesta no es una excepción al diseño: es una forma de ejecución distinta que estaba sin declarar.** F06 admite documentos de hasta 100 MB por agente. Fragmentar, embeber e indexar eso lleva minutos, no milisegundos, y **no cabe en una petición HTTP**. Lo notable es que **el modelo de datos ya lo sabía** —`DocumentStatus` tiene los cuatro estados de un trabajo en cola— mientras que ninguna descripción de arquitectura, ni la del anteproyecto ni la primera versión de este ADR, declaraba el proceso que los recorre.

- **Costo operativo proporcionado al equipo.** Tres personas y trece meses. Un modo de arranque adicional cuesta un `if` en el punto de entrada; un cuarto artefacto cuesta otro despliegue, otra configuración y otra cadena de herramientas.

- **No contradice el texto del anteproyecto.** 8.5 dice *"módulos integrados"*, no servicios; 8.4 ofrece los dos lenguajes para todos los módulos. Este ADR elige dentro del espacio que el anteproyecto dejó abierto — salvo en el punto (d), que sí es un refinamiento, y por eso se declara.

## 4. Consecuencias

### Positivas

- Cada lenguaje se usa donde su ecosistema aporta, sin forzar ninguno de los dos.
- La trazabilidad de OE2 sigue siendo una consulta SQL.
- **El cerebro escala horizontalmente sin tocar nada**: es sin estado, y es donde está la carga cara.
- **La superficie funcional escala barato**, que es el eje que le importa a la tesina: los canales adicionales de 7.3 (Instagram, web, email) son un adaptador dentro de `channel/`, no una reescritura, y el multi-tenant ya tiene previsión estructural en `accounts` ([ADR-0012](0012-cuenta-y-bandejas-de-entrada.md)).
- La ingesta de F06 tiene dónde ocurrir, sin agregar componentes: la cola es una tabla que ya existe.
- El reparto de personas del plan —A en la capa de prompts, B en núcleo y operación, C en ejecución y evaluación— sigue funcionando sin bloqueos mutuos.

### Negativas / costos asumidos

- **El núcleo hoy no escala horizontalmente, y hay que decirlo antes de que lo pregunten.** El timer de F10 mantiene estado en memoria —un temporizador vivo por conversación—, así que con dos instancias no está definido cuál es dueña del timer de una conversación dada. El fan-out de WebSockets tiene el mismo problema: el operador está conectado a la instancia A y el mensaje llega a la B. **Para el alcance de 7.3 una instancia sobra**, pero es un techo real. *Salida cuando haga falta, sin violar 7.3:* `LISTEN`/`NOTIFY` de PostgreSQL resuelve el fan-out —no es MQTT y no agrega un componente nuevo— y el timer necesitaría ruteo pegajoso por conversación o *leases* en la base. **Queda registrado como pendiente del Paso 7.**
- **OE5 se complica.** Exige *"cobertura de pruebas unitarias ≥ 80%"*: con dos runtimes hay dos cadenas de herramientas, dos reportes, y hay que definir **cómo se agregan ambos en un solo número**. SonarQube lo soporta, pero es trabajo que un monolito no tendría.
- **Hay un salto de red en el camino caliente de cada turno.** El núcleo llama al cerebro y el cliente espera. Agrega latencia y un modo de falla nuevo: **cerebro caído**. Propuesta para el Paso 8 — el núcleo persiste el bloque igual, marca el turno como fallido y **cae la conversación a `waiting` para un humano**. Nunca se pierde el mensaje: un cliente esperando en silencio es peor que un operador atendiendo.
- **El DDL único tiene fricción real.** Quien trabaja en Python depende del núcleo para agregar una columna. Es deliberado —es el precio de que no haya dos verdades sobre la forma de las tablas— y es el motivo por el que **conviene un solo repositorio** con los dos servicios adentro: en repos separados, cada columna nueva es un PR cruzado.
- **Las migraciones tocan a los dos servicios y el orden importa.** La disciplina es **expandir → desplegar → contraer**: agregar la columna nueva, desplegar ambos, y recién entonces borrar la vieja. Nunca una migración rompiente en un solo paso.
- **Un documento puede quedar colgado en `processing`** si el worker muere a mitad de la ingesta. Por eso `locked_at` y un barrido de recuperación: es una columna y una tarea periódica, y hay que construirlas, no descubrirlas.
- **Un requisito ya no se puede señalar en un solo lugar del árbol de archivos.** El Módulo 2 vive en `core/internal/turn/` *y* en `brain/cognition/`. Cuando el informe diga "Módulo 2", el lector necesita un mapa: **ése es el §6**, y hay que mantenerlo.
- **El equipo sostiene dos lenguajes** durante trece meses: estilo, linters y revisión de código en ambos.
- **Se desvía del reparto por módulo de 8.4** al partir el Módulo 2. Hay que explicarlo en el informe: el módulo sigue existiendo conceptualmente, pero su implementación vive en dos procesos.

### Condición de reversión

Si hacia el final de la Fase 2 el equipo está gastando más tiempo en la frontera entre los dos servicios que en las funcionalidades, **la salida es colapsar a un único servicio Python** y mover el timer a `asyncio`. Es una vuelta atrás barata mientras el esquema de base no cambie, y conviene tenerla escrita ahora y no descubrirla bajo presión.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Un servicio por módulo (tres o cuatro procesos)** | Es la lectura literal de 8.5 y la más costosa. Los módulos no tienen volumen independiente —cada turno los recorre todos en orden—, así que la separación agrega latencia, despliegues y depuración distribuida sin comprar escalado independiente. Además `conversations` y `messages` los escriben los tres: la frontera de proceso no coincide con la frontera del dato. |
| **Monolito único en Python** | **Es la alternativa más fuerte y conviene decirlo.** Sería lo más rápido de construir, con una sola cadena de herramientas y un solo reporte de cobertura, y la carga real —una PYME cooperante con decenas de conversaciones diarias— la resuelve `asyncio` sin dificultad. Se descarta porque el timer adaptativo de F10 es genuinamente más limpio con goroutines y canales, y porque el equipo declara interés en sostener el uso de Go. **Pero ése es el motivo real y hay que declararlo así:** la justificación de 8.4 —*"concurrencia masiva de webhooks y WebSockets"*— **no está respaldada por el alcance de 7.3**, que es una cooperante propietaria de departamentos en alquiler. Sostener un argumento de escala que el alcance no respalda es exactamente lo que un tribunal pregunta. |
| **Monolito único en Go** | Renuncia a los SDK de proveedores, los tokenizadores y las bibliotecas de evaluación, que es donde vive el aporte de la tesina. **No se nombra LangChain a propósito:** el equipo decidió no usarlo en el camino caliente —sus *templates* y *chains* son justamente la capa de la que este proyecto es dueño (punto (h) y `generation_components`)—, así que sostener el argumento sobre él sería falso. Reimplementarlo en Go es trabajo sin valor académico. |
| **Base de datos por servicio** | Contradice 8.5 —*"almacena todos los estados del sistema"*— y rompe la cadena de trazabilidad de OE2, convirtiéndola en consistencia eventual entre procesos. Es la única alternativa que este ADR descarta sin matices. |
| **Cola de mensajes entre núcleo y cerebro** | 7.3 declara el protocolo MQTT explícitamente fuera de alcance. Y una cola cambiaría el turno de sincrónico a asincrónico, lo que contradice la expectativa de respuesta casi inmediata de WhatsApp. |
| **El trabajador de ingesta como cuarto artefacto propio** | Comparte el 100 % del código de fragmentación y embeddings con el cerebro. Un artefacto aparte duplicaría dependencias, imagen y despliegue **sin separar nada que difiera de verdad**. Un segundo punto de entrada sobre el mismo código da el proceso separado sin el costo. |
| **Ingesta sincrónica dentro de la petición HTTP** | 100 MB no caben en una petición. El administrador subiría el archivo y esperaría minutos frente a una pantalla trabada, con un *timeout* de por medio. |
| **Tabla `jobs` dedicada para la cola de ingesta** | `documents.status` ya es exactamente esa cola, con los cuatro estados correctos. Una tabla nueva sería una segunda representación del mismo estado, con la posibilidad de que se contradigan. |
| **Reimplementar el ensamblador en Go para la previsualización** | Es la alternativa que aparece sola por comodidad, y la más dañina: rompe OE1. Ver (h). |

## 6. Anexo — reparto por requisito

Derivado del Anexo A del anteproyecto, requisito por requisito. **Éste es el mapa que reemplaza a "el Módulo N está en el lenguaje L"**, que §1.2 muestra que no tiene respuesta.

**Regla de corte: ¿este código llama a un modelo?** No es temática, no es por módulo y no depende de qué funcionalidad se esté construyendo. Es la única frontera que se sostiene sola al recorrer los 39 requisitos.

| # | Requisito | Clase de trabajo | Servicio | Paquete |
|---|---|---|---|---|
| F01 | Configuración modular del agente | ABM | Go | `agents/` |
| F02 | Tres modos de operación | ABM + estado | Go | `agents/` (defecto) · `inbox/` (por conversación) |
| F03 | Previsualización del prompt | Render | **Python** | `prompts/` — invocado por el núcleo, ver (h) |
| F04 | Reescritura adaptativa | Inferencia | **Python** | `prompts/` — rewriter.py |
| F05 | Visualización del razonamiento | **Partido** | Python escribe · Go sirve | `pipeline/runner.py` → ~~`inbox/`~~ `reasoning/` *(enmendado por [ADR-0063](0063-paquetes-por-razon-de-cambio.md))* |
| F06 | Pipeline RAG | Inferencia + **lote** | **Python** | `documents/` (ABM, Go) · `knowledge/` + `worker/` (Python) |
| F07 | Acceso a respuestas rápidas por la IA | Inferencia | **Python** | ~~`pipeline/generation.py`~~ `prompts/repository.py` + `prompts/assembler.py` *(enmendado por [ADR-0061](0061-respuestas-rapidas-en-el-prompt.md))* — lee tabla del núcleo |
| F08 | Visualización de plantillas Meta | **Partido** | Go sincroniza · Python sugiere | `meta_templates/` + `pipeline/` |
| F09 | Respuestas rápidas configurables | ABM | Go | `quick_replies/` |
| F10 | Timer adaptativo | **Concurrencia con estado** | Go | `turn/` |
| F11 | Chunking dinámico | Inferencia | **Python** | `classifiers/intent.py` |
| F12 | Clasificador de transferencia | Inferencia | **Python** | `classifiers/transfer.py` · `frustration.py` |
| F13 | Puntaje de conversaciones | ABM | Go | `inbox/` |
| F14 | Registro de ventas | ABM | Go | `sales/` |
| F15 | Trazabilidad de origen | ABM | Go | `contacts/` |
| F16 | Gráficas y métricas de vendedores | Consulta agregada | Go | `reporting/` |
| F17 | Gestión de agentes IA | ABM | Go | `agents/` |
| F18 | Gestión de usuarios y roles | ABM | Go | `operators/` |
| F19 | Gestión de etiquetas | ABM | Go | `labels/` |
| F20 | Gestión de webhooks | IO externo | Go | `whatsapp/` |
| F21 | Backend Go / WebSockets | IO + tiempo real | Go — **nombrado en el requisito** | `whatsapp/` (recepción) · ~~`inbox/`~~ `realtime/` (fan-out) *(enmendado por [ADR-0063](0063-paquetes-por-razon-de-cambio.md))* |
| F22 | Vista centralizada de chats | Consulta caliente | Go | `inbox/` |
| F23 | Filtros de bandeja | Consulta caliente | Go | `inbox/` |
| F24 | Etiquetas por conversación | ABM | Go | `inbox/` |
| F25 | Destacar con estrella | ABM | Go | `inbox/` |
| F26 | Transferencia entre operadores | ABM | Go | `inbox/` · `notes/` (las notas internas) *(enmendado por [ADR-0063](0063-paquetes-por-razon-de-cambio.md))* |
| F27 | Control de modo por conversación | ABM | Go | `inbox/` |
| F28 | Recordatorios | ABM + disparo | Go | `reminders/` |
| F29 | Restricción: sin borrado de mensajes | **Documental** | — | — |
| F30 | ABM de contactos | ABM | Go | `contacts/` |
| F31 | Extracción automática de datos | **Partido** | Python infiere · **Go persiste** | `classifiers/extraction.py` → `contacts/` persiste |
| F32 | Historial unificado por contacto | Consulta | Go | `reporting/` |
| F33 | Segmentación por etiquetas | Consulta | Go | `contacts/` |
| F34 | Métricas de volumen | Consulta agregada | Go | `reporting/` |
| F35 | Métricas por vendedor | Consulta agregada | Go | `reporting/` |
| F36 | Métricas del agente IA | **Partido** | Python produce el juicio de tono · Go agrega | `test/evals/` → `reporting/` |
| F37 | Panel de plantillas Meta | **Partido** | Go sincroniza y sirve · Python sugiere | `meta_templates/` + `pipeline/` |
| F38 | Gráficas temporales | Consulta agregada | Go | `reporting/` |
| F39 | Restricción: sin campañas Meta | **Documental** | — | — |

**Totales: 26 Go · 7 Python · 4 partidos · 2 documentales = 39.**

| Módulo del anteproyecto | Go | Python | Partidos | Documentales | Total |
|---|---|---|---|---|---|
| Módulo 1 (F01–F09) | 3 | 4 | 2 | — | 9 |
| Módulo 2 (F10–F16) | 5 | 2 | — | — | 7 |
| Módulo 3 (F17–F39) | 18 | 1 | 2 | 2 | 23 |

**Ningún módulo del anteproyecto es monolingüe: los tres usan los dos lenguajes.** Y el detalle más incómodo está en el medio — **el Módulo 2, el "agregador y clasificador", es mayoría Go, 5 a 2**, porque F13 a F16 (puntaje, ventas, origen, gráficas) no tienen nada de IA. El nombre del módulo describe su aporte, no su implementación. Es la confirmación cuantitativa del punto (d).

### 6.1. Estructura de paquetes

El corte de primer nivel es el ***bounded context*; las capas van adentro de cada uno.** Si el primer nivel fuera la capa (`controllers/`, `services/`, `repositories/`), la frontera entre Módulo 1, 2 y 3 se volvería invisible en el árbol de archivos — que es justamente la frontera que el informe necesita señalar.

```
sonia/                              ← un solo repositorio
├── .github/workflows/ci.yml        cobertura ≥ 80 % POR SERVICIO (ADR-0028)
├── .dockerignore                   en la raíz: es la raíz del contexto de build
├── .env.example   ·   Makefile     db-migrate · db-seed · db-reset · test
├── contracts/                      enums.yaml (las 22) · brain.openapi.json — fuente única
├── deploy/                         Dockerfiles + compose · context: .. para ver contracts/
└── src/
    ├── core/                       ← DESPLEGABLE 1 · Go
    │   ├── main.go                 sólo main(): la señal y el código de salida
    │   ├── internal/
    │   │   ├── app/                EL ARRANQUE · app.go (Run) · deps.go (el grafo de objetos)
    │   │   ├── whatsapp/           Gateway · F20, F21(rx), F29
    │   │   ├── meta_templates/     F08, F37 (sincronización y panel)
    │   │   ├── turn/               F10 · time.Timer + canales · ABRE LA TRANSACCIÓN
    │   │   ├── conversations/      DUEÑO de conversations y messages · el agregado de §1.1
    │   │   ├── agents/             F01, F02, F17 · ABM de la identidad del agente
    │   │   ├── documents/          F06 (ABM y carga) · knowledge_bases + documents
    │   │   ├── inbox/              F21(fan-out), F05(vista), F13, F22–F27
    │   │   ├── contacts/           F15, F30, F31(persistencia), F33
    │   │   ├── labels/             F19        ├── sales/     F14
    │   │   ├── reminders/          F28        ├── operators/ F18
    │   │   ├── quick_replies/      F09
    │   │   ├── reporting/          F16, F32, F34–F36(lectura), F38 · SÓLO LECTURA
    │   │   ├── brainclient/        puerto de salida hacia el cerebro
    │   │   └── platform/           http · httpclient · storage · db (pool + Executor) · id · config · logging
    │   ├── migrations/             ← DUEÑO ÚNICO del esquema, regla (i.1) · DDL + invariantes
    │   └── seeds/                  datos de demo, idempotentes, nunca en producción
    ├── brain/                      ← DESPLEGABLE 2 · Python · dos modos de arranque
    │   ├── main.py                 un entrypoint, flag api|worker
    │   ├── pyproject.toml          Poetry · absorbe la config de pytest · poetry.lock se commitea
    │   ├── api/                    FastAPI · contrato interno (OpenAPI)
    │   ├── pipeline/               un módulo por valor de PipelineStep (ADR-0016) · F05, F07
    │   ├── prompts/                F03, F04 · EL ENSAMBLADOR, única implementación
    │   ├── classifiers/            F11, F12, F31 — F31 **devuelve**, no escribe
    │   ├── knowledge/              F06 · búsqueda semántica
    │   ├── worker/                 F06 · ingesta — segundo entrypoint, mismo código
    │   ├── adapters/               llm/ · vectorstore/ · storage/ — puertos de (j)
    │   ├── rules/                  prompts internos versionados, no código
    │   ├── infra/                  config · db · logging — NO `platform/`: taparía la stdlib
    │   └── tests/                  unitarios · conftest.py · factories.py
    ├── frontend/                   ← DESPLEGABLE 3 · A DEFINIR (8.4)
    └── test/                       karate/ · evals/ (OFFLINE, no se despliega) · datasets/
```

> **Nota (2026-09-03).** Este árbol reemplaza al de la versión del 22/08, que usaba `channel/`, `operation/`, `cognition/` y `eval/`. El renombre no fue cosmético: al chequearlo contra la tabla del §6 aparecieron **cinco requisitos sin paquete** —F05 (lado Python), F06 (el ABM), F07, F08/F37 y F31—, todos resueltos arriba. Es el mismo patrón del §0: *recorrer requisitos y dibujar árboles contestan preguntas distintas.*

> **Nota (2026-09-05, al firmar).** Se incorporaron al árbol `core/internal/app/` —el arranque, segunda excepción declarada junto a `platform/`: está fuera de `package main` porque `package main` no se puede importar y un test que levante el proceso entero necesita importarlo—, `brain/tests/` y los archivos de raíz que la Fase 0 construye. **El árbol completo, archivo por archivo, y el dueño de cada una de las 37 tablas viven en [`docs/struct.md`](../../struct.md)**, que detalla a éste; ante conflicto entre los dos gana `CLAUDE.md`.

**Puertos y adaptadores sólo en cuatro fronteras**, porque son las cuatro donde el reemplazo está a la vista — no es abstracción especulativa:

| Frontera | Evidencia textual |
|---|---|
| **Proveedor de LLM** | 8.4: *"GPT-4o / Claude / Cohere / Otros"*. OE2 exige compararlos (pendiente abierto) |
| **Base vectorial** | 8.4: *"pgvector o Qdrant"*. [ADR-0010](0010-qdrant-como-base-vectorial.md) eligió Qdrant; el puerto protege la reindexación si cambia el modelo de embeddings |
| **Canal de WhatsApp** | 8.4: *"Twilio Sandbox para desarrollo"* → Anexo D, Fase 4: API oficial de Meta. **El cambio está agendado** |
| **Almacén de objetos** | F06 admite documentos de 100 MB y los bytes no van en la fila. **El proveedor está sin decidir** (Supabase, DigitalOcean, otro), así que el puerto no es especulativo: es la incertidumbre misma. Go escribe al subir, el `worker` lee al ingerir; `documents.object_key` guarda la clave (H-31) |

En el resto —ABM de contactos, etiquetas, recordatorios— **un repositorio alcanza**. No conviene inventar puertos donde no hay dos implementaciones a la vista.
