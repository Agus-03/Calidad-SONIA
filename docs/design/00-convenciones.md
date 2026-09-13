# SONIA — Convenciones de modelado y nomenclatura

| Campo | Valor |
|---|---|
| **Documento** | `docs/design/00-convenciones.md` |
| **Estado** | Vigente |
| **Fecha** | 2026-08-06 |
| **Etapa** | Diseño — Paso 1 (Fundaciones) |
| **Base documental** | [SONIA_Anteproyecto.pdf](../SONIA_Anteproyecto.pdf) — Anexo A (F01–F39), Anexo B (roles y permisos), Anexo C (modelo de datos), secciones 7.3 (alcance) y 8.4–8.5 (tecnologías y arquitectura) |

---

## 0. Propósito y alcance

Este documento fija **cómo se nombran y se tipan las cosas** en SONIA, antes de que exista un modelo de datos, un diagrama o una línea de código. No define *qué* entidades hay —eso corresponde a los pasos siguientes de la etapa de diseño— sino las reglas que todas ellas van a respetar.

Va primero por un motivo concreto: cambiar un nombre o un tipo ahora cuesta un rato; cambiarlo cuando existen quince diagramas, un esquema de base de datos y código en dos lenguajes, cuesta días y deja inconsistencias que sobreviven hasta la defensa.

**Regla de precedencia:** si un diagrama, un esquema o un fragmento de código contradice este documento, gana este documento. Si el documento está equivocado, se corrige **acá primero** y después se propaga. Nunca al revés.

**Aplica a:** modelo de datos lógico y físico, diagramas, esquema de PostgreSQL, código de aplicación y contratos entre servicios.

---

## 1. Resumen de las decisiones

| # | Tema | Decisión |
|---|---|---|
| **C01** | Nomenclatura del agente IA y del humano | `AIAgent` para el agente de IA; `Operator` para la persona |
| **C02** | Idioma | Inglés para tablas, columnas, enums, código y comentarios; español para documentación |
| **C03** | Estilo de nombres | `snake_case`, tablas en plural, PK `id`, FK `<entidad>_id` |
| **C04** | Clave primaria | `UUID` (versión 7), generado en la aplicación |
| **C05** | Claves foráneas | `<entidad_singular>_id`, sin cascada de borrado |
| **C06** | Marcas de tiempo | `timestamptz` en UTC; reloj propio y reloj del canal separados |
| **C07** | Enumeraciones | Declaradas explícitamente como «enumeration» en el modelo |
| **C08** | Valores monetarios | `numeric(14,2)` — **nunca** punto flotante — más `currency` |
| **C09** | Borrado | Lógico por defecto; `messages` no admite borrado (registro de hechos) |
| **C10** | Diagramación | Mermaid por defecto; Miro para el DER y el diagrama de clases |

---

## 2. Las decisiones en detalle

### C01 — Nomenclatura del agente IA y del operador humano

**Decisión:** el agente de inteligencia artificial se llama **`AIAgent`**. La persona que atiende conversaciones se llama **`Operator`**.

**Justificación.** En la jerga de CRM conversacional, *"agent"* designa al **operador humano**, no al bot. Eso incluye a Chatwoot, que el propio anteproyecto releva en la sección 6.1 como una de las herramientas de referencia del estado del arte. Nombrar `Agent` al agente de IA y `User` a la persona invierte la convención del dominio: cualquier lector con experiencia en el rubro —un tutor, un evaluador, o el propio cooperante— va a leer el modelo al revés durante los primeros minutos.

El costo de sostener la nomenclatura invertida es acumulativo: aparece en cada diagrama, en cada tabla, en cada conversación con un tercero y en la defensa. El costo de corregirlo hoy, sin modelo ni código escrito, es cero.

**Reglas:**

- `AIAgent` en el modelo, `ai_agents` como tabla, `ai_agent_id` como FK.
- `Operator` en el modelo, `operators` como tabla, `operator_id` como FK.
- La palabra *"agente"* sin calificar queda **prohibida** en la documentación técnica. Siempre "agente IA" u "operador".
- En la documentación en español se admite "operador" y "agente IA"; los identificadores técnicos van en inglés (ver C02).

