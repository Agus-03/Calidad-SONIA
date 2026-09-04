# SONIA — Estructura del código

| Campo | Valor |
|---|---|
| **Qué es** | El árbol de archivos completo, con el propósito de cada paquete y **de qué tablas es dueño** |
| **Autoridad** | Detalla la sección *Estructura del código* de [`CLAUDE.md`](../../CLAUDE.md), que es la fuente. **Enmienda a [ADR-0017](02-decisiones/0017-decomposicion-en-despliegues-y-lenguajes.md) §6.1** — el ADR sigue en Propuesta y su árbol es el anterior |
| **Qué no es** | No contiene decisiones ni su justificación. El *por qué* de cada corte está en ADR-0017; las convenciones de nombres, en [`00-convenciones.md`](00-convenciones.md); el estado de avance del diseño, en [`ESTADO.md`](ESTADO.md) |
| **Última revisión** | 2026-09-03 |

---

## 1. El árbol

```
sonia/
├── CLAUDE.md   ·   README.md   ·   LICENSE
├── Makefile                            db-migrate · db-seed · db-reset · test
├── .env.example                        plantilla; .env está en .gitignore
├── .gitignore                          go.mod, go.sum y contracts/ SÍ se commitean
├── .dockerignore                       en la RAÍZ: es la raíz del contexto de build
├── .github/workflows/ci.yml
│
├── contracts/                          fuente única compartida por los dos servicios
│   ├── enums.yaml                      las 22 enumeraciones · el esquema las aplica con CHECK
│   └── brain.openapi.json              contrato del cerebro, generado por FastAPI
│
├── deploy/                             context: .. para que los builds vean contracts/
│   ├── docker-compose.yml              el puerto del cerebro NO se publica al host
│   ├── core.Dockerfile
│   ├── brain.Dockerfile
│   └── frontend.Dockerfile
│
├── docs/
│   ├── SONIA_Anteproyecto.pdf          fuente de verdad de requisitos y objetivos
│   ├── design/
│   │   ├── 00-convenciones.md
│   │   ├── 01-trazabilidad.md          F01–F39 y OE1–OE7 con sus hallazgos
│   │   ├── 03-capa-prompts.md
│   │   ├── 05-consultas-de-metricas.sql
│   │   ├── 07-secuencia-del-turno.md
│   │   ├── struct.md                   este documento
│   │   ├── ESTADO.md
│   │   ├── 02-decisiones/              los ADR · README.md + 0001 a 0017
│   │   └── DER/                        entidades.md · relaciones.md · guia-de-dibujo.md
│   └── diagrams/
│
├── z_helps/                            material de trabajo · sin peso oficial · ignorado
│
└── src/
    │
    ├── core/                           ── DESPLEGABLE 1 · Go · gin ──
    │   ├── go.mod   ·   go.sum
    │   ├── main.go                     carga y valida la config, arma e inyecta todo
    │   ├── internal/
    │   │   │
    │   │   ├── whatsapp/               canal · F20, F29
    │   │   │   ├── webhook.go          entrada · valida la firma HMAC-SHA256
    │   │   │   ├── client.go           envío a Meta
    │   │   │   ├── service.go
    │   │   │   ├── repository.go
    │   │   │   └── model.go
    │   │   │
    │   │   ├── meta_templates/         plantillas de Meta · F08, F37
    │   │   │   └── controller · service · repository · model
    │   │   │
    │   │   ├── turn/                   orquestador del turno · ABRE LA TRANSACCIÓN
    │   │   │   ├── timer.go            agrupa mensajes fragmentados · F10
    │   │   │   ├── orchestrator.go     conduce: cerebro → persistir → enviar
    │   │   │   ├── retry.go            reintentos configurables por entorno
    │   │   │   ├── repository.go       sus tablas · más generations.message_id
    │   │   │   └── model.go
    │   │   │
    │   │   ├── conversations/          DUEÑO del agregado compartido
    │   │   │   ├── service.go          turn/, inbox/ y whatsapp/ escriben por acá
    │   │   │   ├── repository.go       recibe el Executor de quien conduce
    │   │   │   └── model.go
    │   │   │
    │   │   ├── agents/                 identidad del agente IA · F01, F02, F17
    │   │   │   └── controller · service · repository · model
    │   │   │
    │   │   ├── documents/              bases de conocimiento y carga · F06 (ABM)
    │   │   │   ├── controller.go       recibe el upload de hasta 100 MB
    │   │   │   ├── service.go          bytes a platform/storage, fila en pending
    │   │   │   ├── repository.go
    │   │   │   └── model.go
    │   │   │
    │   │   ├── inbox/                  bandeja y tiempo real · F05, F13, F21–F27
    │   │   │   ├── controller.go
    │   │   │   ├── websocket.go        fan-out a los operadores conectados
    │   │   │   ├── service.go
    │   │   │   ├── repository.go
    │   │   │   └── model.go
    │   │   │
    │   │   ├── contacts/               contactos y segmentación · F15, F30, F33
    │   │   │   └── controller · service · repository · model
    │   │   │                           persiste también los atributos de F31
    │   │   │
    │   │   ├── labels/                 catálogo de etiquetas · F19
    │   │   ├── sales/                  registro de ventas · F14
    │   │   ├── reminders/              recordatorios · F28
    │   │   ├── operators/              usuarios y roles · F18
    │   │   ├── quick_replies/          respuestas rápidas · F09
    │   │   │                           (los cinco: controller · service · repository · model)
    │   │   │
    │   │   ├── reporting/              SÓLO LECTURA · F16, F32, F34–F38
    │   │   │   ├── controller.go
    │   │   │   ├── service.go
    │   │   │   └── queries.go          no tiene repository: no escribe nada
    │   │   │
    │   │   ├── brainclient/            puerto de salida hacia el cerebro
    │   │   │   ├── client.go           agrega X-Internal-Token
    │   │   │   └── contract.go
    │   │   │
    │   │   └── platform/               lo transversal · única excepción declarada
    │   │       ├── http/               router · middleware · auth · errors
    │   │       ├── httpclient/         cliente HTTP con timeouts y reintentos
    │   │       ├── storage/            PUERTO · base · supabase · disk
    │   │       ├── db/                 db.go (pool) · executor.go (pool | pgx.Tx)
    │   │       ├── id/                 uuid v7
    │   │       ├── config/             se lee del entorno y se valida al arrancar
    │   │       └── logging/
    │   │
    │   ├── migrations/                 DUEÑO ÚNICO del esquema
    │   │   ├── README.md
    │   │   ├── 0001_foundations.sql    incluye la fila única de accounts
    │   │   ├── 0002_agents.sql
    │   │   ├── 0003_conversations.sql
    │   │   └── 0004_turn_evidence.sql
    │   │
    │   └── seeds/                      demo · idempotentes · nunca en producción
    │
    ├── brain/                          ── DESPLEGABLE 2 · Python · FastAPI ──
    │   ├── pyproject.toml              Poetry · absorbe la config de pytest
    │   ├── poetry.lock                 se commitea
    │   ├── main.py                     un entrypoint, flag api|worker
    │   │
    │   ├── api/                        el contrato interno que consume el núcleo
    │   │   ├── routes/                 turns · prompts · ingest · health
    │   │   └── schemas/                Pydantic = el contrato
    │   │
    │   ├── pipeline/                   un módulo por valor de PipelineStep
    │   │   ├── runner.py               ejecuta los pasos y escribe el rastro
    │   │   ├── step.py                 contrato común de un paso
    │   │   ├── blocklist.py
    │   │   ├── guardrail.py
    │   │   ├── triage.py
    │   │   ├── retrieval.py
    │   │   ├── grounding.py
    │   │   └── generation.py           lee quick_replies para F07
    │   │                               reranking.py no se crea hasta que exista
    │   │
    │   ├── prompts/                    F03, F04 · ÚNICA implementación del ensamblador
    │   │   ├── assembler.py
    │   │   ├── rewriter.py
    │   │   ├── repository.py
    │   │   └── budget.py
    │   │
    │   ├── classifiers/                F11, F12, F31
    │   │   ├── frustration.py
    │   │   ├── intent.py
    │   │   ├── transfer.py
    │   │   └── extraction.py           devuelve; el núcleo persiste
    │   │
    │   ├── knowledge/                  F06 · chunking · embeddings · search
    │   ├── worker/                     F06 · ingesta · segundo entrypoint, mismo código
    │   │
    │   ├── adapters/                   los cuatro puertos, menos el canal
    │   │   ├── llm/                    base (puerto) · registry (fábrica) · anthropic
    │   │   ├── vectorstore/            base · qdrant
    │   │   └── storage/                lee los bytes que subió el núcleo
    │   │
    │   ├── rules/                      datos versionados, no código
    │   │   ├── blocklist.yaml   ·   guardrails.yaml
    │   │   ├── triage.yaml      ·   grounding.yaml
    │   │   ├── rewriter.yaml
    │   │   └── frustration.yaml · intent.yaml · transfer.yaml · extraction.yaml
    │   │
    │   ├── infra/                      config · db · logging · ids · rules
    │   └── tests/                      unitarios · conftest.py · factories.py
    │
    ├── frontend/                       ── DESPLEGABLE 3 · A DEFINIR (8.4) ──
    │
    └── test/
        ├── karate/                     API end-to-end · nombrado en 8.4
        ├── evals/                      OFFLINE, no se despliega · OE2–OE4, OE6, OE7
        └── datasets/                   200 conversaciones (OE3) · 500 (OE4)
```

