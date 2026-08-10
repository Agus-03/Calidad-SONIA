# SONIA — Guía de dibujo del DER

| Campo | Valor |
|---|---|
| **Documento** | `docs/design/DER/guia-de-dibujo.md` |
| **Estado** | Vigente — Paso 5 |
| **Fecha** | 2026-08-09 |
| **Deriva de** | [entidades.md](entidades.md) y [relaciones.md](relaciones.md), que son la **fuente de verdad** ([ADR-0013](../02-decisiones/0013-fuente-de-verdad-del-modelo.md)) |

---

## 0. Para qué sirve y qué NO es

Este documento no agrega nada al modelo: **reordena el catálogo para que dibujar sea transcripción y no consulta cruzada.** Trae los nombres de atributos y las relaciones agrupadas por bloque, de modo que se pueda dibujar un bloque completo sin abrir otro archivo.

**No es fuente de verdad.** Si algo acá contradice a [entidades.md](entidades.md) o [relaciones.md](relaciones.md), gana el catálogo y este documento se corrige. Las **notas, justificaciones y líneas `Origen`** viven en el catálogo y no se duplican acá: si necesitás saber *por qué* un atributo existe, es en `entidades.md`.

**Qué se dibuja:** el **modelo lógico**, no el de dominio. Las claves foráneas se representan **como atributos dentro de la caja** *y además* como línea de relación — las dos cosas, de forma consistente en todas las entidades, según exige C10. Esto es deliberado: el Anexo C del anteproyecto llama "Modelo de Datos" a su esquema, y el catálogo está construido como modelo lógico (cada relación documenta de qué lado vive la FK). Dibujarlo como modelo de dominio descartaría ese trabajo.

---

## 1. Antes de empezar: lo que conviene saber del grafo

**Hay dos hubs, y conviene ubicarlos primero.** Si se dibujan al final, todo lo demás queda mal posicionado:

| Entidad | Relaciones | Bloques que toca |
|---|---:|---|
| **`operators`** | 21 | 1, 2, 3, 4, 5, 7, 8 |
| **`conversations`** | 19 | 1, 3, 5, 6, 7 |

*(Grados medidos sobre el andamio; les siguen `ai_agents` y `generations` con 8, y `accounts` y `messages` con 6. El modelo tiene **69 relaciones** en total.)*

Recomendación de layout: **`conversations` en el centro** del tablero y **`operators` en un lateral con espacio libre alrededor**. `operators` es autor o destinatario de casi todo (calificó, resolvió, asignó, editó, subió, anotó), así que sus líneas van a cruzar todo el diagrama sin importar dónde se ponga; darle un carril propio evita que se mezclen con la estructura conversacional.

**El resto de las entidades tiene grado bajo** (2 a 8 relaciones), así que una vez fijados los dos hubs el resto se acomoda por bloque.

**Reparto sugerido por el plan:** Persona B toma los bloques 1, 5 y 6; Persona C los bloques 3, 4 y 7; Persona A el bloque 2 (que es su capa de prompts). El bloque 8 es chico y va con quien tenga menos carga. **Las relaciones marcadas con ⇄ cruzan bloques y hay que acordarlas entre dos personas** — son la única parte del dibujo que no se puede hacer en paralelo sin hablar.

**Notación de cardinalidad:** `1` exactamente uno · `0..1` cero o uno · `0..*` cero o muchos · `1..*` uno o muchos.

---

## 2. Bloque 1 — Núcleo (3 entidades)

### `contacts`
```
PK  id
FK  account_id            → accounts            ⇄ B5
    phone_whatsapp (uq parcial), name, lastname, email,
    type «ContactType», notes, source_channel «SourceChannel»,
    dni_encrypted, cuit_encrypted, cuil_encrypted, cbu_encrypted,
    consent_given_at, consent_revoked_at, opt_out, blocked,
    created_at, updated_at, deleted_at
```

### `conversations`  ← **HUB**
```
PK  id
FK  contact_id            → contacts
FK  inbox_id              → inboxes             ⇄ B6
FK  ai_agent_id      NULL → ai_agents           ⇄ B2
FK  operator_id      NULL → operators           ⇄ B5
FK  scored_by_operator_id    NULL → operators   ⇄ B5
FK  resolved_by_operator_id  NULL → operators   ⇄ B5
    status «ConversationStatus», mode «AgentMode», starred,
    quality_score, scored_at, first_response_at, last_activity_at,
    resolved_at, unread_count, last_read_at,
    last_inbound_at, window_expires_at,
    created_at, updated_at, deleted_at
```