**Consecuencia sobre el Anexo B.** Los tres roles del anteproyecto son *User (operador básico)*, *Super Agent* y *Admin*. Con esta decisión, "Super Agent" queda especialmente confuso, porque contiene justamente la palabra que estamos reasignando. La traducción coherente es:

| Anexo B | Identificador técnico | Capacidades (sin cambios respecto del Anexo B) |
|---|---|---|
| User (operador básico) | `operator` | Responde conversaciones asignadas; activa o desactiva el agente IA en un chat |
| Super Agent | `supervisor` | Todo lo anterior + reasignar conversaciones y ver las de todo el equipo |
| Admin | `admin` | Todo lo anterior + gestión de agentes IA, usuarios, etiquetas y webhooks |

> Este renombre **cambia terminología del anteproyecto** y debe reflejarse en la versión final del informe para que no aparezca como inconsistencia. Las capacidades no se tocan: son exactamente las del Anexo B.

> **Enmendada el 2026-09-13** por [ADR-0035](02-decisiones/0035-reglas-de-la-bandeja-manual.md), regla 10. El `operator` **ya no activa ni desactiva el agente IA**: el modo de una conversación lo cambian `supervisor` y `admin`. Es una desviación declarada de F27 y del Anexo B, y la tabla de arriba queda como estaba para que se vea de dónde partió. Desde esta enmienda, *"sin cambios respecto del Anexo B"* deja de ser cierto para esa capacidad.

---

### C02 — Idioma

**Decisión:**

| Ámbito | Idioma |
|---|---|
| Nombres de tablas, columnas, enumeraciones y sus valores | **Inglés** |
| Código, nombres de funciones, variables y comentarios | **Inglés** |
| Documentación, ADRs, informe, diagramas (etiquetas descriptivas) | **Español** |

**Justificación.** El Anexo C mezcla los dos idiomas dentro de una misma estructura: define `components_json` en inglés pero con claves `voz_marca` y `umbral_transferencia` en español adentro. Esa mezcla obliga a recordar, campo por campo, en qué idioma quedó cada uno — y garantiza errores de tipeo en las consultas.

El inglés técnico gana porque el vocabulario del dominio ya viene en inglés y traducirlo produce nombres peores: `webhook`, `prompt`, `embedding`, `chunk`, `token`, `inbox` no tienen traducción usada en la práctica. Traducir la mitad y dejar la otra es lo que produce `components_json.voz_marca`.

El español gana en la documentación porque el trabajo se escribe, se presenta y se defiende en español.

**Reglas:**

- Ningún identificador técnico en español. Ni siquiera parcialmente.
- Los comentarios de código van en inglés, para que convivan con nombres en inglés sin cambiar de idioma a mitad de línea.
- Los diagramas llevan nombres de entidades en inglés y notas explicativas en español.
- Las palabras del dominio del negocio que no tienen equivalente natural (por ejemplo, tipos de contacto) se traducen igual: `client`, `provider`, `prospect`.

---

### C03 — Estilo de nombres

**Decisión:**

| Elemento | Regla | Ejemplo |
|---|---|---|
| Tabla | `snake_case`, **plural** | `conversations`, `ai_agents`, `quick_replies` |
| Columna | `snake_case`, singular | `phone_whatsapp`, `content_text` |
| Clave primaria | Siempre `id` | `id` |
| Clave foránea | `<entidad_singular>_id` | `conversation_id`, `ai_agent_id` |
| Tabla de unión | Las dos entidades en plural, orden alfabético | `contact_labels`, `conversation_labels` |
| Booleano | Adjetivo o participio, sin prefijo `is_` redundante | `active`, `starred`, `blocked` |
| Instante | Sufijo `_at` | `created_at`, `resolved_at` |
| Duración | Sufijo con unidad | `latency_ms`, `timer_ms_used` |
| Contador | Sufijo `_count` | `unread_count`, `messages_count` |
| Campo cifrado | Sufijo `_encrypted` | `dni_encrypted`, `cbu_encrypted` |
| Enumeración (modelo) | `PascalCase` | `ConversationStatus` |
| Enumeración (base) | `snake_case` | tipo `conversation_status`, valor `active` |
| Índice | `idx_<tabla>_<columnas>` | `idx_messages_conversation_id_created_at` |
| Restricción única | `uq_<tabla>_<columnas>` | `uq_contacts_phone_whatsapp` |

