# SONIA — Catálogo de entidades y atributos

| Campo | Valor |
|---|---|
| **Documento** | `docs/design/DER/entidades.md` |
| **Estado** | Borrador para el Paso 5 (modelo de datos lógico) |
| **Fecha** | 2026-08-06 · revisado 2026-08-08 (auditoría del catálogo) · revisado 2026-08-10 (Paso 6 — validación de las métricas: H-18) · revisado 2026-09-12 (Fase 4 — secretos de `inboxes` nullables) · revisado 2026-09-25 (Fase 8 — la nota de `pipeline_steps.version` se lee en plural para la corrida de salida de `blocklist`: [ADR-0042](../02-decisiones/0042-pipeline-de-decision-del-turno.md)) · revisado 2026-09-26 (Fase 9 — `generations.reasoning_json` queda sin uso: la evidencia de F05 es `generation_components`, [ADR-0049](../02-decisiones/0049-visualizacion-de-los-componentes-de-cada-respuesta.md)) |
| **Base documental** | [Anteproyecto](../../SONIA_Anteproyecto.pdf) (Anexos A, B y C; secciones 7.2, 7.3, 8.4, 8.5) · [00-convenciones.md](../00-convenciones.md) · [01-trazabilidad.md](../01-trazabilidad.md) · [02-decisiones/](../02-decisiones/) |
| **Complemento** | [relaciones.md](relaciones.md) — este documento no describe relaciones |

---

## 0. Cómo leer este documento

Lista **todas las entidades** del sistema con sus atributos. Las relaciones se documentan aparte, en [relaciones.md](relaciones.md).

**Cada entidad lleva una línea `Origen`** que la traza a su fuente: un requisito del Anexo A (F01–F39), un objetivo (OE1–OE7), una entidad del Anexo C, un hallazgo de la [matriz de trazabilidad](../01-trazabilidad.md) (H-00–H-17) o una decisión registrada (ADR-0001–0011). **Ninguna entidad aparece sin origen.** Los pocos atributos que responden a una necesidad de ingeniería sin anclaje textual directo están marcados con **⚙** y justificados en el lugar.

### Convenciones aplicadas

Resumen de [00-convenciones.md](../00-convenciones.md), que es la fuente de verdad:

- Identificadores en **inglés**; documentación en español (C02).
- `snake_case`, tablas en **plural**, PK `id`, FK `<entidad>_id` (C03).
- **PK `uuid` versión 7**, generada en la aplicación (C04).
- FK sin cascada de borrado (C05).
- Marcas de tiempo `timestamptz` en **UTC**; sufijo `_at` para instantes, `_ms` para duraciones. Reloj propio (`created_at`) separado del reloj del canal (`sent_at`, `delivered_at`, `read_at`) (C06).
- Enumeraciones declaradas explícitamente; implementadas como `varchar` + `CHECK` durante el desarrollo (C07).
- Dinero en `numeric(14,2)` con `currency`; nunca punto flotante (C08).
- **Borrado lógico** (`deleted_at`) por defecto. `messages` **no admite borrado** de ninguna clase; las entidades inmutables tampoco llevan `updated_at` (C09).

> **Nota de nomenclatura:** `order` es palabra reservada en SQL. Donde el orden importa se usa **`position`**.

---

## 1. Enumeraciones

| Enumeración | Valores | Origen |
|---|---|---|
| `ContactType` | `client` · `provider` · `prospect` | Anexo C — `contacts.type` |
| `OperatorRole` | `operator` · `supervisor` · `admin` | Anexo B (C01) |
| `AgentMode` | `autonomous` · `suggestion` · `off` | Anexo C; F02, F27 |
| `ConversationStatus` | `active` · `waiting` · `resolved` | Anexo C — `conversations.status` |
| `MessageDirection` | `inbound` · `outbound` | Anexo C — `messages.direction` |
| `MessageContentType` | `text` · `audio` · `image` | Anexo C — `messages.content_type` |
| `SenderType` | `contact` · `ai_agent` · `operator` · `system` | H-09 |
| `MessageStatus` | `sent` · `delivered` · `read` · `failed` | F20 (webhooks de estado del canal) |
| `SourceChannel` | `meta_ads` · `qr` · `referral` · `direct` | F15; ADR-0001 |
| `PromptComponentType` | `objective` · `personality` · `behavior` · `tone` · `brand_voice` · `rules` | F01 |
| `TransferSignalType` | `no_resolution` · `frustration` · `out_of_domain` · `explicit_request` | F12 |
| `BatchCloseReason` | `silence` · `max_messages` · `max_duration` | F10 (`silence` es el único textual; los otros dos son guardas ⚙) |
| `PipelineStep` | `blocklist` · `guardrail` · `triage` · `retrieval` · `reranking` · `grounding` · `generation` | ADR-0016 ⚙ — los pasos del pipeline de respuesta. **Sin anclaje textual salvo `retrieval` (F06) y `generation`**; el resto son decisiones de ingeniería del equipo |
| `GenerationStatus` | `auto_sent` · `pending_review` · `accepted` · `edited` · `rejected` · `blocked` | F02 (modo sugerencia) · **`blocked` (ADR-0016, Paso 8)**: la frenó el guardrail de salida, no una persona. Los otros cinco describen resultados de revisión humana y son anteriores a que existiera un corte automático posterior a la generación; usar `rejected` para eso afirmaría que un operador la rechazó, que es falso. **`sent` renombrado a `auto_sent`**: colisionaba con `MessageStatus.sent` con otro significado, que C07 prohíbe expresamente |
| `DocumentStatus` | `pending` · `processing` · `indexed` · `failed` | ADR-0005 |
| `MetaTemplateStatus` | `approved` · `pending` · `rejected` | F08, F37 |
| `AttributeSource` | `manual` · `llm` | F31 |
| `AnnotationType` | `transfer` · `frustration` · `intent` | OE3, OE4 |
| `WebhookEventType` | `message` · `status` · `unknown` | F20 · §8.5 ⚙ — el payload de la Cloud API trae mensajes entrantes o cambios de estado; `unknown` cubre lo que llegue sin reconocer |
| `WebhookEventStatus` | `received` · `processed` · `failed` · `discarded` | F20 (*"reintentos y log de eventos"*). `discarded` = firma inválida (§8.5) |
| `InboxStatus` | `active` · `inactive` · `error` | F20 ⚙ — estado de la conexión de la bandeja con el canal |
| `MetaTemplateCategory` | `marketing` · `utility` · `authentication` | F08, F37 — **el conjunto lo fija Meta**, no el proyecto |

---

## 2. Bloque 1 — Núcleo

### `contacts`

> **Origen:** Anexo C · F30, F31, F32 · ADR-0001 (origen) · ADR-0006 (cifrado)

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `account_id` | uuid | FK. Entidad raíz (ADR-0012) |
| `phone_whatsapp` | varchar | Único **parcial** (`WHERE deleted_at IS NULL`), por C09 |
| `name` | varchar | |
| `lastname` | varchar | |
| `email` | varchar | Nullable |
| `type` | `ContactType` | |
| `notes` | text | Nullable |
| `source_channel` | `SourceChannel` | Fuente de verdad del origen (ADR-0001). Inmutable tras el alta |
| `dni_encrypted` | bytea | ADR-0006 |
| `cuit_encrypted` | bytea | ADR-0006 |
| `cuil_encrypted` | bytea | ADR-0006 |
| `cbu_encrypted` | bytea | ADR-0006 |
| `consent_given_at` | timestamptz | Anexo C; sección 7.3. **Nullable**: un consentimiento que todavía no ocurrió no tiene instante, y `NULL` dice *"no lo dio"* sin fingir que lo dio cuando se cargó el contacto. Hoy se escribe siempre `NULL`, porque **nada en el sistema le pregunta al cliente todavía**; el día que exista ese acto, se sella con su instante |
| `consent_revoked_at` | timestamptz | Nullable. Derecho de supresión de la Ley 25.326 (C09) |
| `opt_out` | boolean | ⚙ El contacto pidió no recibir más mensajes. **Distinto de `consent_revoked_at`**: revocar el consentimiento alcanza al tratamiento de sus datos; el opt-out alcanza al envío. Se pueden dar por separado |
| `blocked` | boolean | ⚙ Bloqueado por el equipo (spam, abuso). Decisión nuestra, no del contacto |
| `created_at` / `updated_at` / `deleted_at` | timestamptz | |

