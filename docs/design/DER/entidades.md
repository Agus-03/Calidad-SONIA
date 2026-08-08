# SONIA — Catálogo de entidades y atributos

| Campo | Valor |
|---|---|
| **Documento** | `docs/design/DER/entidades.md` |
| **Estado** | Borrador para el Paso 5 (modelo de datos lógico) |
| **Fecha** | 2026-08-06 |
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
| `GenerationStatus` | `sent` · `pending_review` · `accepted` · `edited` · `rejected` | F02 (modo sugerencia) |
| `DocumentStatus` | `pending` · `processing` · `indexed` · `failed` | ADR-0005 |
| `MetaTemplateStatus` | `approved` · `pending` · `rejected` | F08, F37 |
| `AttributeSource` | `manual` · `llm` | F31 |
| `AnnotationType` | `transfer` · `frustration` · `intent` | OE3, OE4 |

---

## 2. Bloque 1 — Núcleo

### `contacts`

> **Origen:** Anexo C · F30, F31, F32 · ADR-0001 (origen) · ADR-0006 (cifrado)

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
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
| `consent_given_at` | timestamptz | Anexo C; sección 7.3 |
| `consent_revoked_at` | timestamptz | Nullable. Derecho de supresión de la Ley 25.326 (C09) |
| `created_at` / `updated_at` / `deleted_at` | timestamptz | |

### `conversations`

> **Origen:** Anexo C · F13, F22, F23, F25, F27, F34 · H-12 · ADR-0002, ADR-0004, ADR-0008

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `contact_id` | uuid | FK |
| `ai_agent_id` | uuid | **Nullable** — en modo `off` no hay agente |
| `operator_id` | uuid | **Nullable** — pool sin asignar, o modo autónomo (ADR-0004) |
| `status` | `ConversationStatus` | |
| `mode` | `AgentMode` | Modo efectivo de esta conversación (F27) |
| `starred` | boolean | F25 |
| `score` | smallint | Nullable. F13 |
| `scored_by_operator_id` | uuid | Nullable. H-13 |
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
| `active` | boolean | OE1 exige el concepto de componente **activo**; habilita ablación |
| `created_at` / `updated_at` | timestamptz | |

### `prompt_component_versions`

> **Origen:** OE2 — *"¿contra qué configuración se midió?"* · H-01

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `prompt_component_id` | uuid | FK |
| `version_number` | integer | Correlativo por componente |
| `content` | text | Contenido congelado |
| `changed_by_operator_id` | uuid | Nullable |
| `change_note` | text | Nullable |
| `created_at` | timestamptz | **Inmutable: sin `updated_at` ni `deleted_at`** (C09) |

### `transfer_policies`

> **Origen:** F01 (*"incluye la configuración de cuándo delegar a un humano"*) · Anexo C (`umbral_transferencia`, hoy dentro del JSON) · F12

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `ai_agent_id` | uuid | FK |
| `signal_type` | `TransferSignalType` | Una fila por señal de F12 |
| `enabled` | boolean | |
| `threshold` | numeric | Umbral de disparo |
| `created_at` / `updated_at` | timestamptz | |

---

## 4. Bloque 3 — Ejecución por turno

### `message_batches`

> **Origen:** F10 · OE3 · H-03

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `conversation_id` | uuid | FK |
| `messages_count` | smallint | |
| `timer_ms_used` | integer | **Lo que permite defender que el timer es "adaptativo"** y no una constante |
| `closed_reason` | `BatchCloseReason` | |
| `opened_at` / `closed_at` / `processed_at` | timestamptz | |

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

### `conversation_contexts`

> **Origen:** F04, textual · OE2 · **H-02 — el aporte original de la tesina**