**Justificación.** Tablas en plural porque una tabla contiene un conjunto de filas; la fila individual es la entidad singular. Esa distinción hace que el modelo se lea solo: `conversations` tiene muchas `Conversation`.

PK siempre `id` y FK siempre `<entidad>_id` porque vuelve las uniones predecibles: `messages.conversation_id = conversations.id` no requiere consultar el esquema. El Anexo C ya sigue mayormente este patrón; acá se lo declara como regla.

`snake_case` porque PostgreSQL pliega a minúsculas los identificadores sin comillas: `contentText` se convierte silenciosamente en `contenttext`. Usar `snake_case` evita depender de comillas dobles en cada consulta.

Sufijo de unidad en las duraciones porque un campo llamado `latency` obliga a adivinar si son milisegundos o segundos. `latency_ms` no.

**Nomenclatura del Anexo C que cambia:**

| Anexo C | Convención | Motivo |
|---|---|---|
| `agents` | `ai_agents` | C01 |
| (rol) *User* | `operators` | C01 |
| `knowledge_base` | `knowledge_bases` | Plural (C03) |
| `mode_default` | `default_mode` | Orden natural en inglés: el adjetivo precede al sustantivo |
| `phone_whatsapp` | `phone_whatsapp` | Se conserva |
| `content_text`, `content_type` | Se conservan | Ya cumplen |

---

### C04 — Clave primaria: UUID

**Decisión:** todas las tablas usan **`UUID` como clave primaria**, en columna de tipo nativo `uuid` (no `varchar`), **generado en la aplicación** y no por la base de datos. Se adopta **UUID versión 7** (ordenable temporalmente).

**Justificación.** Cuatro razones, en orden de peso para este sistema:

1. **El identificador tiene que existir antes de tocar la base.** La sección 8.5 del anteproyecto describe un gateway que recibe webhooks de Meta y los enruta al agregador, que a su vez agrupa mensajes fragmentados antes de procesarlos. Ese flujo construye en memoria un grafo de objetos relacionados —el bloque agregado, los mensajes que lo componen, la respuesta generada— y recién después lo persiste. Con enteros autoincrementales cada relación exige insertar el padre, recuperar su `id` y recién entonces insertar los hijos, encadenando viajes a la base dentro de la ruta caliente. Con UUID el grafo se arma completo y se escribe en una sola transacción.

2. **Los identificadores cruzan el borde entre los dos desplegables, que no comparten transacción.** El núcleo le manda al cerebro el `message_batch_id` del turno y el cerebro le devuelve el `generation_id` de una fila que escribió en su propia transacción; recién después el núcleo cierra `generations.message_id` en la suya ([`07-secuencia-del-turno.md`](07-secuencia-del-turno.md) §2). Como ADR-0017 (i).3 prohíbe que una transacción cruce ese borde, cada lado tiene que conocer el identificador en el momento de armar el payload, sin depender de quién confirmó primero.

> **Lo que este motivo no dice** *(corregido el 2026-09-05)*. La redacción original afirmaba que el UUID *"elimina toda coordinación de secuencias entre procesos"*. **Eso es falso en este sistema:** hay **una sola** PostgreSQL —es lo innegociable de ADR-0017— y una secuencia de la base ya garantiza unicidad entre procesos sin que ninguno le pregunte nada a otro. El argumento valdría con una base por servicio, que es justamente la arquitectura descartada. Lo que queda en pie es lo de arriba, que es más angosto: el peso de esta decisión lo lleva el motivo 1.

3. **Idempotencia frente a los reintentos de Meta.** La plataforma de WhatsApp reintenta la entrega de webhooks ante timeouts, lo que significa que el mismo mensaje puede llegar más de una vez (F20 pide explícitamente "reintentos y log de eventos"). El mecanismo de deduplicación se apoya en la clave natural del mensaje que provee Meta, no en la PK; con UUID, un reintento descartado no consume números de secuencia ni deja huecos que después haya que explicar.

4. **No expone el volumen del negocio.** Un entero secuencial visible en una URL o en una respuesta de API revela cuántos contactos, conversaciones o ventas tiene el cooperante. Es información comercial que no hay motivo para publicar.