### `conversations`

> **Origen:** Anexo C · F13, F22, F23, F25, F27, F34 · H-12 · ADR-0002, ADR-0004, ADR-0008

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `contact_id` | uuid | FK |
| `inbox_id` | uuid | FK. **Por qué bandeja —y por lo tanto por qué número— entró** (ADR-0012) |
| `ai_agent_id` | uuid | **Nullable** — en modo `off` no hay agente. Se inicializa con `inboxes.default_ai_agent_id` (ADR-0012) |
| `operator_id` | uuid | **Nullable** — pool sin asignar (ADR-0004). Desde ADR-0037 toda conversación recibe vendedor cuando el cliente escribe, así que es nulo sólo si llegó cuando no había ningún vendedor activo; el modo autónomo ya no lo deja nulo |
| `status` | `ConversationStatus` | |
| `mode` | `AgentMode` | Modo efectivo de esta conversación (F27) |
| `starred` | boolean | F25 |
| `quality_score` | smallint | Nullable. F13 — el operador califica la conversación cerrada. **Renombrado de `score`**: colisionaba con `tone_evaluations.tone_score`, que mide algo distinto (juicio de tono del LLM, no calificación humana) |
| `scored_by_operator_id` | uuid | Nullable. F13 — falta de autor y fecha señalada en la matriz de trazabilidad |
| `scored_at` | timestamptz | Nullable |
| `first_response_at` | timestamptz | Nullable. H-12 — tiempo de respuesta (F34) |
| `last_activity_at` | timestamptz | H-12 — tiempo de espera (F22) |
| `resolved_at` | timestamptz | Nullable. ADR-0002 |
| `resolved_by_operator_id` | uuid | Nullable. ADR-0002 — el cierre es manual |
| `unread_count` | integer | H-12 — filtro "no leídas" (F23) |
| `last_read_at` | timestamptz | Nullable. H-12 |
| `last_inbound_at` | timestamptz | ADR-0008 |
| `window_expires_at` | timestamptz | ADR-0008 — vencimiento de la ventana de 24 hs |
| `created_at` / `updated_at` / `deleted_at` | timestamptz | |

### `messages`

> **Origen:** Anexo C · F21, F22, F29 · H-08 (idempotencia), H-09 (emisor) · C09

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `conversation_id` | uuid | FK |
| `message_batch_id` | uuid | Nullable — sólo los entrantes agrupados (F10) |
| `wamid` | varchar | **Único.** Identificador del mensaje en el canal → idempotencia ante reintentos (H-08) |
| `context_wamid` | varchar | Nullable ⚙ — `wamid` del mensaje al que este responde. WhatsApp permite responder citando un mensaje anterior; sin esto la bandeja no puede reconstruir a qué se refiere (F21, F22) |
| `direction` | `MessageDirection` | |
| `sender_type` | `SenderType` | **H-09.** Sin esto no existe la tasa de resolución autónoma (OE7) |
| `sender_operator_id` | uuid | Nullable — poblado cuando `sender_type = operator` |
| `sender_ai_agent_id` | uuid | Nullable — poblado cuando `sender_type = ai_agent` |
| `content_text` | text | |
| `content_type` | `MessageContentType` | |
| `transcription` | text | Nullable. Anexo C; §8.5 (transcripción de audios) |
| `media_id` | varchar | Nullable ⚙ — identificador del adjunto en el canal |
| `media_url` | text | Nullable ⚙ |
| `media_mime_type` | varchar | Nullable ⚙ |
| `media_size_bytes` | bigint | Nullable ⚙ |
| `media_duration_ms` | integer | Nullable ⚙ — audios |
| `status` | `MessageStatus` | Nullable en entrantes. Webhooks de estado (F20) |
| `error_code` | varchar | Nullable — mensajes fallidos |
| `sent_at` | timestamptz | Reloj del canal (C06) |
| `delivered_at` | timestamptz | Nullable. Reloj del canal |
| `read_at` | timestamptz | Nullable. Reloj del canal |
| `hidden_at` | timestamptz | Nullable. Ocultar de la bandeja — semántica de presentación (C09) |
| `redacted_at` | timestamptz | Nullable. Supresión del dato personal preservando el hecho (C09) |
| `created_at` | timestamptz | Reloj propio |

> ⚙ Los cinco campos de medios no figuran en el Anexo C, pero `content_type` admite `audio` e `image` y §8.5 describe la transcripción de audios: el contenido tiene que estar referenciado en algún lado. **Es un hueco que la matriz de trazabilidad no detectó.**
>
> **`messages` no lleva `deleted_at`** (C09): es el registro de hechos sobre el que se calculan F34, F35, F36 y OE7.

---

## 3. Bloque 2 — Identidad del agente IA

### `ai_agents`

> **Origen:** Anexo C · F01, F02, F17, F31 · C01 (renombre)

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `account_id` | uuid | FK. Entidad raíz (ADR-0012) |
| `name` | varchar | |
| `description` | text | Nullable |
| `default_mode` | `AgentMode` | Renombrado de `mode_default` (C03) |
| `extraction_enabled` | boolean | F31 — *"toggle por agente"*, textual |
| `llm_provider` | varchar | ⚙ §8.4 lista GPT-4o / Claude / Cohere; OE2 depende del modelo |
| `llm_model` | varchar | ⚙ ídem |
| `temperature` | numeric | ⚙ Nullable |
| `max_tokens` | integer | ⚙ Nullable |
| `active` | boolean | Anexo C |
| `created_at` / `updated_at` / `deleted_at` | timestamptz | |

> **No lleva `knowledge_base_id`.** La relación se modela en una sola dirección, desde `knowledge_bases` (ADR-0005, C05).

### `prompt_components`

> **Origen:** F01, F03, F05 · OE1 · H-01

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `ai_agent_id` | uuid | FK |
| `type` | `PromptComponentType` | Los seis de F01 |
| `content` | text | Contenido vigente |
| `position` | smallint | Orden de ensamblado |
| `active` | boolean | OE1 exige el concepto de componente **activo**; habilita ablación. **Distinto de `deleted_at`**: `active = false` es ablación —el componente existe y se puede volver a encender—, `deleted_at` es baja definitiva |
| `created_at` / `updated_at` / `deleted_at` | timestamptz | Borrado lógico por C09. **Lo exigen los dos índices únicos parciales** de [relaciones.md §9](relaciones.md), sobre `(ai_agent_id, type)` y `(ai_agent_id, position)`, que filtran por `deleted_at IS NULL`: un componente apagado sigue ocupando su tipo y su posición ([ADR-0039](../02-decisiones/0039-componentes-del-prompt-y-su-versionado.md)) |

### `prompt_component_versions`

