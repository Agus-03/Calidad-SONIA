# SONIA — Relaciones entre entidades

| Campo | Valor |
|---|---|
| **Documento** | `docs/design/DER/relaciones.md` |
| **Estado** | Borrador para el Paso 5 (modelo de datos lógico) |
| **Fecha** | 2026-08-06 |
| **Complemento** | [entidades.md](entidades.md) — atributos de cada entidad |
| **Base documental** | [Anteproyecto](../../SONIA_Anteproyecto.pdf) · [00-convenciones.md](../00-convenciones.md) · [01-trazabilidad.md](../01-trazabilidad.md) · [02-decisiones/](../02-decisiones/) |

---

## 0. Notación y reglas

**Cardinalidad:** `1` exactamente uno · `0..1` cero o uno · `0..*` cero o muchos · `1..*` uno o muchos.

Cada relación indica **dónde vive la clave foránea**, porque esa es la decisión de modelado: la cardinalidad describe el mundo, la ubicación de la FK describe la tabla.

### Tres reglas que gobiernan todas las relaciones

1. **Una relación se modela en un solo lugar** (C05). Dos entidades que se apuntan mutuamente para expresar el mismo vínculo no son redundancia: son una inconsistencia esperando ocurrir. El Anexo C tenía ese problema entre `agents` y `knowledge_base`.
2. **Sin borrado en cascada** (C05). `ON DELETE RESTRICT` por defecto. Un contacto dado de baja no se lleva consigo sus conversaciones: F32 exige que el historial sobreviva.
3. **Toda FK opcional documenta por qué puede ser nula.** Una FK nullable sin explicación es una relación mal entendida.

---

## 1. Bloque 1 — Núcleo

| Origen | Cardinalidad | Destino | FK vive en | Por qué |
|---|---|---|---|---|
| `contacts` | 1 — 0..* | `conversations` | `conversations.contact_id` | Un contacto puede existir sin conversaciones: F30 permite el alta manual |
| `conversations` | 1 — 0..* | `messages` | `messages.conversation_id` | |
| `operators` | 0..1 — 0..* | `conversations` | `conversations.operator_id` | **Nullable.** Pool sin asignar (ADR-0004) o modo autónomo (F02) |
| `ai_agents` | 0..1 — 0..* | `conversations` | `conversations.ai_agent_id` | **Nullable.** En modo `off` no hay agente asignado |
| `operators` | 0..1 — 0..* | `conversations` | `conversations.scored_by_operator_id` | **Nullable.** Sólo las conversaciones calificadas (F13) |
| `operators` | 0..1 — 0..* | `conversations` | `conversations.resolved_by_operator_id` | **Nullable.** El cierre es manual (ADR-0002): sólo las resueltas |
| `message_batches` | 0..1 — 0..* | `messages` | `messages.message_batch_id` | **Nullable.** Sólo los entrantes agrupados por el timer (F10) |
| `operators` | 0..1 — 0..* | `messages` | `messages.sender_operator_id` | **Nullable.** Poblado sólo cuando `sender_type = operator` |
| `ai_agents` | 0..1 — 0..* | `messages` | `messages.sender_ai_agent_id` | **Nullable.** Poblado sólo cuando `sender_type = ai_agent` |

> **Sobre el emisor.** Se usan dos FK nullables en vez de un `sender_id` polimórfico. El polimorfismo ahorraría una columna, pero pierde la integridad referencial declarada — el mismo motivo por el que ADR-0003 descartó una tabla de etiquetado polimórfica.

---

## 2. Bloque 2 — Identidad del agente IA

| Origen | Cardinalidad | Destino | FK vive en | Por qué |
|---|---|---|---|---|
| `ai_agents` | 1 — 0..* | `prompt_components` | `prompt_components.ai_agent_id` | Hasta seis por agente, uno por tipo de F01 |
| `prompt_components` | 1 — 1..* | `prompt_component_versions` | `prompt_component_versions.prompt_component_id` | **`1..*`**: crear un componente crea su primera versión. Sin al menos una, OE2 no puede decir contra qué se midió |
| `operators` | 0..1 — 0..* | `prompt_component_versions` | `prompt_component_versions.changed_by_operator_id` | **Nullable.** Una carga inicial o una migración puede no tener autor |
| `ai_agents` | 1 — 0..* | `transfer_policies` | `transfer_policies.ai_agent_id` | Hasta cuatro por agente, una por señal de F12 |

---

## 3. Bloque 3 — Ejecución por turno