**Costo asumido y su mitigación.** Un UUID ocupa 16 bytes contra 8 de un `bigint`, y el costo se paga en cada índice. En `messages` —que por la naturaleza del sistema va a ser la tabla más grande, con varias filas por conversación— es un costo real, no teórico.

Peor aún: el UUID versión 4, que es aleatorio, distribuye las inserciones por todo el índice B-tree y produce fragmentación de páginas. **Por eso se adopta la versión 7**, cuyos bits más significativos son una marca de tiempo: los identificadores generados consecutivamente quedan próximos entre sí, las inserciones vuelven a concentrarse al final del índice y se recupera la localidad que hace eficiente al `bigint`. Se acepta el costo de espacio; se elimina el de fragmentación.

**Reglas:**

- Tipo `uuid` nativo de PostgreSQL. Nunca `varchar(36)`: duplica el espacio y pierde la validación.
- Generación en la aplicación, no con `DEFAULT gen_random_uuid()` en la base — porque el motivo 1 exige tener el ID antes del `INSERT`.
- Versión 7. Si la biblioteca del lenguaje aún no la ofrece, se usa una implementación explícita, no la v4 "provisoriamente".
- Las claves naturales (el identificador de mensaje de Meta, el teléfono de un contacto) **no son claves primarias**: son restricciones únicas. Una PK no debe poder cambiar de valor.

---

### C05 — Claves foráneas

**Decisión:** las FK se nombran `<entidad_singular>_id`, se declaran siempre a nivel de base de datos, y **no se usa borrado en cascada**.

**Justificación.** La cascada de borrado es incompatible con C09: si el borrado es lógico, no hay `DELETE` que cascadee, y si algún día lo hubiera, arrastraría en silencio el historial de conversaciones que F32 ("historial unificado por contacto") exige preservar. Un contacto dado de baja no puede llevarse consigo sus conversaciones, sus ventas ni sus métricas: los datos históricos siguen siendo válidos.

Las FK se declaran en la base y no solo en la aplicación porque son la única garantía de integridad que sobrevive a un script manual, a una migración a medias o a un módulo que escribe por fuera del camino previsto.

**Reglas:**

- `ON DELETE RESTRICT` por defecto.
- Cuando una FK es opcional se documenta **por qué** puede ser nula. Ejemplo: una conversación en modo autónomo no tiene operador asignado, lo que hace que `operator_id` sea legítimamente nulo.
- Cuando dos FK apuntan a la misma tabla desde una misma entidad, el nombre lleva el rol: `from_operator_id` / `to_operator_id`.
- Una relación se modela **en un solo lugar**. Si la entidad A tiene `b_id` y la entidad B tiene `a_id` para expresar el mismo vínculo, eso no es redundancia: es una inconsistencia esperando ocurrir.

---

### C06 — Marcas de tiempo

**Decisión:** todas las marcas de tiempo son **`timestamptz` almacenadas en UTC**. La conversión a hora local (`America/Argentina/Cordoba`) ocurre en la capa de presentación **y en toda consulta que agrupe por unidad temporal, donde ocurre *antes* de agrupar**.

> **Enmienda del 2026-08-10 (Paso 6).** La redacción original decía que la conversión ocurría *"exclusivamente en la capa de presentación"*, y para F38 eso es insuficiente: la *"distribución horaria/semanal"* **bucketiza**, y bucketizar es parte del cálculo, no de la presentación. Agrupar por hora en UTC y convertir el resultado después corre el pico **tres horas** — el de las 10 de la mañana aparece a las 13. En SQL: `date_trunc('day', ts AT TIME ZONE 'America/Argentina/Cordoba')`, nunca al revés. Ver `05-consultas-de-metricas.sql`, sección F38.

**Justificación de UTC.** F38 pide "distribución horaria/semanal" y F34 "tiempos de respuesta": son métricas que dependen de que todos los instantes sean comparables entre sí. Argentina no aplica hoy horario de verano, pero lo aplicó en el pasado y podría volver a hacerlo; si las marcas se guardan en hora local, una hora del día pasa a existir dos veces al año y otra deja de existir, y las series temporales de F38 quedan irreproducibles. Además el proyecto contempla despliegue real (Fase 4) y las marcas que provee la plataforma de mensajería vienen en UTC: convertirlas al escribir es agregar un punto de falla sin ganancia.