Estado **actual** del contexto dinámico. Una fila por conversación.

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `conversation_id` | uuid | FK |
| `turn_number` | integer | Turno al que corresponde el estado |
| `summary` | text | Nullable |
| `detected_intent` | varchar | Nullable. F04: *"intención"* |
| `objections_json` | jsonb | F04: *"objeciones"* |
| `urgency_level` | smallint | Nullable |
| `budget_signal` | varchar | Nullable |
| `extracted_data_json` | jsonb | F04: *"datos detectados del cliente"* |
| `updated_at` | timestamptz | |

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

> **Origen:** F05 · H-01, H-02, H-09, H-11 · ADR-0006 (`prompt_rendered` redactado) · F02 (revisión)

Una fila por respuesta generada por la IA.

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `conversation_id` | uuid | FK |
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
| `cost` | numeric(14,6) | Costo del turno ⚙ — insumo de viabilidad económica |
| `reasoning_json` | jsonb | F05 |
| `status` | `GenerationStatus` | F02 — modo sugerencia |
| `reviewed_by_operator_id` | uuid | Nullable. F02 |
| `reviewed_at` | timestamptz | Nullable |
| `created_at` | timestamptz | |

> **Sobre la inmutabilidad:** el contenido generado es inmutable. Lo único que muta es el resultado de la revisión (`status`, `reviewed_*`), que transiciona **una sola vez** desde `pending_review` a un estado terminal. Por eso la entidad no lleva `updated_at` genérico.
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
| `created_at` / `updated_at` | timestamptz | |

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
| `status` | `DocumentStatus` | Pipeline de ingesta |
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
| `vector_id` | varchar | Identificador del punto en Qdrant (ADR-0010) |
| `created_at` | timestamptz | |

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

### `operators`

> **Origen:** **H-00** — el Anexo C nunca la declara, pese a que cuatro entidades la referencian · F18 · Anexo B · C01

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `name` | varchar | |
| `lastname` | varchar | |
| `email` | varchar | Único parcial (C09) |
| `password_hash` | varchar | ⚙ Implícito en F18: no hay usuarios sin autenticación |
| `role` | `OperatorRole` | Anexo B |
| `active` | boolean | |
| `created_at` / `updated_at` / `deleted_at` | timestamptz | |

### `conversation_assignments`

> **Origen:** F26 (*"reasignación con historial completo"*) · ADR-0004 · H-15

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `conversation_id` | uuid | FK |
| `from_operator_id` | uuid | Nullable — nulo en la primera asignación desde el pool |
| `to_operator_id` | uuid | FK |
| `assigned_by_operator_id` | uuid | Nullable. Igual a `to_operator_id` en la autoasignación |
| `reason` | text | Nullable. **Donde entra "lo pidió el cliente"** (ADR-0004) |
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

### `conversation_labels`

> **Origen:** F24 — **hoy no representable** (H-07) · F23 · ADR-0003

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `conversation_id` | uuid | FK |
| `label_id` | uuid | FK |
| `assigned_by_operator_id` | uuid | Nullable |
| `created_at` | timestamptz | |

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
| `title` | varchar | |
| `content` | text | |
| `created_by_operator_id` | uuid | Renombrado de `created_by_admin_id` (C01) |
| `active` | boolean | |
| `created_at` / `updated_at` / `deleted_at` | timestamptz | |

---

## 7. Bloque 6 — Canal e integración

### `channel_configs`

> **Origen:** F20 · §8.5 (validación HMAC-SHA256) · H-08

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `phone_number_id` | varchar | |
| `waba_id` | varchar | |
| `display_name` | varchar | |
| `access_token_encrypted` | bytea | |
| `verify_token` | varchar | |
| `app_secret_encrypted` | bytea | Validación de firma (§8.5) |
| `status` | varchar | |
| `created_at` / `updated_at` | timestamptz | |

### `webhook_events`

> **Origen:** F20 — *"reintentos y log de eventos"*, textual · H-08

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `channel_config_id` | uuid | FK |
| `event_type` | varchar | |
| `wamid` | varchar | Nullable — correlación con `messages` |
| `payload_json` | jsonb | |
| `signature_valid` | boolean | |
| `status` | varchar | |
| `retries` | smallint | |
| `error_message` | text | Nullable |
| `received_at` / `processed_at` | timestamptz | |