### `messages`
```
PK  id
FK  conversation_id       → conversations
FK  message_batch_id NULL → message_batches     ⇄ B3
FK  sender_operator_id  NULL → operators        ⇄ B5
FK  sender_ai_agent_id  NULL → ai_agents        ⇄ B2
    wamid (único), context_wamid,
    direction «MessageDirection», sender_type «SenderType»,
    content_text, content_type «MessageContentType», transcription,
    media_id, media_url, media_mime_type, media_size_bytes, media_duration_ms,
    status «MessageStatus», error_code,
    sent_at, delivered_at, read_at, hidden_at, redacted_at,
    created_at
```
> **Sin `deleted_at`** (C09). Anotarlo en el diagrama: es el registro de hechos sobre el que se calculan F34–F36 y OE7.

### Relaciones internas del bloque
| Origen | Card. | Destino | FK vive en |
|---|---|---|---|
| `contacts` | 1 — 0..* | `conversations` | `conversations.contact_id` |
| `conversations` | 1 — 0..* | `messages` | `messages.conversation_id` |

### Relaciones que cruzan ⇄
| Origen | Card. | Destino | FK vive en | Bloque |
|---|---|---|---|---|
| `accounts` | 1 — 0..* | `contacts` | `contacts.account_id` | B5 |
| `inboxes` | 1 — 0..* | `conversations` | `conversations.inbox_id` | B6 |
| `ai_agents` | 0..1 — 0..* | `conversations` | `conversations.ai_agent_id` | B2 |
| `operators` | 0..1 — 0..* | `conversations` | `conversations.operator_id` | B5 |
| `operators` | 0..1 — 0..* | `conversations` | `conversations.scored_by_operator_id` | B5 |
| `operators` | 0..1 — 0..* | `conversations` | `conversations.resolved_by_operator_id` | B5 |
| `message_batches` | 0..1 — 0..* | `messages` | `messages.message_batch_id` | B3 |
| `operators` | 0..1 — 0..* | `messages` | `messages.sender_operator_id` | B5 |
| `ai_agents` | 0..1 — 0..* | `messages` | `messages.sender_ai_agent_id` | B2 |

---

## 3. Bloque 2 — Identidad del agente IA (4 entidades)

### `ai_agents`
```
PK  id
FK  account_id            → accounts            ⇄ B5
    name, description, default_mode «AgentMode», extraction_enabled,
    llm_provider, llm_model, temperature, max_tokens, active,
    created_at, updated_at, deleted_at
```
> **No lleva `knowledge_base_id`** (ADR-0005). La relación se dibuja en una sola dirección, desde `knowledge_bases`.

### `prompt_components`
```
PK  id
FK  ai_agent_id           → ai_agents
    type «PromptComponentType», content, position, active,
    created_at, updated_at
```

### `prompt_component_versions`  — inmutable
```
PK  id
FK  prompt_component_id   → prompt_components
FK  changed_by_operator_id NULL → operators     ⇄ B5
    version_number, content, change_note, created_at
```

### `transfer_policies`
```
PK  id
FK  ai_agent_id           → ai_agents
    signal_type «TransferSignalType», enabled, threshold,
    created_at, updated_at
```

### Relaciones internas del bloque
| Origen | Card. | Destino | FK vive en |
|---|---|---|---|
| `ai_agents` | 1 — 0..* | `prompt_components` | `prompt_components.ai_agent_id` |
| `prompt_components` | 1 — **1..\*** | `prompt_component_versions` | `.prompt_component_id` |
| `ai_agents` | 1 — 0..* | `transfer_policies` | `transfer_policies.ai_agent_id` |