Los tests de Go **no** tienen carpeta: van pegados al código como `*_test.go`.

---

## 2. Dueño de cada tabla

La forma operativa de la regla 1. Las **37 entidades** del catálogo, con el paquete que las escribe. Un paquete que no aparece acá no escribe nada.

### Núcleo — Go

| Paquete | Tablas de las que es dueño |
|---|---|
| `conversations/` | `conversations` · `messages` |
| `contacts/` | `contacts` · `contact_labels` · `contact_attributes` |
| `agents/` | `ai_agents` · `prompt_components` · `prompt_component_versions` · `transfer_policies` |
| `turn/` | `message_batches` · `transfer_events` |
| `inbox/` | `conversation_assignments` · `conversation_labels` · `internal_notes` |
| `whatsapp/` | `webhook_events` · `inboxes` |
| `documents/` | `knowledge_bases` · `documents` |
| `meta_templates/` | `meta_templates` |
| `labels/` | `labels` |
| `operators/` | `operators` |
| `sales/` | `sales` |
| `reminders/` | `reminders` |
| `quick_replies/` | `quick_replies` |
| `platform/http/` | `audit_logs` — transversal, lo escribe un middleware |
| `reporting/` | **ninguna** |
| `migrations/` | `accounts` — fila única, es un invariante del esquema y no lo escribe ningún paquete |

