# SONIA — Estructura del código

| Campo | Valor |
|---|---|
| **Qué es** | El árbol de archivos completo, con el propósito de cada paquete y **de qué tablas es dueño** |
| **Autoridad** | Detalla la sección *Estructura del código* de [`CLAUDE.md`](../../CLAUDE.md), que es la fuente. **Enmienda a [ADR-0017](02-decisiones/0017-decomposicion-en-despliegues-y-lenguajes.md) §6.1** — el ADR sigue en Propuesta y su árbol es el anterior |
| **Qué no es** | No contiene decisiones ni su justificación. El *por qué* de cada corte está en ADR-0017; las convenciones de nombres, en [`00-convenciones.md`](00-convenciones.md); el estado de avance del diseño, en [`ESTADO.md`](ESTADO.md) |
| **Orden** | **Las carpetas están en orden alfabético en todos los niveles.** Las listas de archivos conservan su orden cuando ese orden significa algo: los pasos de `pipeline/` van en el orden en que se ejecutan (ADR-0016), las migraciones por número, y los archivos de un paquete en el orden del pedido (`controller` → `service` → `repository` → `model`) |
| **Última revisión** | 2026-09-03 |

---

## 1. El árbol

```
sonia/
├── .dockerignore                       en la RAÍZ: es la raíz del contexto de build
├── .env.example                        plantilla; .env está en .gitignore
├── .github/
│   └── workflows/
│       └── ci.yml
├── .gitignore                          go.mod, go.sum y contracts/ SÍ se commitean
├── CLAUDE.md
├── LICENSE
├── Makefile                            db-migrate · db-seed · db-reset · test
├── README.md
│
├── contracts/                          fuente única compartida por los dos servicios
│   ├── brain.openapi.json              contrato del cerebro, generado por FastAPI
│   └── enums.yaml                      las 22 enumeraciones · el esquema las aplica con CHECK
│
├── deploy/                             context: .. para que los builds vean contracts/
│   ├── brain.Dockerfile
│   ├── core.Dockerfile
│   ├── docker-compose.yml              el puerto del cerebro NO se publica al host
│   └── frontend.Dockerfile
│
├── docs/
│   ├── SONIA_Anteproyecto.pdf          fuente de verdad de requisitos y objetivos
│   ├── design/
│   │   ├── 00-convenciones.md
│   │   ├── 01-trazabilidad.md          F01–F39 y OE1–OE7 con sus hallazgos
│   │   ├── 02-decisiones/              los ADR · README.md + 0001 a 0017
│   │   ├── 03-capa-prompts.md
│   │   ├── 05-consultas-de-metricas.sql
│   │   ├── 07-secuencia-del-turno.md
│   │   ├── DER/                        entidades.md · relaciones.md · guia-de-dibujo.md
│   │   ├── ESTADO.md
│   │   └── struct.md                   este documento
│   └── diagrams/
│
├── src/
│   │
│   ├── brain/                          ── DESPLEGABLE 2 · Python · FastAPI ──
│   │   ├── main.py                     un entrypoint, flag api|worker
│   │   ├── poetry.lock                 se commitea
│   │   ├── pyproject.toml              Poetry · absorbe la config de pytest
│   │   │
│   │   ├── adapters/                   los cuatro puertos, menos el canal
│   │   │   ├── llm/                    base (puerto) · registry (fábrica) · anthropic
│   │   │   ├── storage/                lee los bytes que subió el núcleo
│   │   │   └── vectorstore/            base · qdrant
│   │   │
│   │   ├── api/                        el contrato interno que consume el núcleo
│   │   │   ├── routes/                 turns · prompts · ingest · health
│   │   │   └── schemas/                Pydantic = el contrato
│   │   │
│   │   ├── classifiers/                F11, F12, F31
│   │   │   ├── extraction.py           devuelve; el núcleo persiste
│   │   │   ├── frustration.py
│   │   │   ├── intent.py
│   │   │   └── transfer.py
│   │   │
│   │   ├── infra/                      config · db · ids · logging · rules
│   │   ├── knowledge/                  F06 · chunking · embeddings · search
│   │   │
│   │   ├── pipeline/                   un módulo por valor de PipelineStep
│   │   │   ├── runner.py               ejecuta los pasos y escribe el rastro
│   │   │   ├── step.py                 contrato común de un paso
│   │   │   ├── blocklist.py            ┐
│   │   │   ├── guardrail.py            │
│   │   │   ├── triage.py               ├ en el orden en que se ejecutan
│   │   │   ├── retrieval.py            │  (ADR-0016)
│   │   │   ├── grounding.py            │
│   │   │   └── generation.py           ┘ lee quick_replies para F07
│   │   │                               reranking.py no se crea hasta que exista
│   │   │
│   │   ├── prompts/                    F03, F04 · ÚNICA implementación del ensamblador
│   │   │   ├── assembler.py
│   │   │   ├── budget.py
│   │   │   ├── repository.py
│   │   │   └── rewriter.py
│   │   │
│   │   ├── rules/                      datos versionados, no código
│   │   │   ├── blocklist.yaml   ·   grounding.yaml
│   │   │   ├── guardrails.yaml  ·   triage.yaml
│   │   │   ├── rewriter.yaml
│   │   │   └── extraction.yaml · frustration.yaml · intent.yaml · transfer.yaml
│   │   │
│   │   ├── tests/                      unitarios · conftest.py · factories.py
│   │   └── worker/                     F06 · ingesta · segundo entrypoint, mismo código
│   │
│   ├── core/                           ── DESPLEGABLE 1 · Go · gin ──
│   │   ├── go.mod   ·   go.sum
│   │   ├── main.go                     carga y valida la config, arma e inyecta todo
│   │   │
│   │   ├── internal/
│   │   │   │
│   │   │   ├── agents/                 identidad del agente IA · F01, F02, F17
│   │   │   │   └── controller · service · repository · model
│   │   │   │
│   │   │   ├── brainclient/            puerto de salida hacia el cerebro
│   │   │   │   ├── client.go           agrega X-Internal-Token
│   │   │   │   └── contract.go
│   │   │   │
│   │   │   ├── contacts/               contactos y segmentación · F15, F30, F33
│   │   │   │   └── controller · service · repository · model
│   │   │   │                           persiste también los atributos de F31
│   │   │   │
│   │   │   ├── conversations/          DUEÑO del agregado compartido
│   │   │   │   ├── service.go          turn/, inbox/ y whatsapp/ escriben por acá
│   │   │   │   ├── repository.go       recibe el Executor de quien conduce
│   │   │   │   └── model.go
│   │   │   │
│   │   │   ├── documents/              bases de conocimiento y carga · F06 (ABM)
│   │   │   │   ├── controller.go       recibe el upload de hasta 100 MB
│   │   │   │   ├── service.go          bytes a platform/storage, fila en pending
│   │   │   │   ├── repository.go
│   │   │   │   └── model.go
│   │   │   │
│   │   │   ├── inbox/                  bandeja y tiempo real · F05, F13, F21–F27
│   │   │   │   ├── controller.go
│   │   │   │   ├── websocket.go        fan-out a los operadores conectados
│   │   │   │   ├── service.go
│   │   │   │   ├── repository.go
│   │   │   │   └── model.go
│   │   │   │
│   │   │   ├── labels/                 catálogo de etiquetas · F19
│   │   │   ├── meta_templates/         plantillas de Meta · F08, F37
│   │   │   ├── operators/              usuarios y roles · F18
│   │   │   │
│   │   │   ├── platform/               lo transversal · única excepción declarada
│   │   │   │   ├── config/             se lee del entorno y se valida al arrancar
│   │   │   │   ├── db/                 db.go (pool) · executor.go (pool | pgx.Tx)
│   │   │   │   ├── http/               router · middleware · auth · errors
│   │   │   │   ├── httpclient/         cliente HTTP con timeouts y reintentos
│   │   │   │   ├── id/                 uuid v7
│   │   │   │   ├── logging/
│   │   │   │   └── storage/            PUERTO · base · supabase · disk
│   │   │   │
│   │   │   ├── quick_replies/          respuestas rápidas · F09
│   │   │   ├── reminders/              recordatorios · F28
│   │   │   │
│   │   │   ├── reporting/              SÓLO LECTURA · F16, F32, F34–F38
│   │   │   │   ├── controller.go
│   │   │   │   ├── service.go
│   │   │   │   └── queries.go          no tiene repository: no escribe nada
│   │   │   │
│   │   │   ├── sales/                  registro de ventas · F14
│   │   │   │
│   │   │   ├── turn/                   orquestador del turno · ABRE LA TRANSACCIÓN
│   │   │   │   ├── timer.go            agrupa mensajes fragmentados · F10
│   │   │   │   ├── orchestrator.go     conduce: cerebro → persistir → enviar
│   │   │   │   ├── retry.go            reintentos configurables por entorno
│   │   │   │   ├── repository.go       sus tablas · más generations.message_id
│   │   │   │   └── model.go
│   │   │   │
│   │   │   └── whatsapp/               canal · F20, F29
│   │   │       ├── webhook.go          entrada · valida la firma HMAC-SHA256
│   │   │       ├── client.go           envío a Meta
│   │   │       ├── service.go
│   │   │       ├── repository.go
│   │   │       └── model.go
│   │   │
│   │   ├── migrations/                 DUEÑO ÚNICO del esquema
│   │   │   ├── README.md
│   │   │   ├── 0001_foundations.sql    incluye la fila única de accounts
│   │   │   ├── 0002_agents.sql
│   │   │   ├── 0003_conversations.sql
│   │   │   └── 0004_turn_evidence.sql
│   │   │
│   │   └── seeds/                      demo · idempotentes · nunca en producción
│   │
│   ├── frontend/                       ── DESPLEGABLE 3 · A DEFINIR (8.4) ──
│   │
│   └── test/
│       ├── datasets/                   200 conversaciones (OE3) · 500 (OE4)
│       ├── evals/                      OFFLINE, no se despliega · OE2–OE4, OE6, OE7
│       └── karate/                     API end-to-end · nombrado en 8.4
│
└── z_helps/                            material de trabajo · sin peso oficial · ignorado
```