### Relaciones que cruzan ⇄
| Origen | Card. | Destino | FK vive en | Bloque |
|---|---|---|---|---|
| `accounts` | 1 — 0..* | `ai_agents` | `ai_agents.account_id` | B5 |
| `operators` | 0..1 — 0..* | `prompt_component_versions` | `.changed_by_operator_id` | B5 |
| `ai_agents` | 0..1 — 0..* | `conversations` | `conversations.ai_agent_id` | B1 |
| `ai_agents` | 0..1 — 0..* | `messages` | `messages.sender_ai_agent_id` | B1 |
| `ai_agents` | 1 — 0..1 | `knowledge_bases` | `knowledge_bases.ai_agent_id` | B4 |
| `ai_agents` | 0..1 — 0..* | `sales` | `sales.ai_agent_id` | B5 |
| `ai_agents` | 0..1 — 0..* | `inboxes` | `inboxes.default_ai_agent_id` | B6 |
| `prompt_component_versions` | 1 — 0..* | `generation_components` | `.prompt_component_version_id` | B3 |

---

## 4. Bloque 3 — Ejecución por turno (6 entidades)

### `message_batches`
```
PK  id
FK  conversation_id       → conversations       ⇄ B1
    messages_count, timer_ms_used, closed_reason «BatchCloseReason»,
    opened_at, closed_at, processed_at
```

### `detected_intents`  — inmutable
```
PK  id
FK  message_batch_id      → message_batches
    intent_label, confidence, position, created_at
```

### `conversation_contexts`
```
PK  id
FK  conversation_id (único) → conversations     ⇄ B1
    turn_number, summary, primary_intent, objections_json,
    urgency_level, budget_signal, extracted_data_json, updated_at
```

### `conversation_context_versions`  — inmutable
```
PK  id
FK  conversation_id       → conversations       ⇄ B1
    turn_number, content, rewritten_by_model, latency_ms, created_at
```

### `generations`
```
PK  id
FK  conversation_id       → conversations       ⇄ B1
FK  message_id       NULL → messages            ⇄ B1
FK  message_batch_id NULL → message_batches
FK  conversation_context_version_id → conversation_context_versions
FK  reviewed_by_operator_id NULL → operators    ⇄ B5
    llm_provider, llm_model, temperature,
    prompt_rendered, completion_text,
    prompt_tokens, completion_tokens, latency_ms,
    cost, currency, reasoning_json,
    status «GenerationStatus», reviewed_at, created_at
```

### `generation_components`
```
PK  id
FK  generation_id         → generations
FK  prompt_component_version_id → prompt_component_versions  ⇄ B2
    position
```

### Relaciones internas del bloque
| Origen | Card. | Destino | FK vive en |
|---|---|---|---|
| `message_batches` | 1 — 0..* | `detected_intents` | `detected_intents.message_batch_id` |
| `message_batches` | 0..1 — 0..* | `generations` | `generations.message_batch_id` |
| `conversation_context_versions` | 0..1 — 0..* | `generations` | `.conversation_context_version_id` |
| `generations` | 1 — **1..\*** | `generation_components` | `generation_components.generation_id` |

### Relaciones que cruzan ⇄
| Origen | Card. | Destino | FK vive en | Bloque |
|---|---|---|---|---|
| `conversations` | 1 — 0..* | `message_batches` | `message_batches.conversation_id` | B1 |
| `conversations` | 1 — **0..1** | `conversation_contexts` | `.conversation_id` (único) | B1 |
| `conversations` | 1 — 0..* | `conversation_context_versions` | `.conversation_id` | B1 |
| `conversations` | 1 — 0..* | `generations` | `generations.conversation_id` | B1 |
| `generations` | 0..1 — 0..1 | `messages` | **`generations.message_id`** | B1 |
| `operators` | 0..1 — 0..* | `generations` | `generations.reviewed_by_operator_id` | B5 |
| `prompt_component_versions` | 1 — 0..* | `generation_components` | `.prompt_component_version_id` | B2 |
| `generations` | 1 — 0..* | `retrieval_logs` | `retrieval_logs.generation_id` | B4 |
| `generations` | 0..1 — 0..* | `tone_evaluations` | `tone_evaluations.generation_id` | B7 |

> **`generations ↔ messages`: la FK va sólo en `generations`.** Es opcional en ambos sentidos y es tentador ponerla en las dos tablas — sería el mismo error que el Anexo C cometió entre `agents` y `knowledge_base`. Ver la nota de [relaciones.md §3](relaciones.md).

---

## 5. Bloque 4 — Conocimiento / RAG (4 entidades)