| Origen | Cardinalidad | Destino | FK vive en | Por qué |
|---|---|---|---|---|
| `conversations` | 1 — 0..* | `message_batches` | `message_batches.conversation_id` | |
| `message_batches` | 1 — 0..* | `detected_intents` | `detected_intents.message_batch_id` | F11 detecta *cuántas* intenciones hay: puede ser ninguna, una o varias |
| `conversations` | 1 — 0..1 | `conversation_contexts` | `conversation_contexts.conversation_id` (único) | **`0..1`**: una conversación recién abierta todavía no tuvo turno, así que no tiene contexto |
| `conversations` | 1 — 0..* | `conversation_context_versions` | `conversation_context_versions.conversation_id` | Una por turno reescrito. **Es el histórico que OE2 necesita** |
| `conversations` | 1 — 0..* | `generations` | `generations.conversation_id` | |
| `generations` | 0..1 — 0..1 | `messages` | **`generations.message_id`** | **Nullable.** Una generación rechazada en modo sugerencia nunca produce mensaje (F02). Ver la nota de abajo |
| `conversation_context_versions` | 0..1 — 0..* | `generations` | `generations.conversation_context_version_id` | **La versión exacta del contexto que se usó.** Es la mitad de la trazabilidad de OE2 |
| `message_batches` | 0..1 — 0..* | `generations` | `generations.message_batch_id` | **Nullable.** El bloque que disparó la generación |
| `operators` | 0..1 — 0..* | `generations` | `generations.reviewed_by_operator_id` | **Nullable.** Sólo las revisadas en modo sugerencia |
| `generations` | 1 — 1..* | `generation_components` | `generation_components.generation_id` | **`1..*`**: toda generación ensambla al menos un componente |
| `prompt_component_versions` | 1 — 0..* | `generation_components` | `generation_components.prompt_component_version_id` | **La otra mitad de la trazabilidad de OE2**, y la respuesta literal a F05 |

> **Nota — dirección única entre `generations` y `messages`.** La relación es opcional en ambos sentidos: una generación rechazada no tiene mensaje, y un mensaje entrante o escrito por un operador no tiene generación. Es tentador poner la FK en las dos tablas, y sería el mismo error que el Anexo C cometió entre `agents` y `knowledge_base`.
>
> **La FK vive únicamente en `generations.message_id`**, por orden temporal: la generación existe antes que el mensaje —se produce, se revisa y recién entonces se envía—, así que es la que puede aprender el vínculo. Para responder *"¿qué razonamiento produjo este mensaje?"* se consulta `generations` por `message_id`, que va indexado.

> **Cómo se lee la trazabilidad completa de una respuesta.** Dado un `message`, su `generation` da el prompt renderizado, el costo y la latencia; `generation_components` da **qué componentes, en qué versión** se ensamblaron; `conversation_context_version_id` da **el estado exacto del contexto dinámico** de ese turno; y `retrieval_logs` da **qué fragmentos** recuperó el RAG. Esa cadena de cuatro saltos es lo que hace reproducible a OE2.

---

## 4. Bloque 4 — Conocimiento (RAG)

| Origen | Cardinalidad | Destino | FK vive en | Por qué |
|---|---|---|---|---|
| `ai_agents` | 1 — 0..1 | `knowledge_bases` | `knowledge_bases.ai_agent_id` (único) | **Dirección única** (ADR-0005, C05). `0..1` porque un agente puede no tener RAG configurado |
| `knowledge_bases` | 1 — 0..* | `documents` | `documents.knowledge_base_id` | **La corrección central de H-06**: F06 habla de documentos en plural |
| `documents` | 1 — 0..* | `document_chunks` | `document_chunks.document_id` | `0..*` porque un documento recién subido aún no está fragmentado (`status = pending`) |
| `operators` | 1 — 0..* | `documents` | `documents.uploaded_by_operator_id` | |
| `generations` | 1 — 0..* | `retrieval_logs` | `retrieval_logs.generation_id` | `0..*`: una generación puede no haber consultado el RAG |
| `document_chunks` | 1 — 0..* | `retrieval_logs` | `retrieval_logs.document_chunk_id` | Qué fragmento se recuperó y con qué score — **OE6** |

---

## 5. Bloque 5 — Operación