### `meta_templates`

> **Origen:** F08, F37 — **dos requisitos completos sin ninguna entidad** (H-05)

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `channel_config_id` | uuid | FK |
| `meta_template_id` | varchar | Identificador en la plataforma |
| `name` | varchar | |
| `language` | varchar | |
| `category` | varchar | |
| `status` | `MetaTemplateStatus` | F08: *"muestra plantillas existentes con estado"* |
| `body` | text | |
| `rejection_reason` | text | Nullable |
| `llm_suggestion` | text | Nullable. F08/F37: sugerencia de reformulación |
| `synced_at` | timestamptz | |
| `created_at` / `updated_at` | timestamptz | |

---

## 8. Bloque 7 — Evaluación

### `transfer_events`

> **Origen:** F12 · F36 (*"tasa de transferencia **por tipo de señal**"*) · OE3 · H-04

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `conversation_id` | uuid | FK |
| `trigger_type` | `TransferSignalType` | Las cuatro señales de F12 |
| `confidence` | numeric | |
| `classifier_version` | varchar | **Sin esto, ajustar el clasificador invalida las mediciones previas** |
| `turn_number` | integer | |
| `to_operator_id` | uuid | Nullable |
| `created_at` | timestamptz | Inmutable |

### `conversation_annotations`

> **Origen:** OE3 (200 conversaciones) · OE4 (500 conversaciones **etiquetadas**) · H-10

**Ground truth** de evaluación. No confundir con `labels`, que es el catálogo operativo del negocio.

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `conversation_id` | uuid | FK |
| `annotation_type` | `AnnotationType` | Qué se está etiquetando |
| `gold_value` | varchar | La etiqueta correcta |
| `annotated_by_operator_id` | uuid | Quién etiquetó — permite medir acuerdo entre etiquetadores |
| `notes` | text | Nullable |
| `created_at` | timestamptz | Inmutable |

### `tone_evaluations`

> **Origen:** F36 · OE2 (≥80 %) · OE7 (≥85 %) · H-11

| Atributo | Tipo | Notas |
|---|---|---|
| `id` | uuid | PK |
| `conversation_id` | uuid | FK |
| `generation_id` | uuid | Nullable — evaluación por respuesta |
| `score` | numeric | |
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

> **Origen:** ADR-0007 — alcance acotado a **cambios de configuración de agentes IA**

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
| `sensitive_data_access_logs` | Depende del pendiente *"registro de desencriptado"* (ADR-0007, ADR-0011) | `id`, `operator_id`, `contact_id`, `field`, `purpose`, `created_at` |
| `reporting_events` | Diferida: agregación pre-calculada para F34–F38. **Se incorpora cuando el volumen lo pida**, no antes | `id`, `event_name`, `value`, `conversation_id`, `operator_id`, `ai_agent_id`, `created_at` |

---

## 11. Resumen

**34 entidades** en el modelo, más 2 condicionadas.

| Bloque | Entidades | Cantidad |
|---|---|---|
| 1 — Núcleo | `contacts`, `conversations`, `messages` | 3 |
| 2 — Identidad del agente IA | `ai_agents`, `prompt_components`, `prompt_component_versions`, `transfer_policies` | 4 |
| 3 — Ejecución por turno | `message_batches`, `detected_intents`, `conversation_contexts`, `conversation_context_versions`, `generations`, `generation_components` | 6 |
| 4 — Conocimiento | `knowledge_bases`, `documents`, `document_chunks`, `retrieval_logs` | 4 |
| 5 — Operación | `operators`, `conversation_assignments`, `internal_notes`, `labels`, `contact_labels`, `conversation_labels`, `reminders`, `sales`, `quick_replies` | 9 |
| 6 — Canal e integración | `channel_configs`, `webhook_events`, `meta_templates` | 3 |
| 7 — Evaluación | `transfer_events`, `conversation_annotations`, `tone_evaluations` | 3 |
| 8 — Cumplimiento | `contact_attributes`, `audit_logs` | 2 |