> **Origen:** OE2 — *"¿contra qué configuración se midió?"* · H-01 · **H-20** (`active`)

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `prompt_component_id` | uuid | FK |
| `version_number` | integer | Correlativo por componente |
| `content` | text | Contenido congelado |
| `active` | boolean | **Estado encendido/apagado congelado en esta versión.** Es el lado *"debía"* de OE1 (H-20, ADR-0015) |
| `changed_by_operator_id` | uuid | Nullable |
| `change_note` | text | Nullable |
| `created_at` | timestamptz | **Inmutable: sin `updated_at` ni `deleted_at`** (C09) |

> **Por qué `active` se versiona y `position` no.** OE1 mide que *"el 100 % de los prompts generados incorporen todos los componentes activos"*: es una comparación entre lo que **se debía** ensamblar y lo que **se ensambló**. `generation_components` da el segundo conjunto; sin el histórico de `active`, el primero no existe y la consulta compara un conjunto contra sí mismo — **devuelve 100 % siempre, aunque el ensamblador falle** (H-20).
>
> `position` no se versiona porque **ninguna métrica del anteproyecto mide el orden de ensamblado**, y `generation_components.position` ya registra el orden real usado. Es una omisión decidida, no un olvido.
>
> **Enmienda a la Regla 2 de [03-capa-prompts.md](../03-capa-prompts.md):** se crea una versión nueva cuando cambia `content` **o `active`**. La redacción original sostenía que `active` quedaba congelado por `generation_components` — cierto para *cuáles participaron*, pero eso presupone que participar equivale a estar activo, que es justamente lo que OE1 verifica.

### `transfer_policies`

> **Origen:** F01 (*"incluye la configuración de cuándo delegar a un humano"*) · Anexo C (`umbral_transferencia`, hoy dentro del JSON) · F12

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `ai_agent_id` | uuid | FK |
| `signal_type` | `TransferSignalType` | Una fila por señal de F12 |
| `enabled` | boolean | Apagar la señal sin darla de baja |
| `threshold` | numeric | Umbral de disparo |
| `created_at` / `updated_at` / `deleted_at` | timestamptz | Borrado lógico por C09. **Distinto de `enabled = false`**: apagar una señal es reversible y esperable; darla de baja saca la fila del modelo |

---

## 4. Bloque 3 — Ejecución por turno

### `message_batches`

> **Origen:** F10 · OE3 · H-03

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `conversation_id` | uuid | FK |
| `messages_count` | smallint | |
| `turn_number` | integer | Correlativo por conversación. **Fuente de verdad del número de turno** (ADR-0022, H-19): `conversation_contexts`, `conversation_context_versions` y `transfer_events` lo **copian** desde acá, nunca lo calculan por su cuenta |
| `timer_ms_used` | integer | **La ventana que se aplicó al cerrar el bloque**, en milisegundos ([ADR-0041](../02-decisiones/0041-temporizador-del-turno-por-poller.md), regla 6) — no cuánto duró el bloque, que es `closed_at - opened_at`. Mientras `TURN_BATCH_WINDOW_MS` no cambie, vale lo mismo en todas las filas: **sirve para comparar bloques cerrados con ventanas distintas**, no para demostrar que el timer es adaptativo. Lo adaptativo es el reinicio de la ventana (regla 2), y la evidencia de eso es `closed_at - opened_at` contra `timer_ms_used` |
| `closed_reason` | `BatchCloseReason` | |
| `error_message` | text | Nullable — **por qué falló el turno**, cuando falló. Ver la nota |
| `expires_at` | timestamptz | **Cuándo vence la espera de F10** ([ADR-0041](../02-decisiones/0041-temporizador-del-turno-por-poller.md), regla 1). Se reinicia con cada mensaje que entra al bloque, y es lo único que cambia mientras el bloque está abierto |
| `opened_at` / `closed_at` / `processed_at` | timestamptz | **Inmutable una vez cerrado: sin `created_at` genérico, sin `updated_at` ni `deleted_at`** (C09). `opened_at` cumple la función de `created_at` que exige C06 |

> **El bloque es inmutable *una vez cerrado*, no desde que se abre.** [ADR-0041](../02-decisiones/0041-temporizador-del-turno-por-poller.md) enmienda acá a [ADR-0022](../02-decisiones/0022-el-turno-vive-en-message-batches.md) §4: mientras el bloque espera, cada mensaje nuevo reescribe `expires_at`, que es lo que hace que el temporizador se reinicie. El resto de la fila se escribe una sola vez —`turn_number` al abrir; `messages_count`, `timer_ms_used`, `closed_reason` y `closed_at` al cerrar—, y **una conversación tiene a lo sumo un bloque abierto**, garantizado por un índice único parcial y no por una regla de aplicación.
>
> **`error_message` no rompe la inmutabilidad del bloque.** Se llena en la misma escritura en la que el núcleo declara el turno fallido, después de agotar los reintentos, y nunca se sobreescribe. Sin él, un turno caído deja únicamente `closed_at IS NOT NULL AND processed_at IS NULL`: alcanza para **contar** turnos fallidos, no para **diagnosticarlos**. En la Fase 4, con despliegue en producción real, distinguir *"el cerebro no respondió"* de *"el proveedor de LLM devolvió 429"* son dos acciones correctivas distintas.
>
> **Es texto libre y no una enumeración, a propósito.** Sigue el precedente de `webhook_events.error_message`, que existe por el mismo motivo (F20). La contrapartida hay que decirla: **sirve para diagnosticar un turno, no para agregar por causa.** Si la Fase 4 pidiera contar fallas por tipo, eso pide una enumeración y es decisión del Paso 11, no de ahora.
>
> **Sin contador de reintentos.** El número de intentos es configuración —variable de entorno, 3 por defecto—, no un hecho del turno: guardarlo en cada fila repetiría en millones de bloques lo que ya dice el archivo de configuración. Lo que no se puede reconstruir después es el motivo, y eso es lo que se guarda.

### `detected_intents`

> **Origen:** F11 · H-03

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `message_batch_id` | uuid | FK |
| `intent_label` | varchar | |
| `confidence` | numeric | |
| `position` | smallint | Orden dentro del bloque |
| `created_at` | timestamptz | Inmutable |

### `pipeline_steps`

> **Origen:** **ADR-0016** — decisión de ingeniería del equipo (pipeline de respuesta) · OE6 (el veredicto de `triage` es el denominador) · F12 (`out_of_domain`) · F06 (`retrieval`) · **H-23**

Una fila **por cada paso del pipeline ejecutado en un turno**. El pipeline es: *blocked-list → guardrail → triage → RAG → grounding (con reranking si hace falta) → blocked-list → guardrail → respuesta.*

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `message_batch_id` | uuid | FK. El turno. **No cuelga de `generations`**: los primeros pasos pueden impedir que exista una generación |
| `step` | `PipelineStep` | Qué paso se ejecutó |
| `direction` | `MessageDirection` | Si corrió sobre el mensaje entrante o sobre la respuesta saliente. **Reutiliza la enumeración existente** (C07) y es lo que distingue las dos apariciones de `blocklist` y `guardrail` |
| `position` | smallint | Orden de ejecución dentro del turno |
| `outcome` | varchar | Qué decidió. **Dominio declarado por `step`** — ver abajo |
| `score` | numeric | Nullable — confianza, sólo en los pasos que la producen (`triage`, `grounding`) |
| `version` | varchar | Versión de la lista, la regla o el modelo que decidió — **o de las dos, cuando el paso decide con dos archivos**: la corrida de salida de `blocklist` escribe `1+profanity1` ([ADR-0042](../02-decisiones/0042-pipeline-de-decision-del-turno.md)), y la de entrada del `guardrail` hará lo mismo con `injection.yaml` ([ADR-0043](../02-decisiones/0043-prompt-injection-en-el-guardrail-de-entrada.md)). **Sin esto, ajustar un guardrail invalida las mediciones previas** — mismo argumento que `signal_evaluations.classifier_version` |
| `latency_ms` | integer | Nullable. Latencia del paso |
| `detail_json` | jsonb | Nullable. Parámetros y detalle: qué regla coincidió, y **el `top_k` y el umbral de similitud del paso `retrieval`** (H-23) |
| `created_at` | timestamptz | **Inmutable: sin `updated_at` ni `deleted_at`** (C09) |