Se usa `timestamptz` y no `timestamp` porque el segundo no guarda ninguna información de zona y depende de que *todo* el sistema recuerde la convención. `timestamptz` la hace explícita.

**Justificación de separar los dos relojes.** Hay dos fuentes de tiempo distintas y **no son intercambiables**:

| Reloj | Campos | Qué mide |
|---|---|---|
| **Propio** | `created_at`, `updated_at` | Cuándo nuestro sistema registró el hecho |
| **Del canal** | `sent_at`, `delivered_at`, `read_at` | Cuándo la plataforma de mensajería informa que ocurrió |

Pueden diferir por segundos o por minutos: un webhook demorado, un reintento, o una caída del gateway separan ambos relojes. Si se usa `created_at` para calcular el tiempo de respuesta de F34, se está midiendo la latencia de nuestra propia base de datos, no la experiencia del cliente — que es lo que la métrica dice medir. El Anexo C define hoy un único `created_at` en `messages`; esta convención exige que la marca del canal se persista por separado.

**Reglas:**

- Toda tabla lleva `created_at`, no nulo.
- `updated_at` solo en tablas mutables. Las entidades inmutables (registros de auditoría, versiones históricas) **no llevan `updated_at`**: si lo llevaran, sugerirían que pueden modificarse.
- Instantes con sufijo `_at`; duraciones con sufijo de unidad (`_ms`, `_seconds`).
- **Una duración que es una métrica se persiste, no se recalcula.** Si un valor aparece en un objetivo específico o en un requisito de métricas, se guarda en el momento en que se conoce.
- Ninguna columna de fecha se guarda como texto.

---

### C07 — Enumeraciones

**Decisión:** todo atributo con un conjunto cerrado de valores se declara **explícitamente como enumeración** en el modelo, con el estereotipo «enumeration» en los diagramas.

**Justificación.** El Anexo C ya usa enumeraciones en todas partes, pero sin declararlas: escribe `status (active/waiting/resolved)`, `mode (autonomous/suggestion/off)`, `direction (inbound/outbound)`, `content_type (text/audio/image)` y `type (client/provider/prospect)` como si fueran comentarios al margen de un atributo de texto. La consecuencia práctica es que nada impide que en el código aparezca `"resolved"`, `"Resolved"` y `"resuelto"` conviviendo, y que una consulta de métricas cuente mal sin fallar.

Declararlas es además una exigencia de notación UML: un tipo enumerado usado sin declarar es una observación estándar en la revisión de un diagrama de clases.

**Enumeraciones derivadas del anteproyecto** (valores tomados textualmente de los Anexos B y C):

| Enumeración | Valores | Origen |
|---|---|---|
| `ContactType` | `client` · `provider` · `prospect` | Anexo C — `contacts.type` |
| `OperatorRole` | `operator` · `supervisor` · `admin` | Anexo B (ver C01) |
| `AgentMode` | `autonomous` · `suggestion` · `off` | Anexo C — `agents.mode_default`, `conversations.mode`; F02, F27 |
| `ConversationStatus` | `active` · `waiting` · `resolved` | Anexo C — `conversations.status` |
| `MessageDirection` | `inbound` · `outbound` | Anexo C — `messages.direction` |
| `MessageContentType` | `text` · `audio` · `image` | Anexo C — `messages.content_type` |

> Nota de nombre: se la llama `MessageContentType` y no `MessageType` para que el nombre de la enumeración coincida con el de la columna que tipa (`content_type`), según C03. Si más adelante se necesita una categorización distinta del mensaje, `MessageType` queda libre para ella.

**Enumeraciones previstas.** Los pasos siguientes de la etapa de diseño van a introducir más conjuntos cerrados —estado de entrega de un mensaje, tipo de emisor, tipo de señal de transferencia, estado de un documento en el pipeline de ingesta, estado de una plantilla, entre otros—. Todas quedan sujetas a esta misma convención desde el momento en que se definan; no se agregan acá porque las entidades que las contienen todavía no están decididas.

**Reglas:**