### Cerebro — Python

| Paquete | Tablas de las que es dueño |
|---|---|
| `pipeline/` | `pipeline_steps` · `generations` · `generation_components` |
| `classifiers/` | `detected_intents` · `signal_evaluations` |
| `prompts/` | `conversation_contexts` · `conversation_context_versions` |
| `knowledge/` | `retrieval_logs` |
| `worker/` | `document_chunks` |

### Instrumental offline — `src/test/evals/`

| Paquete | Tablas de las que es dueño |
|---|---|
| `evals/` | `conversation_annotations` · `tone_evaluations` |

**Su único escritor es un proceso que no se despliega**, y eso es deliberado: garantiza que el sistema evaluado no pueda escribir su propia nota.

### Las dos excepciones

1. **`generations` la escriben los dos servicios.** El cerebro la inserta; el núcleo completa `message_id` y el desenlace de la revisión de F02. La regla que lo ordena: *el cerebro es dueño de la fila hasta que responde; desde ahí es del núcleo.*
2. **`conversations` y `messages` las escriben tres paquetes** —`turn/`, `inbox/` y `whatsapp/`— pero **ninguno con SQL propio**: todos pasan por el repositorio de `conversations/`, dentro de la transacción que abre quien conduce.

---

## 3. Estado de avance

15 de las 37 tablas y 3 de los 16 paquetes del núcleo.

| | Existe hoy |
|---|---|
| **Migraciones** | `0001_foundations` · `0002_agents` · `0003_conversations` · `0004_turn_evidence` — 15 tablas |
| **Paquetes del núcleo** | `agents/` (completo, con tests) · `inbox/` (sólo lectura: la vista de F05) · `operators/` (sólo repositorio) · `brainclient/` · `platform/` sin `httpclient/` ni `storage/` |
| **Paquetes del cerebro** | `api/` (sólo la ruta de prompts) · `prompts/` (completo) · `adapters/llm/` (sólo `anthropic`) · `infra/` · `tests/` |
| **Sin empezar** | `deploy/` · `contracts/brain.openapi.json` · `Makefile` · `ci.yml` · `seeds/` · `frontend/` · `test/` |