### `knowledge_bases`
```
PK  id
FK  ai_agent_id (único)   → ai_agents           ⇄ B2
    collection_name, size_bytes_total, created_at, updated_at
```

### `documents`
```
PK  id
FK  knowledge_base_id     → knowledge_bases
FK  uploaded_by_operator_id → operators         ⇄ B5
    filename, mime_type, size_bytes, checksum,
    status «DocumentStatus», error_message,
    created_at, updated_at, deleted_at
```

### `document_chunks`
```
PK  id
FK  document_id           → documents
    chunk_index, content, token_count, vector_id, created_at
```

### `retrieval_logs`  — inmutable
```
PK  id
FK  generation_id         → generations         ⇄ B3
FK  document_chunk_id     → document_chunks
    similarity_score, rank, created_at
```

### Relaciones internas del bloque
| Origen | Card. | Destino | FK vive en |
|---|---|---|---|
| `knowledge_bases` | 1 — 0..* | `documents` | `documents.knowledge_base_id` |
| `documents` | 1 — 0..* | `document_chunks` | `document_chunks.document_id` |
| `document_chunks` | 1 — 0..* | `retrieval_logs` | `retrieval_logs.document_chunk_id` |

### Relaciones que cruzan ⇄
| Origen | Card. | Destino | FK vive en | Bloque |
|---|---|---|---|---|
| `ai_agents` | 1 — **0..1** | `knowledge_bases` | `knowledge_bases.ai_agent_id` (único) | B2 |
| `operators` | 1 — 0..* | `documents` | `documents.uploaded_by_operator_id` | B5 |
| `generations` | 1 — 0..* | `retrieval_logs` | `retrieval_logs.generation_id` | B3 |

---

## 6. Bloque 5 — Operación (10 entidades)

### `accounts`
```
PK  id
    name, created_at, updated_at, deleted_at
```
> Anotar en el diagrama: **una sola fila durante todo el proyecto** (ADR-0012). La estructura multi-empresa está fuera de alcance.

### `operators`  ← **HUB**
```
PK  id
FK  account_id            → accounts
    name, lastname, email (uq parcial), password_hash,
    role «OperatorRole», active,
    created_at, updated_at, deleted_at
```

### `conversation_assignments`  — inmutable
```
PK  id
FK  conversation_id       → conversations       ⇄ B1
FK  from_operator_id NULL → operators
FK  to_operator_id        → operators
FK  assigned_by_operator_id NULL → operators
    reason, created_at
```

### `internal_notes`
```
PK  id
FK  conversation_id       → conversations       ⇄ B1
FK  operator_id           → operators
    content, created_at, updated_at, deleted_at
```

### `labels`
```
PK  id
FK  account_id            → accounts
FK  created_by_operator_id → operators
    name (uq parcial), color, created_at, updated_at, deleted_at
```

### `contact_labels`
```
PK  id
FK  contact_id            → contacts            ⇄ B1
FK  label_id              → labels
FK  assigned_by_operator_id NULL → operators
    created_at
```

### `conversation_labels`
```
PK  id
FK  conversation_id       → conversations       ⇄ B1
FK  label_id              → labels
FK  assigned_by_operator_id NULL → operators
    created_at
```

### `reminders`
```
PK  id
FK  contact_id            → contacts            ⇄ B1   (obligatorio)
FK  conversation_id  NULL → conversations       ⇄ B1
FK  operator_id           → operators
    description, due_at, notified, notified_at,
    created_at, updated_at, deleted_at
```

### `sales`
```
PK  id
FK  conversation_id       → conversations       ⇄ B1
FK  operator_id           → operators
FK  ai_agent_id      NULL → ai_agents           ⇄ B2
    amount, currency, created_at, updated_at, deleted_at
```
> **Sin `source_channel`** (ADR-0001): el origen vive en `contacts` y se alcanza por join.

### `quick_replies`
```
PK  id
FK  account_id            → accounts
FK  created_by_operator_id → operators
    title, content, active, created_at, updated_at, deleted_at
```