Los seis paquetes de una línea —`labels/`, `meta_templates/`, `operators/`, `quick_replies/`, `reminders/`, `sales/`— tienen los mismos cuatro archivos: `controller.go`, `service.go`, `repository.go` y `model.go`.

Los tests de Go **no** tienen carpeta: van pegados al código como `*_test.go`.

---

## 2. Dueño de cada tabla

La forma operativa de la regla 1. Las **37 entidades** del catálogo, con el paquete que las escribe, en orden alfabético. Un paquete que no aparece acá no escribe nada.

### Núcleo — Go

| Paquete | Tablas de las que es dueño |
|---|---|
| `agents/` | `ai_agents` · `prompt_components` · `prompt_component_versions` · `transfer_policies` |
| `contacts/` | `contacts` · `contact_attributes` · `contact_labels` |
| `conversations/` | `conversations` · `messages` |
| `documents/` | `documents` · `knowledge_bases` |
| `inbox/` | `conversation_assignments` · `conversation_labels` · `internal_notes` |
| `labels/` | `labels` |
| `meta_templates/` | `meta_templates` |
| `migrations/` | `accounts` — fila única, es un invariante del esquema y no lo escribe ningún paquete |
| `operators/` | `operators` |
| `platform/http/` | `audit_logs` — transversal, lo escribe un middleware |
| `quick_replies/` | `quick_replies` |
| `reminders/` | `reminders` |
| `reporting/` | **ninguna** |
| `sales/` | `sales` |
| `turn/` | `message_batches` · `transfer_events` |
| `whatsapp/` | `inboxes` · `webhook_events` |