- Nombre en `PascalCase` en el modelo y los diagramas; tipo en `snake_case` en la base; valores en `snake_case` y en inglés.
- **Implementación en PostgreSQL:** durante el diseño y el desarrollo del núcleo se usa `varchar` + restricción `CHECK`, no el tipo `ENUM` nativo. Motivo: los conjuntos de valores todavía van a cambiar, y aunque agregar un valor a un `ENUM` nativo es sencillo, renombrar o eliminar uno es una operación costosa que obliga a recrear el tipo y todas las columnas que lo usan. Cuando los conjuntos se estabilicen se puede reevaluar.
- Ningún atributo enumerado se documenta como texto libre en un diagrama: si tiene valores cerrados, se declara.
- Los valores no se reutilizan entre enumeraciones distintas con significados distintos.

---

### C08 — Valores monetarios

**Decisión:** todo importe se almacena como **`numeric(14,2)`**. Se prohíbe el punto flotante (`float`, `double`, `real`) para dinero, en la base y en todos los lenguajes. Todo importe va acompañado de una columna **`currency`** de tres caracteres (ISO 4217), con valor por defecto `ARS`.

**Justificación.** El punto flotante binario no puede representar exactamente valores decimales como `0.10`, de modo que las sumas acumulan error. F35 pide "ventas generadas, **monto atribuido** y tasa de conversión por operador", es decir, agregaciones sobre importes: exactamente el caso donde el error se acumula y donde dos consultas equivalentes pueden devolver totales distintos. Un informe de métricas que no cuadra con la suma manual es indefendible, y el problema es tanto más visible cuanto mayores son los montos — relevante para el cooperante del proyecto, cuya actividad son alquileres de departamentos (sección 7.3).

`numeric` en PostgreSQL es aritmética decimal exacta. La escala 2 cubre la moneda argentina y la precisión 14 admite importes muy por encima de cualquier operación esperable.

La columna `currency` se incluye aunque hoy todo sea en pesos: agregarla ahora cuesta una columna; agregarla después de tener datos obliga a decidir retroactivamente en qué moneda estaba cada fila histórica.

**Reglas:**

- En Python: `decimal.Decimal`. Nunca `float`.
- En Go: un tipo decimal de precisión arbitraria. Nunca `float64`.
- La conversión a texto para presentación ocurre solo en la capa de presentación.
- Ningún importe viaja como número de punto flotante en JSON: se serializa como cadena o como entero de centavos, con la convención declarada en el contrato.

---

### C09 — Política de borrado

**Decisión:** el borrado es **lógico** por defecto. Y hay una distinción que este sistema necesita hacer explícita: **eliminar, ocultar y anonimizar son tres operaciones distintas** y no deben compartir mecanismo.

#### Los tres verbos

| Verbo | Mecanismo | Significa | Se aplica a |
|---|---|---|---|
| **Eliminar** | `deleted_at` + `deleted_by_operator_id` | "Ya no se usa" | Entidades de configuración y de negocio |
| **Ocultar** | `hidden_at` | "No se muestra en la bandeja, pero ocurrió" | Solo donde se justifique, con semántica de presentación |
| **Anonimizar** | `redacted_at` + reemplazo del contenido | "Ocurrió, pero el dato personal se suprimió" | Contenido con datos personales |

#### El caso de `messages`

**`messages` no admite borrado, ni físico ni lógico.**

El razonamiento importa, porque es el punto donde F29 se malinterpreta con facilidad. F29 documenta que *"los mensajes enviados no pueden eliminarse"* como **limitación de la API de Meta**. Esa restricción es del **canal**, no de nuestra base de datos: si marcáramos un mensaje como borrado, eso jamás se propagaría a WhatsApp — el cliente ya lo recibió y lo tiene en su teléfono. Marcar un mensaje como eliminado solo podría significar "no lo muestres en la bandeja", nunca "no ocurrió".

Y ahí está el problema real: `messages` es el **registro de hechos** sobre el que se calculan las métricas. F34 (volumen y tiempos de respuesta), F35 (ventas por operador), F36 (tasa de resolución autónoma y de transferencia) y el OE7 (resolución autónoma ≥75 %) se apoyan todos en contar mensajes. Si las filas de `messages` pudieran marcarse como borradas, cada consulta de métricas tendría que decidir si filtrarlas o no, y **el mismo objetivo daría resultados distintos según quién escribiera la consulta**. Un registro de hechos que admite borrado deja de servir para medir.