### Relaciones internas del bloque
| Origen | Card. | Destino | FK vive en |
|---|---|---|---|
| `accounts` | 1 — 0..* | `operators` | `operators.account_id` |
| `accounts` | 1 — 0..* | `labels` | `labels.account_id` |
| `accounts` | 1 — 0..* | `quick_replies` | `quick_replies.account_id` |
| `operators` | 0..1 — 0..* | `conversation_assignments` | `.from_operator_id` |
| `operators` | 1 — 0..* | `conversation_assignments` | `.to_operator_id` |
| `operators` | 0..1 — 0..* | `conversation_assignments` | `.assigned_by_operator_id` |
| `operators` | 1 — 0..* | `internal_notes` | `internal_notes.operator_id` |
| `operators` | 1 — 0..* | `labels` | `labels.created_by_operator_id` |
| `operators` | 0..1 — 0..* | `contact_labels` | `.assigned_by_operator_id` |
| `operators` | 0..1 — 0..* | `conversation_labels` | `.assigned_by_operator_id` |
| `operators` | 1 — 0..* | `reminders` | `reminders.operator_id` |
| `operators` | 1 — 0..* | `sales` | `sales.operator_id` |
| `operators` | 1 — 0..* | `quick_replies` | `quick_replies.created_by_operator_id` |
| `labels` | 1 — 0..* | `contact_labels` | `contact_labels.label_id` |
| `labels` | 1 — 0..* | `conversation_labels` | `conversation_labels.label_id` |

### Relaciones que cruzan ⇄
| Origen | Card. | Destino | FK vive en | Bloque |
|---|---|---|---|---|
| `accounts` | 1 — 0..* | `contacts` | `contacts.account_id` | B1 |
| `accounts` | 1 — 0..* | `ai_agents` | `ai_agents.account_id` | B2 |
| `accounts` | 1 — 0..* | `inboxes` | `inboxes.account_id` | B6 |
| `conversations` | 1 — 0..* | `conversation_assignments` | `.conversation_id` | B1 |
| `conversations` | 1 — 0..* | `internal_notes` | `.conversation_id` | B1 |
| `contacts` | 1 — 0..* | `contact_labels` | `contact_labels.contact_id` | B1 |
| `conversations` | 1 — 0..* | `conversation_labels` | `.conversation_id` | B1 |
| `contacts` | 1 — 0..* | `reminders` | `reminders.contact_id` | B1 |
| `conversations` | 0..1 — 0..* | `reminders` | `reminders.conversation_id` | B1 |
| `conversations` | 1 — 0..* | `sales` | `sales.conversation_id` | B1 |
| `ai_agents` | 0..1 — 0..* | `sales` | `sales.ai_agent_id` | B2 |
| `operators` | 0..1 — 0..* | `conversations` (×3) | `operator_id` · `scored_by_` · `resolved_by_` | B1 |
| `operators` | 0..1 — 0..* | `messages` | `messages.sender_operator_id` | B1 |
| `operators` | 0..1 — 0..* | `prompt_component_versions` | `.changed_by_operator_id` | B2 |
| `operators` | 0..1 — 0..* | `generations` | `.reviewed_by_operator_id` | B3 |
| `operators` | 1 — 0..* | `documents` | `.uploaded_by_operator_id` | B4 |
| `operators` | 0..1 — 0..* | `transfer_events` | `.to_operator_id` | B7 |
| `operators` | 1 — 0..* | `conversation_annotations` | `.annotated_by_operator_id` | B7 |
| `operators` | 1 — 0..* | `audit_logs` | `audit_logs.operator_id` | B8 |

---

## 7. Bloque 6 — Canal e integración (3 entidades)

### `inboxes`
```
PK  id
FK  account_id            → accounts            ⇄ B5
FK  default_ai_agent_id NULL → ai_agents        ⇄ B2
    phone_number_id, waba_id, display_name,
    access_token_encrypted, verify_token, app_secret_encrypted,
    status «InboxStatus», created_at, updated_at
```

### `webhook_events`
```
PK  id
FK  inbox_id              → inboxes
    event_type «WebhookEventType», wamid, payload_json,
    signature_valid, status «WebhookEventStatus», retries,
    error_message, received_at, processed_at
```

### `meta_templates`
```
PK  id
FK  inbox_id              → inboxes
    meta_template_id, name, language,
    category «MetaTemplateCategory», status «MetaTemplateStatus»,
    body, rejection_reason, llm_suggestion, synced_at,
    created_at, updated_at
```