> **Dominio de `outcome` por `step`** (regla de aplicación, igual que `conversation_annotations.gold_value`):
>
> | `step` | Valores |
> |---|---|
> | `blocklist` | `pass` · `blocked` |
> | `guardrail` | `pass` · `blocked` · `modified` |
> | `triage` | `in_domain` · `out_of_domain` |
> | `retrieval` | `hit` · `empty` |
> | `reranking` | `applied` · `skipped` |
> | `grounding` | `grounded` · `not_grounded` |
> | `generation` | `ok` · `failed` |
>
> **Por qué existe.** El modelo guardaba lo que el RAG devolvió (`retrieval_logs`) y lo que el bot respondió (`generations`), y **nada de las decisiones del medio**. Es el mismo error que H-18, H-20, H-21 y H-23: se conserva el resultado y no la evidencia sobre la que se decidió.
>
> **Y es lo que destraba OE6 sin circularidad.** Definir *"consulta dentro del dominio"* por el `similarity_score` deja que el sistema decida qué le corresponde saber y después se evalúe sobre eso — **mejora su métrica justo cuando falla en recuperar**. El veredicto de `triage` se emite **antes** de recuperar y es independiente de si la recuperación funcionó, así que sirve de denominador. Sigue siendo el sistema juzgándose, y por eso se valida contra `conversation_annotations` en una muestra.
>
> **`retrieval` y `generation` figuran como pasos aunque su detalle viva en otro lado** (`retrieval_logs`, `generations`). Es deliberado: sin ellos el rastro queda con agujeros y no se puede leer en orden ni medir la latencia de punta a punta. El costo es una fila que sobre todo señala hacia otra tabla.
>
> **La enumeración `PipelineStep` va a cambiar.** El pipeline todavía no está dibujado — eso es el **Paso 8**. Se asume el costo: no hay datos, y ampliar una enumeración es barato comparado con descubrir en producción que no se registró nada.

### `conversation_contexts`

> **Origen:** F04, textual · OE2 · **H-02 — el aporte original de la tesina**

Estado **actual** del contexto dinámico. Una fila por conversación.

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `conversation_id` | uuid | FK |
| `turn_number` | integer | Turno al que corresponde el estado |
| `summary` | text | Nullable |
| `primary_intent` | varchar | Nullable. F04: *"intención"*. **Renombrado de `detected_intent`**: es la intención **dominante** del último bloque —la de mayor `confidence` en `detected_intents`— y es la que viaja en el prompt. El registro completo de F11 vive en la tabla, no acá |
| `objections_json` | jsonb | F04: *"objeciones"* |
| `urgency_level` | smallint | Nullable |
| `budget_signal` | varchar | Nullable |
| `extracted_data_json` | jsonb | F04: *"datos detectados del cliente"* |
| `created_at` | timestamptz | C06 — toda tabla lo lleva. Cuándo se creó el contexto, es decir **cuándo se procesó el primer bloque** de la conversación |
| `updated_at` | timestamptz | Última reescritura |

> **Sin `deleted_at`: no se borra por separado.** Es el estado actual de una conversación, en relación 1:1: se sobrescribe en cada bloque y deja de tener sentido cuando la conversación deja de tenerlo. El histórico vive en `conversation_context_versions`, que es inmutable.

### `conversation_context_versions`

> **Origen:** OE2 — sin histórico no se puede evaluar el mecanismo de reescritura · H-02

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `conversation_id` | uuid | FK |
| `turn_number` | integer | |
| `content` | jsonb | Estado congelado del contexto en ese turno |
| `rewritten_by_model` | varchar | Modelo que produjo la reescritura |
| `latency_ms` | integer | |
| `created_at` | timestamptz | **Inmutable** |

### `generations`

> **Origen:** F05 · H-01, H-02, H-09, H-11 · **H-21** (`ai_agent_id`) · ADR-0006 (`prompt_rendered` redactado) · F02 (revisión)

Una fila por respuesta generada por la IA.

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `conversation_id` | uuid | FK |
| `ai_agent_id` | uuid | FK. **Qué agente produjo esta generación.** No nullable: si hubo generación, hubo agente (H-21, ADR-0015) |
| `message_id` | uuid | Nullable — nulo si fue rechazada y nunca se envió |
| `message_batch_id` | uuid | Nullable — bloque que la disparó |
| `conversation_context_version_id` | uuid | Versión exacta del contexto usada |
| `llm_provider` | varchar | |
| `llm_model` | varchar | |
| `temperature` | numeric | Nullable |
| `prompt_rendered` | text | **Versión redactada** — nunca el original (ADR-0006) |
| `completion_text` | text | Lo que propuso la IA |
| `prompt_tokens` | integer | |
| `completion_tokens` | integer | |
| `latency_ms` | integer | |
| `cost` | numeric(14,6) | Costo del turno ⚙ — insumo de viabilidad económica. Escala 6 y no 2 (C08): el costo por turno es una fracción de centavo |
| `currency` | char(3) | C08 — **obligatoria junto a todo importe**. Por defecto `USD`: los proveedores de LLM facturan en dólares, mientras que `sales.currency` es `ARS`. Sumar ambos sin distinguir moneda invalidaría el análisis de viabilidad |
| `reasoning_json` | jsonb | F05 · **Sin uso** desde [ADR-0049](../02-decisiones/0049-visualizacion-de-los-componentes-de-cada-respuesta.md): la evidencia de F05 es `generation_components`, y la columna queda siempre en `NULL` |
| `status` | `GenerationStatus` | F02 — modo sugerencia |
| `reviewed_by_operator_id` | uuid | Nullable. F02 |
| `reviewed_at` | timestamptz | Nullable |
| `assembled_at` | timestamptz | **Cuándo se leyeron los componentes que se ensamblaron**, con el reloj de la base. OE1 compara contra este instante y no contra `created_at`, que se escribe segundos después, cuando el modelo ya respondió ([ADR-0039](../02-decisiones/0039-componentes-del-prompt-y-su-versionado.md), regla 10) |
| `created_at` | timestamptz | |