Por lo tanto:

- Si hace falta sacar un mensaje de la vista del operador, es `hidden_at`, con semántica declarada de presentación, y las métricas lo siguen contando.
- Si un contacto ejerce el derecho de supresión de la Ley 25.326 —contemplada en el alcance del proyecto, sección 7.3—, la fila **se conserva** y se anonimiza el contenido: `redacted_at` más el reemplazo de los campos de texto. La conversación existió y las métricas siguen siendo válidas; el dato personal ya no está.

Esta distinción es también la respuesta honesta a una tensión que el proyecto tiene incorporada: F29 dice que los mensajes no se borran, y la Ley 25.326 otorga derecho de supresión. No son contradictorias una vez que se separan "borrar el hecho" de "suprimir el dato personal". Conviene que el informe lo explique en esos términos.

#### Entidades inmutables

Las entidades de **auditoría y de versionado** que definan los pasos siguientes de la etapa de diseño no admiten borrado de ninguna clase, ni `updated_at`. Su valor es precisamente que no se puedan modificar: un registro de auditoría editable no prueba nada.

#### Reglas operativas del borrado lógico

Estas son las tres trampas del borrado lógico, y conviene tenerlas escritas antes de escribir la primera consulta:

1. **Toda consulta debe filtrar `deleted_at IS NULL`.** Olvidarlo es el error más frecuente y el más silencioso: la consulta funciona y devuelve datos de más. Mitigación: exponer vistas que ya incluyan el filtro, o aplicarlo por defecto en la capa de acceso a datos, de modo que incluirlo sea el camino fácil y omitirlo requiera un acto deliberado.

2. **La unicidad debe ser parcial.** El Anexo C define `contacts.phone_whatsapp` como único. Con borrado lógico, un contacto dado de baja seguiría ocupando ese número y bloquearía el alta de un contacto nuevo con el mismo teléfono — que es un escenario perfectamente normal. La restricción correcta es un índice único parcial:

   ```sql
   CREATE UNIQUE INDEX uq_contacts_phone_whatsapp
       ON contacts (phone_whatsapp)
       WHERE deleted_at IS NULL;
   ```

3. **El borrado lógico no cascadea y no anonimiza.** Las filas que apuntan a una entidad eliminada siguen siendo válidas: F32 exige el historial unificado por contacto, y ese historial sobrevive a la baja del contacto. Y `deleted_at` **no** satisface el derecho de supresión: para eso está la anonimización.

---

### C10 — Herramientas de diagramación

**Decisión:**

| Artefacto | Herramienta |
|---|---|
| **Modelo de datos lógico (DER)** | **Miro** |
| **Diagrama de clases** | **Miro** |
| Diagramas de secuencia | **Mermaid** (`sequenceDiagram`) |
| Máquinas de estados | **Mermaid** (`stateDiagram-v2`) |
| Árboles de decisión y flujos | **Mermaid** (`flowchart`) |
| Diagramas de flujo de datos | **Mermaid** (`flowchart` con `subgraph`) |
| Cronograma | **Mermaid** (`gantt`) |

Mermaid es la herramienta **por defecto**: todo diagrama que no sea el DER o el de clases se escribe en Mermaid.

**Justificación.** El DER y el diagrama de clases son los dos artefactos donde el **layout manual** es una ventaja decisiva. El modelo de SONIA va a superar las treinta entidades, y las herramientas de layout automático producen, a esa escala, diagramas que se pueden generar pero no leer, sin forma práctica de corregirlos. Son además los dos artefactos que más se discuten entre los tres autores y donde trabajar sobre un canvas compartido en tiempo real vale más que versionarlos.

Para el resto —secuencias, estados, flujos, DFD— Mermaid gana con claridad: son diagramas chicos, el layout automático rinde bien, el archivo vive en el repositorio junto al código, un `git diff` muestra qué cambió del modelo y no de la imagen, y GitHub los renderiza sin instalar nada.