### Relaciones internas del bloque
| Origen | Card. | Destino | FK vive en |
|---|---|---|---|
| `inboxes` | 1 — 0..* | `webhook_events` | `webhook_events.inbox_id` |
| `inboxes` | 1 — 0..* | `meta_templates` | `meta_templates.inbox_id` |

### Relaciones que cruzan ⇄
| Origen | Card. | Destino | FK vive en | Bloque |
|---|---|---|---|---|
| `accounts` | 1 — 0..* | `inboxes` | `inboxes.account_id` | B5 |
| `ai_agents` | 0..1 — 0..* | `inboxes` | `inboxes.default_ai_agent_id` | B2 |
| `inboxes` | 1 — 0..* | `conversations` | `conversations.inbox_id` | B1 |

> **`webhook_events` y `messages` NO llevan FK entre sí** — se correlacionan por el valor de `wamid`. **No dibujar esa línea.** El motivo está en [relaciones.md §6](relaciones.md): un evento se registra antes de saber si va a producir un mensaje, y muchos no lo producen.

---

## 8. Bloque 7 — Evaluación (3 entidades)

### `transfer_events`  — inmutable
```
PK  id
FK  conversation_id       → conversations       ⇄ B1
FK  to_operator_id   NULL → operators           ⇄ B5
    trigger_type «TransferSignalType», confidence, threshold_used,
    classifier_version, turn_number, created_at
```

### `conversation_annotations`  — inmutable
```
PK  id
FK  conversation_id       → conversations       ⇄ B1
FK  annotated_by_operator_id → operators        ⇄ B5
    annotation_type «AnnotationType», gold_value, notes, created_at
```

### `tone_evaluations`  — inmutable
```
PK  id
FK  conversation_id       → conversations       ⇄ B1
FK  generation_id    NULL → generations         ⇄ B3
    tone_score, rubric_version, judge_model, rationale, created_at
```

### Relaciones internas del bloque
Ninguna. **Las tres entidades cuelgan de `conversations` y no se relacionan entre sí.**

### Relaciones que cruzan ⇄
| Origen | Card. | Destino | FK vive en | Bloque |
|---|---|---|---|---|
| `conversations` | 1 — 0..* | `transfer_events` | `.conversation_id` | B1 |
| `conversations` | 1 — 0..* | `conversation_annotations` | `.conversation_id` | B1 |
| `conversations` | 1 — 0..* | `tone_evaluations` | `.conversation_id` | B1 |
| `operators` | 0..1 — 0..* | `transfer_events` | `.to_operator_id` | B5 |
| `operators` | 1 — 0..* | `conversation_annotations` | `.annotated_by_operator_id` | B5 |
| `generations` | 0..1 — 0..* | `tone_evaluations` | `.generation_id` | B3 |

> **`conversation_annotations` NO se relaciona con `labels`, y no hay que dibujar ninguna línea entre ambas.** Comparten la palabra "etiqueta" en castellano y nada más: `labels` es el catálogo operativo del negocio (F19, F24, F33); `conversation_annotations` es el ground truth de evaluación de OE3 y OE4.

---

## 9. Bloque 8 — Cumplimiento y auditoría (2 entidades)

### `contact_attributes`
```
PK  id
FK  contact_id            → contacts            ⇄ B1
FK  source_message_id NULL → messages           ⇄ B1
    key, value_encrypted, source «AttributeSource», confidence,
    created_at, updated_at, deleted_at
```

### `audit_logs`  — inmutable, sin borrado
```
PK  id
FK  operator_id           → operators           ⇄ B5
    action, entity_type, entity_id, changes_json, ip_address, created_at
```
> **`entity_id` NO es una FK y no lleva línea de relación.** Es deliberado: el registro de auditoría debe sobrevivir al borrado de lo que audita. Anotarlo en el diagrama para que no se lea como un olvido.

### Relaciones que cruzan ⇄
| Origen | Card. | Destino | FK vive en | Bloque |
|---|---|---|---|---|
| `contacts` | 1 — 0..* | `contact_attributes` | `.contact_id` | B1 |
| `messages` | 0..1 — 0..* | `contact_attributes` | `.source_message_id` | B1 |
| `operators` | 1 — 0..* | `audit_logs` | `audit_logs.operator_id` | B5 |

---

## 10. Las 21 enumeraciones