> **Por qué `ai_agent_id` acá y no por join con `conversations`.** Parece duplicar un dato que ya se alcanza por `generations → conversations.ai_agent_id`, pero **no son el mismo dato**: el de `conversations` es el agente **actual** de la conversación y es mutable —F27 permite cambiarlo o apagarlo, y [relaciones.md §9](relaciones.md) lo dice textual—, mientras que éste es el agente que **efectivamente produjo esta generación**, congelado. Es la misma distinción que hay entre `transfer_policies.threshold` y el `threshold_used` de `signal_evaluations`: configuración mutable contra instantánea del momento del hecho.
>
> Sin él, OE1 no tiene contra qué comparar: derivar el agente desde `generation_components` sería deducir *qué agente* a partir de *qué se ensambló*, que es exactamente lo que se está auditando. La entidad ya congelaba `llm_provider`, `llm_model` y `temperature` —que salen del agente— y no congelaba **de quién eran**: eso era un olvido, no una decisión (H-21).
>
> **Sobre la inmutabilidad:** el contenido generado es inmutable. Lo único que muta es el resultado de la revisión (`status`, `reviewed_*`), que transiciona **una sola vez** desde `pending_review` a un estado terminal. Por eso la entidad no lleva `updated_at` genérico.
>
> **Quién escribe `status` y cuándo** (Paso 8). La fila la escribe el cerebro **una sola vez, después del guardrail de salida**, con el valor que describe su propio veredicto: `blocked` si lo frenó, `pending_review` en modo sugerencia, `auto_sent` en modo autónomo. En modo autónomo el envío todavía no ocurrió cuando se escribe `auto_sent`, y eso **no es una mentira**: `GenerationStatus` describe el resultado del **filtrado** —¿pasó sin revisión humana, la revisó alguien, la frenó el sistema?—, no el de la **entrega**. La entrega vive en `messages.status` «MessageStatus», que ya distingue `sent · delivered · read · failed`. Si el envío falla, la generación sigue siendo `auto_sent` y su `message_id` apunta a un mensaje `failed`: las dos afirmaciones son ciertas y cada una está donde corresponde. Separarlas es lo que evita tener que agregar un sexto estado sólo para nombrar el intervalo entre generar y enviar.
>
> **Modo sugerencia sin entidad adicional:** el borrador de F02 es la propia `generation` en estado `pending_review`. Si el operador edita antes de enviar, `messages.content_text` guarda lo enviado y `completion_text` lo propuesto: **comparar ambos es la tasa de edición**, un proxy directo de la calidad del agente.

### `generation_components`

> **Origen:** F05 — *"el operador ve qué componentes del prompt influyeron en cada respuesta"*

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `generation_id` | uuid | FK |
| `prompt_component_version_id` | uuid | **Versión exacta** del componente ensamblado |
| `position` | smallint | Orden en que se ensambló |
| `created_at` | timestamptz | **Inmutable.** C06 exige `created_at` en toda tabla; era la única del modelo que no lo tenía |

---

## 5. Bloque 4 — Conocimiento (RAG)

### `knowledge_bases`

> **Origen:** Anexo C · F06 · ADR-0005, ADR-0010

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `ai_agent_id` | uuid | FK. Única dirección de la relación (ADR-0005) |
| `collection_name` | varchar | Colección de Qdrant asociada (ADR-0010) |
| `size_bytes_total` | bigint | Control del límite de 100 MB **por agente** (F06) |
| `created_at` / `updated_at` / `deleted_at` | timestamptz | Borrado lógico por C09 |

### `documents`

> **Origen:** F06 (*"documentos"*, plural) · ADR-0005 · H-06

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `knowledge_base_id` | uuid | FK |
| `filename` | varchar | |
| `mime_type` | varchar | |
| `size_bytes` | bigint | |
| `checksum` | varchar | Detección de duplicados ⚙ |
| `object_key` | varchar | Clave de los bytes en el almacén de objetos (H-31, ADR-0017 · ADR-0019). Los bytes no van en la fila: un archivo de 100 MB en `bytea` infla backups y WAL |
| `status` | `DocumentStatus` | Pipeline de ingesta |
| `locked_at` | timestamptz | **Nullable.** Marca de toma del documento por el `worker` de ingesta (ADR-0017 (g)). Es la única columna, junto a `status` y `error_message`, que escribe el cerebro sobre esta tabla |
| `error_message` | text | Nullable |
| `uploaded_by_operator_id` | uuid | |
| `created_at` / `updated_at` / `deleted_at` | timestamptz | |

### `document_chunks`

> **Origen:** ADR-0005 · OE6

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `document_id` | uuid | FK |
| `chunk_index` | integer | |
| `content` | text | |
| `token_count` | integer | |
| `vector_id` | varchar | **Nullable.** Identificador del punto en Qdrant (ADR-0010). Se pone en nulo cuando el punto se elimina de la colección: registra que el vector ya no existe, aunque la fila sí |
| `created_at` | timestamptz | |
| `deleted_at` | timestamptz | **Nullable.** Fuera del índice activo — por borrado del documento o por reindexado. Ver la nota de abajo |

> **Por qué el chunk se marca pero no se borra.** Al dar de baja un documento hay una tensión de tres lados: los fragmentos **tienen que dejar de recuperarse** (si no, el agente sigue citando contenido dado de baja), pero **no se pueden borrar** porque `retrieval_logs` los referencia y es la evidencia de OE6 — el registro de qué se recuperó en cada generación ya medida. La salida es que **la fila sobrevive en PostgreSQL y el vector desaparece de Qdrant**.
>
> No alcanza con derivarlo de `documents.deleted_at`: el **reindexado** —cambiar el modelo de embeddings, que ADR-0010 anticipa como pendiente— deja fragmentos obsoletos **sin que su documento esté borrado**. Por eso la marca vive en el chunk.
>
> **Orden de escritura al dar de baja** (se detalla en el Paso 7): primero se eliminan los puntos de Qdrant, después se marcan `vector_id = NULL` y `deleted_at`. Si falla el primer paso, el chunk queda vivo y recuperable — que es preferible a que quede marcado como borrado pero todavía presente en el índice.

### `retrieval_logs`

> **Origen:** OE6 — distinguir *"falló el RAG"* de *"falló el LLM"*

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `generation_id` | uuid | FK |
| `document_chunk_id` | uuid | FK |
| `similarity_score` | numeric | |
| `rank` | smallint | Posición en el resultado |
| `created_at` | timestamptz | Inmutable |

---

## 6. Bloque 5 — Operación

### `accounts`

> **Origen:** ADR-0012 — **previsión estructural**, no funcionalidad. La sección 7.3 declara la estructura multi-empresa como *trabajo futuro*

Contenedor organizativo del que cuelgan las seis entidades raíz. **Opera con una única fila durante todo el proyecto** (ver [relaciones.md §9](relaciones.md)): la gestión multi-empresa —alta de cuentas, aislamiento, permisos por cuenta— sigue fuera de alcance y no se implementa.

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `name` | varchar | Razón social o nombre comercial de la empresa |
| `created_at` / `updated_at` / `deleted_at` | timestamptz | |

> **Por qué existe una tabla que durante todo el proyecto tiene una sola fila.** No por los varios números de teléfono —eso ya lo resuelven varias filas de `inboxes`, sin contenedor—, sino por el costo asimétrico: agregar `account_id` después obliga a migrar las entidades raíz **y a decidir retroactivamente a qué cuenta perteneció cada fila histórica**, sobre datos productivos reales de la Fase 4. Ver ADR-0012.

### `operators`

> **Origen:** **H-00** — el Anexo C nunca la declara, pese a que cuatro entidades la referencian · F18 · Anexo B · C01

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `name` | varchar | |
| `lastname` | varchar | |
| `account_id` | uuid | FK. Entidad raíz (ADR-0012) |
| `email` | varchar | Único parcial (C09) |
| `password_hash` | varchar | ⚙ Implícito en F18: no hay usuarios sin autenticación |
| `role` | `OperatorRole` | Anexo B |
| `active` | boolean | |
| `created_at` / `updated_at` / `deleted_at` | timestamptz | |

### `conversation_assignments`

> **Origen:** F26 (*"reasignación con historial completo"*) · ADR-0004 · H-15 · ADR-0037

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `conversation_id` | uuid | FK |
| `from_operator_id` | uuid | Nullable — nulo en la primera asignación de una conversación, sea por el reparto o desde el pool |
| `to_operator_id` | uuid | FK |
| `assigned_by_operator_id` | uuid | Nullable. Igual a `to_operator_id` cuando un operador toma el pool respondiendo (ADR-0035, regla 7); **nulo cuando asigna el reparto**, porque no la asignó nadie (ADR-0037, regla 5) |
| `reason` | text | Nullable. **Donde entra "lo pidió el cliente"** (ADR-0004). Los motivos del sistema son tokens fijos: `auto_assigned_on_first_reply` (toma del pool), `auto_assigned_to_previous_operator` y `auto_assigned_by_turn` (reparto de ADR-0037) |
| `created_at` | timestamptz | Inmutable |