### Cerebro — Python

| Paquete | Tablas de las que es dueño |
|---|---|
| `classifiers/` | `detected_intents` · `signal_evaluations` |
| `knowledge/` | `retrieval_logs` |
| `pipeline/` | `generations` · `generation_components` · `pipeline_steps` |
| `prompts/` | `conversation_contexts` · `conversation_context_versions` |
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

15 de las 37 tablas y 3 de los 16 paquetes del núcleo. Verificado el 2026-09-03: **Go compila y `go vet` está limpio; 44 tests del cerebro pasan, con 67 % de cobertura.** Ninguna línea de SQL se ejecutó todavía — las migraciones no se aplicaron a ningún Postgres, y por eso `agents/` mide 12,1 % y `prompts/repository.py` 37 %.

| | Existe hoy |
|---|---|
| **Migraciones** | `0001_foundations` · `0002_agents` · `0003_conversations` · `0004_turn_evidence` — 15 tablas |
| **Paquetes del núcleo** | `agents/` (completo, con tests) · `brainclient/` · `inbox/` (sólo lectura: la vista de F05) · `operators/` (sólo repositorio) · `platform/` sin `httpclient/` ni `storage/` |
| **Paquetes del cerebro** | `adapters/llm/` (sólo `anthropic`) · `api/` (sólo la ruta de prompts) · `infra/` · `prompts/` (completo) · `tests/` |
| **Sin empezar** | `Makefile` · `ci.yml` · `contracts/brain.openapi.json` · `deploy/` · `frontend/` · `seeds/` · `test/` |