Se declaran con el estereotipo **«enumeration»** (C07). Conviene agruparlas en un sector aparte del tablero, no dispersas junto a las entidades que las usan — si no, las líneas de las enumeraciones compiten visualmente con las relaciones reales.

`ContactType` · `OperatorRole` · `AgentMode` · `ConversationStatus` · `MessageDirection` · `MessageContentType` · `SenderType` · `MessageStatus` · `SourceChannel` · `PromptComponentType` · `TransferSignalType` · `BatchCloseReason` · `GenerationStatus` · `DocumentStatus` · `MetaTemplateStatus` · `MetaTemplateCategory` · `AttributeSource` · `AnnotationType` · `WebhookEventType` · `WebhookEventStatus` · `InboxStatus`

Los valores están en [entidades.md §1](entidades.md).

---

## 11. Las reglas que van como nota, no como relación

No son cardinalidades y **no se pueden dibujar como línea**. C10 exige que vayan como nota en el tablero. Lista completa en [relaciones.md §9](relaciones.md); estas son las que más fácil se olvidan:

- **`accounts` tiene exactamente una fila** durante todo el proyecto (ADR-0012).
- **`knowledge_bases.collection_name` y `document_chunks.vector_id` apuntan a Qdrant, fuera de PostgreSQL.** No son FK y **no llevan línea de relación**; la consistencia con el vector store es eventual (ADR-0010). Anotarlo evita que se lea como una FK olvidada.
- Un contacto tiene **como máximo una conversación no resuelta** a la vez (ADR-0002).
- Un `prompt_component` tiene **un solo tipo activo por agente** (F01: seis componentes, uno de cada tipo).
- **`transfer_policies` manda sobre `rules`.** La decisión de transferir la toma el clasificador según sus umbrales; el componente `rules` no puede habilitarla ni inhibirla. Sin esta nota, las dos entidades parecen configurar lo mismo por dos vías.
- `sender_operator_id` y `sender_ai_agent_id` son **excluyentes** y deben coincidir con `sender_type` (`CHECK`).
- La suma de `documents.size_bytes` de una base **no supera 100 MB** (F06).
- `messages` **no admite borrado** de ninguna clase (F29 · C09).
- Sólo un `admin` crea respuestas rápidas y etiquetas (F09, F19).
- Un `operator` sólo alcanza las conversaciones que tiene asignadas (ADR-0011).

---

## 12. Checklist antes de dar por cerrado el Paso 5

- [ ] Las 35 entidades dibujadas, repartidas en los 8 bloques.
- [ ] Las 21 enumeraciones declaradas con «enumeration» (C07).
- [ ] Las FK se representan **de la misma manera en todas las entidades** — atributo en la caja **y** línea de relación, sin mezclar criterios (C10).
- [ ] Las cinco cardinalidades de [relaciones.md §10](relaciones.md) revisadas explícitamente: son las que la lectura ingenua del anteproyecto equivoca.
- [ ] Ninguna línea entre `webhook_events` y `messages`, ni entre `conversation_annotations` y `labels`, ni desde `audit_logs.entity_id`.
- [ ] Las ocho reglas de §11 anotadas en el tablero.
- [ ] Título, fecha y nota de propósito en el diagrama (C10).
- [ ] Exportado a `docs/diagrams/` en PNG y PDF **con la fecha en el nombre**: `der-logico-AAAA-MM-DD.png` (C10).
- [ ] Verificado contra el andamio Mermaid (`docs/diagrams/der-logico-scaffold.mmd`): mismo conjunto de relaciones, sin sobrantes ni faltantes.

> **Estado del andamio al 2026-08-09:** parsea correctamente y cruza limpio contra el catálogo — 35 entidades en ambos, ninguna faltante ni sobrante, 69 relaciones. Si al dibujar aparece una relación que el andamio no tiene, **el error puede estar en cualquiera de los dos**: se resuelve mirando el catálogo, que es la fuente de verdad (ADR-0013).
>
> **Al editar el andamio, cuidado con una trampa de Mermaid:** una línea de comentario vacía —sólo `%%`, sin texto— **rompe el parser**. Mermaid elimina los comentarios con al menos un carácter después del prefijo, pero deja los vacíos, y quedan como basura antes de `erDiagram`. Toda línea de comentario tiene que llevar texto.