| Origen | Cardinalidad | Destino | FK vive en | Por qué |
|---|---|---|---|---|
| `conversations` | 1 — 0..* | `conversation_assignments` | `conversation_assignments.conversation_id` | `0..*`: una conversación en el pool todavía no fue asignada |
| `operators` | 0..1 — 0..* | `conversation_assignments` | `.from_operator_id` | **Nullable.** Nulo en la primera asignación desde el pool (ADR-0004) |
| `operators` | 1 — 0..* | `conversation_assignments` | `.to_operator_id` | Siempre hay destinatario |
| `operators` | 0..1 — 0..* | `conversation_assignments` | `.assigned_by_operator_id` | **Nullable.** En la autoasignación coincide con el destinatario |
| `conversations` | 1 — 0..* | `internal_notes` | `internal_notes.conversation_id` | F26 — notas preservadas en la reasignación |
| `operators` | 1 — 0..* | `internal_notes` | `internal_notes.operator_id` | |
| `contacts` | 1 — 0..* | `contact_labels` | `contact_labels.contact_id` | F33 |
| `labels` | 1 — 0..* | `contact_labels` | `contact_labels.label_id` | |
| `conversations` | 1 — 0..* | `conversation_labels` | `conversation_labels.conversation_id` | **F24 — hoy no representable** (H-07) |
| `labels` | 1 — 0..* | `conversation_labels` | `conversation_labels.label_id` | |
| `operators` | 0..1 — 0..* | `contact_labels` / `conversation_labels` | `.assigned_by_operator_id` | **Nullable.** El etiquetado automático no tiene autor humano |
| `operators` | 1 — 0..* | `labels` | `labels.created_by_operator_id` | F19: sólo `admin` los crea (ver §8) |
| `contacts` | 1 — 0..* | `reminders` | `reminders.contact_id` | **Obligatorio** (ADR-0002): F28 vincula recordatorios a contactos |
| `conversations` | 0..1 — 0..* | `reminders` | `reminders.conversation_id` | **Nullable** (ADR-0002): el recordatorio sobrevive al cierre de la conversación que lo originó |
| `operators` | 1 — 0..* | `reminders` | `reminders.operator_id` | Quién recibe la notificación |
| `conversations` | 1 — 0..* | `sales` | `sales.conversation_id` | F14 |
| `operators` | 1 — 0..* | `sales` | `sales.operator_id` | F35 — métricas por vendedor |
| `ai_agents` | 0..1 — 0..* | `sales` | `sales.ai_agent_id` | **Nullable.** F14: *"el agente que la gestionó"*; puede no haber intervenido |
| `operators` | 1 — 0..* | `quick_replies` | `quick_replies.created_by_operator_id` | F09 |

> **El origen del contacto no aparece en `sales`** (ADR-0001). F14 pide vincular la venta al canal de origen, y eso se resuelve con `sales → conversations → contacts.source_channel`. Duplicarlo sería crear dos versiones del mismo dato.

---

## 6. Bloque 6 — Canal e integración

| Origen | Cardinalidad | Destino | FK vive en | Por qué |
|---|---|---|---|---|
| `channel_configs` | 1 — 0..* | `webhook_events` | `webhook_events.channel_config_id` | F20 — log de eventos |
| `channel_configs` | 1 — 0..* | `meta_templates` | `meta_templates.channel_config_id` | F08, F37 |

> **`webhook_events` y `messages` no tienen FK entre sí.** Se correlacionan por el valor de `wamid`, que ambas guardan. El motivo: un evento de webhook se registra **antes** de saber si producirá un mensaje —y muchos no lo producen, como los de estado de entrega o los que llegan con firma inválida—. Forzar una FK obligaría a crear el mensaje antes de haber validado el evento, que es exactamente lo contrario del orden correcto.

---

## 7. Bloque 7 — Evaluación

| Origen | Cardinalidad | Destino | FK vive en | Por qué |
|---|---|---|---|---|
| `conversations` | 1 — 0..* | `transfer_events` | `transfer_events.conversation_id` | `0..*`: una conversación resuelta de forma autónoma no tiene ninguno — **y esa ausencia es la métrica de OE7** |
| `operators` | 0..1 — 0..* | `transfer_events` | `transfer_events.to_operator_id` | **Nullable.** La transferencia deja la conversación en el pool si nadie la toma todavía (ADR-0004) |
| `conversations` | 1 — 0..* | `conversation_annotations` | `conversation_annotations.conversation_id` | Varias por conversación: una por tipo de anotación (transferencia, frustración, intención) |
| `operators` | 1 — 0..* | `conversation_annotations` | `.annotated_by_operator_id` | Permite medir el acuerdo entre etiquetadores |
| `conversations` | 1 — 0..* | `tone_evaluations` | `tone_evaluations.conversation_id` | |
| `generations` | 0..1 — 0..* | `tone_evaluations` | `tone_evaluations.generation_id` | **Nullable.** La evaluación puede ser de una respuesta puntual o de la conversación completa |

> **`conversation_annotations` y `labels` no se relacionan, y no deben confundirse.** `labels` es el catálogo operativo del negocio (F19, F24, F33); `conversation_annotations` es el **ground truth de evaluación** de OE3 y OE4. Comparten la palabra "etiqueta" en castellano y nada más: una la usa el operador para organizar su trabajo, la otra el equipo para medir un clasificador.

---

## 8. Bloque 8 — Cumplimiento y auditoría

| Origen | Cardinalidad | Destino | FK vive en | Por qué |
|---|---|---|---|---|
| `contacts` | 1 — 0..* | `contact_attributes` | `contact_attributes.contact_id` | F31 — datos extraídos por el LLM |
| `messages` | 0..1 — 0..* | `contact_attributes` | `contact_attributes.source_message_id` | **Nullable.** Nulo si el dato se cargó a mano. **Qué mensaje originó el dato** — trazabilidad ante la Ley 25.326 |
| `operators` | 1 — 0..* | `audit_logs` | `audit_logs.operator_id` | ADR-0007 |