### `internal_notes`

> **Origen:** F26 — *"notas internas preservadas"* · H-15

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `conversation_id` | uuid | FK |
| `operator_id` | uuid | FK |
| `content` | text | |
| `created_at` / `updated_at` / `deleted_at` | timestamptz | |

### `labels`

> **Origen:** Anexo C · F19 · ADR-0003

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `account_id` | uuid | FK. Entidad raíz (ADR-0012) |
| `name` | varchar | Único parcial |
| `color` | varchar | |
| `created_by_operator_id` | uuid | Renombrado de `created_by_admin_id` (C01) |
| `created_at` / `updated_at` / `deleted_at` | timestamptz | |

### `contact_labels`

> **Origen:** F33 · ADR-0003 — reemplaza a `contacts.tags_json`

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `contact_id` | uuid | FK |
| `label_id` | uuid | FK |
| `assigned_by_operator_id` | uuid | Nullable |
| `created_at` | timestamptz | |

> **Sin `deleted_at`: admite borrado físico, por excepción declarada a C09.** Desetiquetar borra la fila. Es una tabla de unión operativa, no un registro de hechos, y ninguna métrica del anteproyecto cuenta etiquetados históricos. Costo: se pierde quién había asignado la etiqueta y cuándo.

### `conversation_labels`

> **Origen:** F24 — **hoy no representable** (H-07) · F23 · ADR-0003

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `conversation_id` | uuid | FK |
| `label_id` | uuid | FK |
| `assigned_by_operator_id` | uuid | Nullable |
| `created_at` | timestamptz | |

> **Sin `deleted_at`: admite borrado físico**, misma excepción declarada a C09 que `contact_labels`. Quitar una etiqueta de una conversación borra la fila.

### `reminders`

> **Origen:** Anexo C · F28, F32 · ADR-0002

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `contact_id` | uuid | **Obligatorio** (ADR-0002) — F28 los vincula a contactos |
| `conversation_id` | uuid | **Nullable** (ADR-0002) — sobrevive al cierre de la conversación |
| `operator_id` | uuid | Operador notificado |
| `description` | text | |
| `due_at` | timestamptz | |
| `notified` | boolean | Anexo C |
| `notified_at` | timestamptz | Nullable |
| `created_at` / `updated_at` / `deleted_at` | timestamptz | |

### `sales`

> **Origen:** Anexo C · F14, F35 · ADR-0001 (sin `source_channel`) · C08 (dinero)

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `conversation_id` | uuid | FK |
| `operator_id` | uuid | FK |
| `ai_agent_id` | uuid | Nullable — el agente que la gestionó (F14) |
| `amount` | numeric(14,2) | **Nunca punto flotante** (C08) |
| `currency` | char(3) | ISO 4217, por defecto `ARS` (C08) |
| `created_at` / `updated_at` / `deleted_at` | timestamptz | |

> **`source_channel` eliminado** (ADR-0001): el origen vive en `contacts` y se alcanza por join.

### `quick_replies`

> **Origen:** Anexo C · F07, F09

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `account_id` | uuid | FK. Entidad raíz (ADR-0012) |
| `title` | varchar | |
| `content` | text | |
| `created_by_operator_id` | uuid | Renombrado de `created_by_admin_id` (C01) |
| `active` | boolean | |
| `created_at` / `updated_at` / `deleted_at` | timestamptz | |

---

## 7. Bloque 6 — Canal e integración

### `inboxes`

> **Origen:** F20 · §8.5 (validación HMAC-SHA256) · H-08 · ADR-0012 (renombre y ruteo)

Una bandeja por número de WhatsApp. Meta permite varios números bajo una misma WABA —típicamente uno de ventas y otro de soporte—, y eso **sigue siendo canal único** en los términos de la sección 7.3.

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `account_id` | uuid | FK. Entidad raíz (ADR-0012) |
| `default_ai_agent_id` | uuid | **Nullable.** El agente que atiende las conversaciones nuevas de esta bandeja (ADR-0012). Nulo si la bandeja opera sin IA |
| `phone_number_id` | varchar | Único **parcial** (`WHERE deleted_at IS NULL`): una bandeja por número. Es lo que Meta manda en cada webhook para nombrarlo |
| `waba_id` | varchar | Agrupa las bandejas del mismo número comercial en Meta |
| `display_name` | varchar | |
| `access_token_encrypted` | bytea | **Nullable.** `NULL` es un número que nadie conectó todavía. Cifrado en la aplicación (ADR-0031): SQL no puede escribirlo |
| `verify_token` | varchar | |
| `app_secret_encrypted` | bytea | **Nullable**, con el mismo sentido. Validación de firma (§8.5): sin él, todo webhook de la bandeja se guarda como `discarded` |
| `status` | `InboxStatus` | C07 — era `varchar` sin declarar |
| `created_at` / `updated_at` / `deleted_at` | timestamptz | Borrado lógico por C09. **Distinto de `status = inactive`**: una bandeja inactiva sigue existiendo en la configuración; una dada de baja, no |

> **Renombrada de `channel_configs`** (ADR-0012). Conserva los mismos campos, todos específicos de WhatsApp: **no se agregó `channel_type` ni ninguna generalización**, porque los canales adicionales están fuera de alcance (7.3). El renombre es sólo vocabulario — el que no habría que volver a cambiar si algún día entra otro canal, y el que usa Chatwoot, referencia del dominio declarada en C01.

### `webhook_events`

> **Origen:** F20 — *"reintentos y log de eventos"*, textual · H-08

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `inbox_id` | uuid | FK. Renombrada de `channel_config_id` (ADR-0012) |
| `event_type` | `WebhookEventType` | C07 — era `varchar` sin declarar |
| `wamid` | varchar | Nullable — correlación con `messages` |
| `payload_json` | jsonb | |
| `signature_valid` | boolean | |
| `status` | `WebhookEventStatus` | C07 — era `varchar` sin declarar |
| `retries` | smallint | |
| `error_message` | text | Nullable |
| `received_at` / `processed_at` | timestamptz | **Inmutable: sin `updated_at` ni `deleted_at`** (C09) — es el log de eventos que pide F20. `received_at` cumple la función de `created_at` que exige C06. `status` y `retries` mutan durante el reprocesamiento, no después |

### `meta_templates`

> **Origen:** F08, F37 — **dos requisitos completos sin ninguna entidad** (H-05)

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `inbox_id` | uuid | FK. Renombrada de `channel_config_id` (ADR-0012) |
| `meta_template_id` | varchar | Identificador en la plataforma |
| `name` | varchar | |
| `language` | varchar | |
| `category` | `MetaTemplateCategory` | C07 — el conjunto lo fija Meta, no el proyecto |
| `status` | `MetaTemplateStatus` | F08: *"muestra plantillas existentes con estado"* |
| `body` | text | |
| `rejection_reason` | text | Nullable |
| `llm_suggestion` | text | Nullable. F08/F37: sugerencia de reformulación |
| `synced_at` | timestamptz | Última sincronización contra Meta |
| `created_at` / `updated_at` / `deleted_at` | timestamptz | Borrado lógico por C09 — **una plantilla puede desaparecer del lado de Meta**, y la fila local tiene que poder reflejarlo sin perder el historial |

---