De las 34, **9 provienen del Anexo C** (con renombres y correcciones) y **25 son nuevas**.

---

## 12. Por qué estas entidades

### El criterio

Cada entidad responde a una fila de la [matriz de trazabilidad](../01-trazabilidad.md) que quedó en ❌ o ⚠️, o conserva una entidad del Anexo C que quedó en ✅. **No se agregó ninguna entidad "por si acaso"**: la matriz recorrió los 39 requisitos y los 7 objetivos, y las 25 entidades nuevas son la respuesta a los 18 hallazgos que produjo ese recorrido.

### Lo que revela el reparto

El Anexo C tenía **nueve** entidades y el modelo necesita **treinta y cuatro**. Pero lo interesante no es el número, sino **dónde** se concentran las que faltaban:

- Las nueve del Anexo C cubren **la operación**: contactos, conversaciones, mensajes, etiquetas, ventas, recordatorios, respuestas rápidas. Es el territorio que Chatwoot ya resolvió.
- Las veinticinco nuevas se concentran en **la composición de prompts, la ejecución por turno, la clasificación de transferencia y la evaluación**. Es decir: casi exactamente la lista del aporte original que declara la sección 6.3 del anteproyecto.

**Estaba modelado lo que SONIA comparte con el estado del arte, y no estaba modelado lo que la diferencia de él.** Los bloques 2, 3 y 7 —catorce entidades— existen para sostener el aporte original y su medición.

### Las cinco decisiones estructurales

1. **Los componentes del prompt son filas, no un JSON** (`prompt_components` + `prompt_component_versions`). F03 necesita previsualizar componente por componente, F05 señalar cuál influyó, y OE1 saber cuáles están activos. Con `components_json` ninguna de las tres es posible. El versionado es lo que permite responder la pregunta que **es** OE2: *contra qué configuración se midió el 80 %*.

2. **El contexto dinámico pertenece a la conversación, no al agente** (`conversation_contexts` + `conversation_context_versions`). F04 es un séptimo componente que cambia en cada turno y es distinto para cada conversación aunque el agente sea el mismo. Sin el histórico no se puede evaluar la reescritura, que es el mecanismo central de la tesina.

3. **El registro por turno es una entidad aparte de `messages`** (`generations`). `messages` va a ser la tabla más grande del sistema y F22 lista **todos** los chats: arrastrar el prompt renderizado y el razonamiento en cada render de bandeja es un problema de rendimiento garantizado. Además sólo los salientes generados por IA tienen esos datos. Como beneficio secundario, `generations` da **costo y latencia por turno**, insumo directo de la viabilidad económica ante el cooperante.

4. **La capa de evaluación existe desde el diseño, no después** (`conversation_annotations`, `tone_evaluations`, `transfer_events`, `retrieval_logs`). Cinco de los siete objetivos específicos son métricas, y ninguna era calculable con el Anexo C. `conversation_annotations` en particular es el ground truth de OE3 y OE4: si esas 200 y 500 conversaciones se etiquetan en una planilla suelta, la precisión ≥85 % no es reproducible ante el tribunal.

5. **`messages.sender_type` es el atributo más crítico de todo el modelo.** No es una entidad, pero merece mención: `direction` sólo dice entrante o saliente. Sin distinguir si respondió la IA o el humano, **OE7 —la tasa de resolución autónoma, el objetivo de producción del proyecto— no existe**.

### Un hueco que la matriz no había detectado

Los cinco campos de medios en `messages` (`media_id`, `media_url`, `media_mime_type`, `media_size_bytes`, `media_duration_ms`). El Anexo C admite `content_type` de tipo `audio` e `image` y §8.5 describe la transcripción de audios, pero no hay ningún campo donde referenciar el contenido. **Apareció al escribir los atributos, no al recorrer los requisitos** — vale como recordatorio de que la matriz encuentra entidades faltantes, no atributos faltantes.