> **`audit_logs` no tiene FK a la entidad auditada.** Guarda `entity_type` + `entity_id` sin restricción referencial, deliberadamente: el registro debe sobrevivir al borrado de lo que audita. Es la única excepción a la regla 1 de §0, y se acepta porque una FK aquí convertiría al registro de auditoría en un obstáculo para borrar datos — justo lo contrario de su función.

---

## 9. Reglas que no son relaciones

Restricciones reales del sistema que **no se pueden expresar como cardinalidad**. Van como nota en el diagrama y como `CHECK`, índice parcial o regla de aplicación.

| Regla | Origen | Cómo se implementa |
|---|---|---|
| Sólo un `admin` puede crear respuestas rápidas y etiquetas | F09, F19 · Anexo B | Regla de aplicación + nota en el diagrama. No es expresable como relación |
| El módulo de contactos (F30, F32, F33) es exclusivo de `admin` | ADR-0011 | Regla de aplicación |
| Un `operator` sólo alcanza las conversaciones que tiene asignadas | Anexo B · ADR-0011 | Regla de aplicación sobre `conversations.operator_id` |
| `sender_operator_id` y `sender_ai_agent_id` son excluyentes y deben coincidir con `sender_type` | H-09 | `CHECK` sobre `messages` |
| Un contacto tiene **como máximo una conversación no resuelta** a la vez | ADR-0002 — un mensaje entrante sobre una resuelta abre una nueva | Índice único parcial sobre `conversations (contact_id) WHERE status <> 'resolved' AND deleted_at IS NULL` |
| `phone_whatsapp`, `email` de operador y `name` de etiqueta son únicos **entre los no borrados** | C09 | Índice único parcial `WHERE deleted_at IS NULL` |
| La suma de `documents.size_bytes` de una base no supera 100 MB | F06 | Regla de aplicación; `knowledge_bases.size_bytes_total` la materializa |
| Un `prompt_component` tiene un solo tipo activo por agente | F01 — seis componentes, uno de cada tipo | Índice único parcial sobre `(ai_agent_id, type) WHERE active AND deleted_at IS NULL` |
| `messages` no admite borrado de ninguna clase | F29 · C09 | Ausencia de `deleted_at` + regla de aplicación |

---

## 10. Cardinalidades que requieren atención

Cinco puntos donde la lectura ingenua del anteproyecto lleva a una cardinalidad equivocada. Conviene revisarlos explícitamente al dibujar el DER.

| # | Relación | Lectura ingenua | Correcta | Motivo |
|---|---|---|---|---|
| 1 | `conversations` — `operators` | `1` — toda conversación tiene operador | **`0..1`** | Contradiría F02: una conversación en modo autónomo no tiene operador humano. Y ADR-0004 define un pool sin asignar |
| 2 | `conversations` — `ai_agents` | `1` | **`0..1`** | En modo `off` no hay agente |
| 3 | `ai_agents` — `knowledge_bases` | Bidireccional (como en el Anexo C) | **Una sola dirección**, desde `knowledge_bases` | C05. La duplicación del Anexo C es una garantía de inconsistencia |
| 4 | `knowledge_bases` — `documents` | `1 — 1` (un archivo) | **`1 — 0..*`** | H-06: F06 habla de documentos en plural, con límite **por agente** |
| 5 | `generations` — `messages` | FK en ambas tablas | **Sólo en `generations`** | Misma trampa que el punto 3. Ver la nota de §3 |

---

## 11. Cómo se verifica este modelo

Las relaciones de este documento no se dan por buenas hasta pasar el **Paso 6** del plan de diseño: escribir, contra este modelo y sin ejecutarlas, las consultas SQL de cada métrica declarada en OE1–OE7 y en F34–F38.

El criterio es binario: **si una métrica del anteproyecto no se puede escribir como consulta, falta un dato y el modelo no está terminado.** Las cadenas críticas a verificar son cuatro:

1. **OE7 — resolución autónoma:** `conversations` resueltas sin `transfer_events`, filtrando por `messages.sender_type = 'ai_agent'`.
2. **OE2 — consistencia de tono:** `tone_evaluations` agrupadas por las versiones de componente que realmente se usaron, vía `generations → generation_components → prompt_component_versions`.
3. **OE3 y OE4 — precisión de los clasificadores:** `transfer_events` contra `conversation_annotations`, agrupando por `classifier_version`.
4. **OE6 — resolución dentro del dominio:** `retrieval_logs` con su `similarity_score` contra la ausencia de `transfer_events` de tipo `out_of_domain`.