## 8. Bloque 7 — Evaluación

### `signal_evaluations`

> **Origen:** F12 (las cuatro señales) · **OE4** — *"detectar señales de frustración y cambios de intención con precisión ≥ 85 %"* · F36 (*"tasa de transferencia **por tipo de señal**"*) · **H-18**

Una fila **por cada señal evaluada en cada bloque procesado**, haya disparado la transferencia o no.

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `message_batch_id` | uuid | FK. El turno en que se evaluó la señal |
| `signal_type` | `TransferSignalType` | Las cuatro de F12. **No requiere enumeración nueva** (C07) |
| `score` | numeric | Salida del clasificador, **siempre**. Es el atributo que justifica la entidad: hasta ahora el puntaje sólo existía si superaba el umbral |
| `threshold_used` | numeric | **El umbral que rigió esta evaluación.** `transfer_policies` es mutable y guarda sólo el valor actual: sin congelarlo acá, mover el umbral a mitad de las 200 conversaciones de OE3 vuelve incomparables las mediciones anteriores — el mismo argumento que justificó versionar los componentes del prompt para OE2 |
| `classifier_version` | varchar | **Sin esto, ajustar el clasificador invalida las mediciones previas** |
| `fired` | boolean | Si esta señal disparó la transferencia. **No es derivable de `score >= threshold_used`**: una señal puede superar el umbral y no disparar, porque `transfer_policies.enabled = false` o porque otra señal transfirió primero |
| `created_at` | timestamptz | **Inmutable: sin `updated_at` ni `deleted_at`** (C09) |

> **Por qué existe esta entidad.** OE4 mide la precisión del detector de frustración, y un detector sólo es medible si su salida se persiste **con independencia de si disparó una acción**. `transfer_events` registra la acción y por lo tanto conserva únicamente las señales que ganaron: es una muestra filtrada justamente por el umbral que se quiere evaluar. Medir precisión sobre ella da un número que **sube al subir el umbral**, sin que el clasificador haya mejorado.
>
> Es el mismo razonamiento que ya justificó `message_batches.timer_ms_used` —*sin persistir el bloque, F10 es una afirmación sin evidencia*— aplicado al otro clasificador del Módulo 2. Formulado como principio: **todo clasificador cuya precisión es un objetivo específico tiene que persistir su salida aunque esa salida no dispare nada.** El de transferencia lo cumple por accidente, porque su salida *es* una acción; el de frustración no lo cumplía.
>
> **Sin `conversation_id`.** Se llega por `message_batch_id → message_batches.conversation_id`. Es la misma regla que ADR-0012 aplicó a `account_id`: la pertenencia se hereda por la cadena de FK, y duplicarla crea dos caminos hacia el mismo dato. Si las consultas de OE3/OE4 justificaran desnormalizarlo, se decide en el Paso 7 con la medición en la mano.
>
> **`score` y `threshold_used` no son nullable**, ni siquiera para señales deterministas como `explicit_request`: `transfer_policies` ya asigna un `threshold` a las cuatro, así que el modelo ya asume que todas puntúan. Una señal booleana escribe 0 o 1.
>
> **Reclasificación fuera de línea.** Volver a correr una versión nueva del clasificador sobre bloques históricos escribe filas con `fired = false` — nada dispara fuera de línea. Para comparar versiones se usa `score >= threshold_used`, no `fired`.

### `transfer_events`

> **Origen:** F12 · F36 (*"tasa de transferencia **por tipo de señal**"*) · OE3 · H-04 · H-18 (traslado de atributos)

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `conversation_id` | uuid | FK |
| `signal_evaluation_id` | uuid | FK. **La evidencia que causó la transferencia** |
| `turn_number` | integer | |
| `to_operator_id` | uuid | Nullable |
| `created_at` | timestamptz | Inmutable |

> **Cuatro atributos se mudaron a `signal_evaluations`** (H-18): `trigger_type`, `confidence`, `threshold_used` y `classifier_version`. No se copian acá: el evento **apunta a la evidencia, no la duplica** — la misma separación que ya existe entre `generations` (la acción) y `generation_components` (con qué se armó). F36 alcanza el tipo de señal por join.
>
> **Falta declarar la regla de atribución.** Si dos señales superan el umbral en el mismo turno hay una sola transferencia, y `signal_evaluation_id` apunta a una sola fila. F12 no dice cuál gana: hay que fijar el criterio —mayor `score / threshold_used`, o un orden de prioridad entre las cuatro señales— y dejarlo escrito.

### `conversation_annotations`

> **Origen:** OE3 (200 conversaciones) · OE4 (500 conversaciones **etiquetadas**) · H-10

**Ground truth** de evaluación. No confundir con `labels`, que es el catálogo operativo del negocio.

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `conversation_id` | uuid | FK |
| `annotation_type` | `AnnotationType` | Qué se está etiquetando |
| `gold_value` | varchar | La etiqueta correcta. **Dominio declarado por `annotation_type`** — ver abajo (ADR-0014) |
| `annotated_by_operator_id` | uuid | Quién etiquetó — permite medir acuerdo entre etiquetadores |
| `notes` | text | Nullable |
| `created_at` | timestamptz | Inmutable |

> **Dominio de `gold_value`.** Es `varchar` y no una enumeración porque su conjunto de valores **depende de `annotation_type`**, y C07 no admite una enumeración cuyo dominio cambie según otra columna. Se declara acá y se valida por regla de aplicación:
>
> | `annotation_type` | Valores admitidos | Contra qué se compara |
> |---|---|---|
> | `transfer` | `no_resolution` · `frustration` · `out_of_domain` · `explicit_request` · `none` | `signal_evaluations.signal_type` de la evaluación que disparó (`none` = no había que transferir) |
> | `frustration` | `frustration` · `none` | `signal_evaluations` con `signal_type = 'frustration'` |
> | `intent` | *pendiente* | Depende de la definición de "cambio de intención", abierta en ADR-0014 §6 |
>
> Reutilizar los valores de `TransferSignalType` en el caso `transfer` no es cosmético: permite medir **dos precisiones con el mismo dato** — la gruesa (`gold_value <> 'none'`: ¿había que transferir?) y la fina (`= signal_type`: ¿además acertó *por qué*?), que es la que le da sustancia a la *"tasa de transferencia por tipo de señal"* de F36.

### `tone_evaluations`

> **Origen:** F36 · OE2 (≥80 %) · OE7 (≥85 %) · H-11

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `conversation_id` | uuid | FK |
| `generation_id` | uuid | Nullable — evaluación por respuesta |
| `tone_score` | numeric | **Renombrado de `score`** — ver `conversations.quality_score` |
| `rubric_version` | varchar | **Una métrica de juez sin rúbrica versionada no es una métrica** |
| `judge_model` | varchar | |
| `rationale` | text | Nullable |
| `created_at` | timestamptz | Inmutable |

---

## 9. Bloque 8 — Cumplimiento y auditoría

### `contact_attributes`

> **Origen:** F31 — *"el LLM extrae datos del usuario"*, abierto · H-17

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `contact_id` | uuid | FK |
| `key` | varchar | Nombre del dato detectado |
| `value_encrypted` | bytea | Cifrado por defecto: puede contener datos personales |
| `source` | `AttributeSource` | `manual` o `llm` |
| `confidence` | numeric | Nullable — sólo si `source = llm` |
| `source_message_id` | uuid | Nullable. **Qué mensaje originó el dato** — trazabilidad ante la Ley 25.326 |
| `created_at` / `updated_at` / `deleted_at` | timestamptz | |

### `audit_logs`