**Costo asumido.** Miro no se versiona: no hay historial de cambios ni revisión por diferencias sobre el DER ni sobre el diagrama de clases, que son justamente los artefactos que más van a cambiar. Se acepta deliberadamente, a cambio del layout manual y de la colaboración en vivo, y se mitiga con las reglas de exportación de abajo.

**Reglas:**

- **Mermaid:** archivos `.mmd` en `docs/diagrams/`, un archivo por diagrama, nombre en `kebab-case` y en español (por C02, los nombres de archivo son documentación).
- **Miro:** ~~el tablero es la fuente de verdad del DER y del diagrama de clases~~ → **enmendado por [ADR-0013](02-decisiones/0013-fuente-de-verdad-del-modelo.md): la fuente de verdad es el catálogo** (`DER/entidades.md` + `DER/relaciones.md`), y el tablero es su representación legible. Se cambia primero en el catálogo y después en el tablero; un cambio que sólo aparece en el tablero no está tomado. Miro sigue siendo la herramienta de dibujo, por el argumento de layout manual. Al cerrar cada paso de la etapa de diseño se exporta a `docs/diagrams/` en PNG y PDF, con la fecha en el nombre: `der-logico-2026-08-14.png`. Sin exportación fechada no hay forma de saber qué versión del modelo corresponde a qué momento del trabajo.
- **El diagrama de clases se deriva del DER.** Se actualizan juntos, en la misma sesión, o no se actualiza ninguno. Mantener a mano un derivado en otro tablero es la forma más rápida de que los dos se contradigan.
- **Coherencia de notación en el diagrama de clases**, porque Miro no la valida sola. Dos reglas mínimas: las claves foráneas se representan **siempre de la misma manera** en todas las entidades —o como atributos, o como asociaciones, nunca mezclado—; y las enumeraciones se declaran con el estereotipo «enumeration» (C07).
- Las restricciones que no son expresables como relación se escriben como **nota** en el diagrama. Ejemplo: F09 establece que solo un Admin puede crear respuestas rápidas — eso no es una cardinalidad, es una regla, y va anotada.
- Todo diagrama lleva título, fecha y una nota de una línea con su propósito.

---

## 3. Checklist de aplicación

Antes de dar por cerrado cualquier diagrama, esquema o migración:

- [ ] No aparece `Agent` refiriéndose al agente IA, ni `User` refiriéndose al operador (C01).
- [ ] No hay ningún identificador técnico en español (C02).
- [ ] Tablas en plural, columnas en `snake_case`, PK `id`, FK `<entidad>_id` (C03).
- [ ] Todas las PK son `uuid`; ninguna clave natural se usa como PK (C04).
- [ ] Ninguna FK tiene borrado en cascada; las FK opcionales tienen documentado por qué (C05).
- [ ] Todas las marcas de tiempo son `timestamptz`; el reloj propio y el del canal están separados (C06).
- [ ] Todo atributo de valores cerrados está declarado como «enumeration» (C07).
- [ ] Ningún importe es de punto flotante; todos tienen `currency` (C08).
- [ ] `messages` no tiene columna de borrado; las restricciones únicas sobre tablas con borrado lógico son parciales (C09).
- [ ] El DER y el diagrama de clases coinciden entre sí y están exportados con fecha (C10).

---

## 4. Consecuencias que requieren confirmación del equipo

Tres decisiones de este documento tienen efectos fuera de él y conviene confirmarlas explícitamente:

1. **El renombre de los roles del Anexo B** (`User` → `operator`, `Super Agent` → `supervisor`). Cambia terminología del anteproyecto y debe reflejarse en el informe final. Las capacidades no se modifican. **Enmendado el 2026-09-13 por [ADR-0035](02-decisiones/0035-reglas-de-la-bandeja-manual.md):** hay una excepción, el cambio de modo del agente IA, que pasa del `operator` a `supervisor` y `admin`. También va al informe, como desviación de F27.
2. **La separación entre eliminar, ocultar y anonimizar**, y la lectura de F29 como restricción del canal y no de la base. Es la interpretación que hace compatibles a F29 y al derecho de supresión de la Ley 25.326, y merece un párrafo en el informe.
3. **El nombre `MessageContentType`** en lugar de `MessageType`, para que la enumeración coincida con la columna que tipa.

Ninguna de las tres bloquea el avance de los pasos siguientes.