> **Origen:** ADR-0007 — alcance acotado a **cambios de configuración de agentes IA**, que incluyen qué agente IA atiende cada número (ADR-0038, regla 13, `entity_type = inboxes`) y excluyen los componentes del prompt, auditados en `prompt_component_versions` (ADR-0039, regla 5)

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `operator_id` | uuid | Quién |
| `action` | varchar | Qué hizo |
| `entity_type` | varchar | Sobre qué entidad |
| `entity_id` | uuid | |
| `changes_json` | jsonb | Antes y después |
| `ip_address` | inet | Nullable |
| `created_at` | timestamptz | **Inmutable, sin borrado** (C09) |

> Las reasignaciones de conversación —la segunda clase de evento de ADR-0007— **no se duplican acá**: ya viven en `conversation_assignments`.

---

## 10. Entidades condicionadas a decisiones pendientes

No forman parte del modelo hasta que se cierren los pendientes registrados en [02-decisiones/README.md](../02-decisiones/README.md).

| Entidad | Condición | Atributos previstos |
|---|---|---|
| `sensitive_data_access_logs` | Depende del pendiente *"registro de desencriptado"* (ADR-0007, ADR-0032 · H-39) | `id`, `operator_id`, `contact_id`, `field`, `purpose`, `created_at` |
| `reporting_events` | Diferida: agregación pre-calculada para F34–F38. **Se incorpora cuando el volumen lo pida**, no antes | `id`, `event_name`, `value`, `conversation_id`, `operator_id`, `ai_agent_id`, `created_at` |

---

## 11. Resumen

**37 entidades** en el modelo, más 2 condicionadas.

| Bloque | Entidades | Cantidad |
|---|---|---|
| 1 — Núcleo | `contacts`, `conversations`, `messages` | 3 |
| 2 — Identidad del agente IA | `ai_agents`, `prompt_components`, `prompt_component_versions`, `transfer_policies` | 4 |
| 3 — Ejecución por turno | `message_batches`, `detected_intents`, `pipeline_steps`, `conversation_contexts`, `conversation_context_versions`, `generations`, `generation_components` | 7 |
| 4 — Conocimiento | `knowledge_bases`, `documents`, `document_chunks`, `retrieval_logs` | 4 |
| 5 — Operación | `accounts`, `operators`, `conversation_assignments`, `internal_notes`, `labels`, `contact_labels`, `conversation_labels`, `reminders`, `sales`, `quick_replies` | 10 |
| 6 — Canal e integración | `inboxes`, `webhook_events`, `meta_templates` | 3 |
| 7 — Evaluación | `signal_evaluations`, `transfer_events`, `conversation_annotations`, `tone_evaluations` | 4 |
| 8 — Cumplimiento | `contact_attributes`, `audit_logs` | 2 |

De las 37, **9 provienen del Anexo C** (con renombres y correcciones) y **28 son nuevas**.

---

## 12. Por qué estas entidades

### El criterio

Cada entidad responde a una fila de la [matriz de trazabilidad](../01-trazabilidad.md) que quedó en ❌ o ⚠️, o conserva una entidad del Anexo C que quedó en ✅. **No se agregó ninguna entidad "por si acaso"**: la matriz recorrió los 39 requisitos y los 7 objetivos, y 25 de las 28 entidades nuevas son la respuesta a los 18 hallazgos que produjo ese recorrido. La vigesimosexta, `signal_evaluations`, nace de un hallazgo posterior —**H-18**, de la validación del Paso 6— y también traza a requisitos: OE4, F12 y F36.

**Las dos excepciones conviene declararlas.**

**`accounts`** no nace de ningún requisito ni de ningún hallazgo: nace de la auditoría del catálogo y de una decisión de previsión estructural (ADR-0012). Por eso lleva una regla escrita que la acota a una sola fila en operación. Fingir que deriva de un requisito sería más cómodo y menos honesto.

**`pipeline_steps`** es un caso mixto (ADR-0016). El pipeline de respuesta —blocked-list, guardrail, triage, grounding— **es una decisión de ingeniería del equipo y no está en el anteproyecto**: sólo `retrieval` (F06) y `generation` tienen anclaje textual. Pero la entidad sí responde a un objetivo: el veredicto de `triage` es lo que permite definir *"consulta dentro del dominio"* (OE6) sin caer en la circularidad de medirlo por el `similarity_score`. Nace de una decisión propia y resuelve un problema del anteproyecto — las dos cosas, y así queda dicho.

### Lo que revela el reparto

El Anexo C tenía **nueve** entidades y el modelo necesita **treinta y cinco**. Pero lo interesante no es el número, sino **dónde** se concentran las que faltaban:

- Las nueve del Anexo C cubren **la operación**: contactos, conversaciones, mensajes, etiquetas, ventas, recordatorios, respuestas rápidas. Es el territorio que Chatwoot ya resolvió.
- Las veintiocho nuevas se concentran en **la composición de prompts, la ejecución por turno, la clasificación de transferencia y la evaluación**. Es decir: casi exactamente la lista del aporte original que declara la sección 6.3 del anteproyecto.

**Estaba modelado lo que SONIA comparte con el estado del arte, y no estaba modelado lo que la diferencia de él.** Los bloques 2, 3 y 7 —quince entidades— existen para sostener el aporte original y su medición.

### Las cinco decisiones estructurales

1. **Los componentes del prompt son filas, no un JSON** (`prompt_components` + `prompt_component_versions`). F03 necesita previsualizar componente por componente, F05 señalar cuál influyó, y OE1 saber cuáles están activos. Con `components_json` ninguna de las tres es posible. El versionado es lo que permite responder la pregunta que **es** OE2: *contra qué configuración se midió el 80 %*.

2. **El contexto dinámico pertenece a la conversación, no al agente** (`conversation_contexts` + `conversation_context_versions`). F04 es un séptimo componente que cambia en cada turno y es distinto para cada conversación aunque el agente sea el mismo. Sin el histórico no se puede evaluar la reescritura, que es el mecanismo central de la tesina.

3. **El registro por turno es una entidad aparte de `messages`** (`generations`). `messages` va a ser la tabla más grande del sistema y F22 lista **todos** los chats: arrastrar el prompt renderizado y el razonamiento en cada render de bandeja es un problema de rendimiento garantizado. Además sólo los salientes generados por IA tienen esos datos. Como beneficio secundario, `generations` da **costo y latencia por turno**, insumo directo de la viabilidad económica ante el cooperante.

4. **La capa de evaluación existe desde el diseño, no después** (`conversation_annotations`, `tone_evaluations`, `transfer_events`, `signal_evaluations`, `retrieval_logs`). Cinco de los siete objetivos específicos son métricas, y ninguna era calculable con el Anexo C. `conversation_annotations` en particular es el ground truth de OE3 y OE4: si esas 200 y 500 conversaciones se etiquetan en una planilla suelta, la precisión ≥85 % no es reproducible ante el tribunal.

5. **`messages.sender_type` es el atributo más crítico de todo el modelo.** No es una entidad, pero merece mención: `direction` sólo dice entrante o saliente. Sin distinguir si respondió la IA o el humano, **OE7 —la tasa de resolución autónoma, el objetivo de producción del proyecto— no existe**.

### Un hueco que la matriz no había detectado

Los cinco campos de medios en `messages` (`media_id`, `media_url`, `media_mime_type`, `media_size_bytes`, `media_duration_ms`). El Anexo C admite `content_type` de tipo `audio` e `image` y §8.5 describe la transcripción de audios, pero no hay ningún campo donde referenciar el contenido. **Apareció al escribir los atributos, no al recorrer los requisitos** — vale como recordatorio de que la matriz encuentra entidades faltantes, no atributos faltantes.
