# SONIA — Estructura del código

| Campo | Valor |
|---|---|
| **Qué es** | El árbol de archivos completo, con el propósito de cada paquete y **de qué tablas es dueño** |
| **Autoridad** | Detalla [`08-structure-rules.md`](design/08-structure-rules.md) (las 12 reglas) y el resumen de *Estructura del código* en [`CLAUDE.md`](../CLAUDE.md), que son la fuente. **Detalla a [ADR-0017](design/02-decisiones/0017-decomposicion-en-despliegues-y-lenguajes.md) §6.1**, cuyo árbol se actualizó al firmarse el ADR el 2026-09-05 |
| **Qué no es** | No contiene decisiones ni su justificación. El *por qué* de cada corte está en ADR-0017; las convenciones de nombres, en [`00-convenciones.md`](design/00-convenciones.md); el estado de avance del diseño, en [`ESTADO.md`](../z_helps/ESTADO.md) |
| **Orden** | **Las carpetas están en orden alfabético en todos los niveles.** Las listas de archivos conservan su orden cuando ese orden significa algo: los pasos de `pipeline/` van en el orden en que se ejecutan (ADR-0016), las migraciones por número, y los archivos de un paquete en el orden del pedido (`controller` → `routes` → `service` → `repository` → `model`) |
| **Última revisión** | 2026-09-19 |

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
├── .mcp.json                           MCP de Claude Code: Atlassian (OAuth por persona) · PostgreSQL vía `make dbhub`
├── CLAUDE.md
├── LICENSE
├── Makefile                            up · migrate · seed · seed-admin · connect-inbox · test · test-integration · smoke
├── README.md
│
├── contracts/                          fuente única compartida por los dos servicios
│   ├── brain.openapi.json              contrato del cerebro, generado por FastAPI
│   ├── enums.yaml                      las 22 enumeraciones · el esquema las aplica con CHECK
│   └── generate.py                     las baja a Go y a Python · nadie edita lo generado
│
├── deploy/                             context: .. para que los builds vean contracts/
│   ├── brain.Dockerfile
│   ├── core.Dockerfile
│   ├── docker-compose.yml              el puerto del cerebro NO se publica al host
│   ├── docker-compose.smoke.yml        los dos dobles · sólo lo aplica `make smoke`
│   └── frontend.Dockerfile
│
├── docs/
│   ├── SONIA_Anteproyecto.pdf          fuente de verdad de requisitos y objetivos
│   ├── design/
│   │   ├── 00-convenciones.md
│   │   ├── 01-trazabilidad.md          F01–F39 y OE1–OE7 con sus hallazgos
│   │   ├── 02-decisiones/              los ADR, uno por archivo · README.md es el índice
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
│   │   │   ├── llm/                    base (puerto) · registry (fábrica) · cohere · anthropic · openai · double
│   │   │   ├── storage/                lee los bytes que subió el núcleo
│   │   │   └── vectorstore/            base · qdrant
│   │   │
│   │   ├── api/                        el contrato interno que consume el núcleo
│   │   │   ├── routes/                 turns · prompts · models · ingest · health
│   │   │   └── schemas/                Pydantic = el contrato
│   │   │
│   │   ├── classifiers/                F11, F12, F31
│   │   │   ├── extraction.py           devuelve; el núcleo persiste
│   │   │   ├── frustration.py          no se crea hasta que exista el modelo (ADR-0044)
│   │   │   ├── intent.py
│   │   │   └── transfer.py
│   │   │
│   │   ├── infra/                      config · db · enums (GENERADO) · ids · logging · rules
│   │   ├── knowledge/                  F06 · chunking · embeddings · search
│   │   │
│   │   ├── pipeline/                   un módulo por valor de PipelineStep
│   │   │   ├── runner.py               ejecuta los pasos y escribe el rastro
│   │   │   ├── step.py                 contrato común de un paso
│   │   │   ├── repository.py           las lecturas del turno: bloque · conversación · mensajes · agente IA · escribe la generación con lo que ensambló (ADR-0039, regla 10)
│   │   │   ├── notices.py              el texto fijo de cada corte (O2) · no es un paso
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
│   │   │   ├── repository.py           los componentes activos con su versión más nueva
│   │   │   └── rewriter.py
│   │   │
│   │   ├── rules/                      datos versionados, no código
│   │   │   ├── blocklist.yaml   ·   profanity.yaml   ·   notices.yaml
│   │   │   ├── guardrails.yaml  ·   triage.yaml   ·   grounding.yaml
│   │   │   ├── generation.yaml  ·   pricing.yaml  ·   rewriter.yaml
│   │   │   └── extraction.yaml · frustration.yaml · intent.yaml
│   │   │                               frustration.yaml no se crea hasta que exista el modelo (ADR-0044)
│   │   │                               los umbrales por defecto de F12 viven en triage.yaml (ADR-0045)
│   │   │
│   │   ├── tests/                      unitarios · conftest.py · factories.py
│   │   └── worker/                     F06 · ingesta · segundo entrypoint, mismo código
│   │
│   ├── core/                           ── DESPLEGABLE 1 · Go · gin ──
│   │   ├── go.mod   ·   go.sum
│   │   ├── main.go                     sólo main(): la señal y el código de salida
│   │   │
│   │   ├── internal/
│   │   │   │
│   │   │   ├── agents/                 identidad del agente IA · F01, F02, F17
│   │   │   │   ├── controller.go       lee el cuerpo por claves: null limpia, y rechaza por nombre lo que no expone (ADR-0038, regla 7)
│   │   │   │   ├── components_controller.go  los seis bloques de F01, bajo el agente IA · tipo y posición no se editan · el historial pagina por número (ADR-0039)
│   │   │   │   ├── preview_controller.go  POST /ai-agents/:id/preview: los borradores viajan en el cuerpo (ADR-0040)
│   │   │   │   ├── routes.go           sólo admin, también la lectura (F17, ADR-0038) · las de componentes y la previsualización también (ADR-0039, ADR-0040)
│   │   │   │   ├── service.go          valida el modelo contra el cerebro antes de la transacción · la baja vacía el default de las bandejas · la pausa y la baja apagan el bot de sus conversaciones
│   │   │   │   ├── components.go       un componente por tipo · cada cambio del texto o del interruptor es una versión (ADR-0039)
│   │   │   │   ├── preview.go          valida los borradores como un guardado y le pide el system al cerebro · no escribe nada (F03, ADR-0040)
│   │   │   │   ├── repository.go       ai_agents, audit_logs, prompt_components y sus versiones · la baja es deleted_at y active es la pausa · audita el default de una bandeja para whatsapp/
│   │   │   │   └── model.go            Config es lo que se edita y lo que la auditoría compara
│   │   │   │
│   │   │   ├── app/                    el arranque · excepción declarada, sin tablas
│   │   │   │   ├── app.go              Run(ctx): config, pool, router, apagado ordenado
│   │   │   │   ├── connect.go          --connect-inbox: cifra los secretos de Meta de una bandeja que ya existe (ADR-0033)
│   │   │   │   ├── deps.go             el grafo de objetos · lo único que crece por contexto
│   │   │   │   └── seed.go             --seed-admin: la cuenta y su primer admin · nunca al arrancar (regla 3)
│   │   │   │
│   │   │   ├── brainclient/            puerto de salida hacia el cerebro
│   │   │   │   ├── client.go           agrega X-Internal-Token
│   │   │   │   └── contract.go         el turno · los modelos con tarifa · la previsualización (ADR-0040)
│   │   │   │
│   │   │   ├── contacts/               contactos y segmentación · F15, F30, F33
│   │   │   │   └── controller · routes · service · repository · model · attributes
│   │   │   │                           attributes: guarda lo que extrae F31 y lo muestra en la ficha (ADR-0047)
│   │   │   │
│   │   │   ├── conversations/          DUEÑO del agregado compartido
│   │   │   │   ├── service.go          turn/, inbox/, whatsapp/ y agents/ escriben por acá · nace en off si el agente IA no puede responder (ADR-0038) · cada mensaje del cliente suma a unread_count y la respuesta de un operador lo borra (ADR-0052)
│   │   │   │   ├── repository.go       recibe el Executor de quien conduce
│   │   │   │   ├── scope.go            qué contactos alcanza un operator (ADR-0032, regla 2) · la condición la escribe el dueño de la tabla
│   │   │   │   └── model.go
│   │   │   │
│   │   │   ├── documents/              bases de conocimiento y carga · F06 (ABM)
│   │   │   │   ├── controller.go       recibe el upload de hasta 100 MB
│   │   │   │   ├── routes.go
│   │   │   │   ├── service.go          bytes a platform/storage, fila en pending
│   │   │   │   ├── repository.go
│   │   │   │   └── model.go
│   │   │   │
│   │   │   ├── inbox/                  bandeja y tiempo real · F05, F13, F21–F27
│   │   │   │   ├── controller.go       DTO, cursor opaco y el tope de limit (ADR-0036)
│   │   │   │   ├── routes.go           los tres roles en todas: lo que cada uno alcanza lo decide service.go
│   │   │   │   ├── dispatch.go         el vendedor de cada conversación, en la transacción del webhook (ADR-0037)
│   │   │   │   ├── reasoning.go        F05: los componentes de cada respuesta, con el alcance del hilo · el texto, sólo para el admin (ADR-0049)
│   │   │   │   ├── reasoning_controller.go  GET /conversations/:id/generations/:generation_id/components
│   │   │   │   ├── websocket.go        F21: el hub en memoria (ADR-0027) · tickets de un uso · cada evento con el alcance del listado · nunca bloquea a quien publica (ADR-0051)
│   │   │   │   ├── stream.go           POST /inbox/stream-ticket detrás del bearer · GET /inbox/stream, pública: el ticket es la credencial (ADR-0051)
│   │   │   │   ├── service.go          alcance por rol · toma el pool en la transacción del envío · envía una sola vez (ADR-0020) · el modo, el operator sólo de lo suyo (ADR-0037), y prenderlo exige un agente IA que pueda responder (ADR-0038) · la estrella de F25, con el alcance del hilo y sin mover la bandeja (ADR-0053) · la marca de leída, con el alcance de responder (ADR-0052) · el cierre manual, con el alcance del interruptor y rechazado con un turno en vuelo (ADR-0054)
│   │   │   │   ├── repository.go       la bandeja con su último mensaje, sus fallidos y si su agente IA puede responder · los filtros de F23, siempre después del alcance (ADR-0050) · escribe conversation_assignments
│   │   │   │   └── model.go
│   │   │   │
│   │   │   ├── labels/                 catálogo de etiquetas · F19 · DUEÑO de labels
│   │   │   │   ├── controller.go       DTO, paginación por número de página y el tope de per_page
│   │   │   │   ├── routes.go           leer: los tres roles · escribir: sólo admin (F19, ADR-0032)
│   │   │   │   ├── service.go          genera el id (C04) y rechaza el nombre en blanco
│   │   │   │   ├── repository.go       el índice único es sobre lower(name): Ventas y ventas son una
│   │   │   │   └── model.go            Label y Page · las dos tablas de unión no son de acá
│   │   │   │
│   │   │   ├── meta_templates/         plantillas de Meta · F08, F37
│   │   │   │
│   │   │   ├── operators/              usuarios y roles · F18 · DUEÑO de operators
│   │   │   │   ├── controller.go       login (público) y /me (autenticado): dos tipos, dos zonas
│   │   │   │   ├── routes.go           POST /auth/login fuera de /api/v1: quien se loguea no es actor
│   │   │   │   ├── service.go          una sola respuesta para las tres formas de fallar (ADR-0030)
│   │   │   │   ├── repository.go       normaliza el email: el índice único es sobre la columna cruda
│   │   │   │   ├── model.go            Operator lleva el hash, así que nunca se serializa
│   │   │   │   └── password.go         bcrypt cost 12 · gasta una comparación si el email no existe
│   │   │   │
│   │   │   ├── platform/               lo transversal · la otra excepción declarada
│   │   │   │   ├── auth/               emite y verifica el token de sesión · sin gin y sin base (ADR-0030)
│   │   │   │   ├── encryption/         AES-256-GCM sobre los cuatro campos sensibles · la llave se inyecta (ADR-0031)
│   │   │   │   ├── config/             se lee del entorno y se valida al arrancar
│   │   │   │   ├── db/                 db.go (pool) · executor.go (pool | pgx.Tx)
│   │   │   │   ├── enums/              GENERADO desde contracts/enums.yaml
│   │   │   │   ├── http/               router · route · middleware · errors
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
│   │   │   │   ├── collector.go        agrupa mensajes fragmentados · F10 (ADR-0041)
│   │   │   │   ├── poller.go           cierra los bloques vencidos y arranca el turno · F10 (ADR-0041)
│   │   │   │   ├── orchestrator.go     conduce: cerebro → persistir → enviar
│   │   │   │   ├── retry.go            reintentos configurables por entorno
│   │   │   │   ├── repository.go       sus tablas · más generations.message_id
│   │   │   │   └── model.go
│   │   │   │
│   │   │   └── whatsapp/               canal · F20, F29
│   │   │       ├── controller.go       entrada del webhook · lee la firma y responde a Meta antes del turno
│   │   │       ├── settings_controller.go  el panel de los números, bajo /api/v1 · sin ningún secreto en la respuesta (ADR-0038)
│   │   │       ├── routes.go           GET y POST /webhooks/whatsapp, fuera de /api/v1: Meta no es un operador · GET y PATCH /inboxes, sólo admin
│   │   │       ├── service.go          valida la firma HMAC-SHA256 con el app secret de la bandeja · relee el agente IA por defecto dentro de la transacción (ADR-0038)
│   │   │       ├── settings.go         qué agente IA atiende cada número · audita por el repositorio de agents/ (ADR-0038)
│   │   │       ├── repository.go       descifra los dos secretos de inboxes (ADR-0031) · vacía el default de un agente IA dado de baja (ADR-0038)
│   │   │       ├── model.go
│   │   │       ├── payload.go          la forma que manda Meta, reducida a lo que se lee
│   │   │       └── client.go           envío a Meta
│   │   │
│   │   ├── migrations/                 DUEÑO ÚNICO del esquema
│   │   │   ├── README.md
│   │   │   ├── embed.go                el go:embed que el runner de platform/db lee
│   │   │   └── 0001_initial_schema.sql las 37 tablas · incluye la fila única de accounts
│   │   │
│   │   ├── seeds/                      demo · idempotentes · nunca en producción
│   │   │   └── 0001_demo.sql           cuelga de la cuenta que haya: no asume que ella la crea
│   │   │
│   │   └── smoke/                      el camino entero desde afuera · el arnés de regresión de la Fase 1
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

---

## 2. El dueño de cada tabla

Las 37 tablas de `0001_initial_schema.sql`, agrupadas por el paquete que las escribe. Es la regla 1 de `CLAUDE.md` puesta en una lista: **escribir en la tabla de otro se hace por su repositorio, dentro de la transacción de quien conduce**, nunca con SQL propio.

### 2.1. Núcleo (Go) — 25 tablas

| Paquete | Tablas | Nota |
|---|---|---|
| `agents/` | `ai_agents` · `prompt_components` · `prompt_component_versions` · `audit_logs` | `audit_logs` está acotada a cambios de configuración del agente IA, con qué agente IA atiende cada número y sin los componentes del prompt (ADR-0007, ADR-0038, ADR-0039) |
| `contacts/` | `contacts` · `contact_attributes` · `contact_labels` | `contact_attributes` la infiere el cerebro y **la persiste el núcleo** (F31) |
| `conversations/` | `conversations` · `messages` | El **agregado compartido**: `turn/`, `inbox/`, `whatsapp/` y `agents/` escriben por acá |
| `documents/` | `knowledge_bases` · `documents` | Los *chunks* no: los escribe el cerebro |
| `inbox/` | `conversation_assignments` · `internal_notes` · `conversation_labels` | |
| `labels/` | `labels` | El catálogo; las dos tablas de unión son de quien etiqueta |
| `meta_templates/` | `meta_templates` | |
| `operators/` | `operators` | |
| `quick_replies/` | `quick_replies` | La lee el cerebro (F07), no la escribe |
| `reminders/` | `reminders` | |
| `sales/` | `sales` | |
| `turn/` | `message_batches` · `transfer_events` · `conversation_contexts` | Más `generations.message_id`, en la misma transacción (regla 5). `conversation_contexts` es la copia de la versión que usó la generación (ADR-0048) |
| `whatsapp/` | `inboxes` · `webhook_events` | |

### 2.2. Cerebro (Python) — 8 tablas, todas de evidencia

La regla 3 lo dice al revés y es lo que importa: **el cerebro no escribe `conversations`, `messages`, `contact_attributes` ni `conversation_contexts`.** Lo que no es evidencia lo devuelve, y el núcleo lo persiste y lo envía.

| Tablas | Nota |
|---|---|
| `pipeline_steps` · `generations` · `retrieval_logs` · `signal_evaluations` · `detected_intents` · `document_chunks` · `conversation_context_versions` | Las siete que nombra la regla 3 (la última, desde ADR-0048) |
| `generation_components` | Por adyacencia a `generations`; la regla 3 no la nombra |

### 2.3. Fuera de los dos despliegues — 2 tablas

| Tabla | Quién la escribe | Nota |
|---|---|---|
| `conversation_annotations` | `test/datasets/` | *Ground truth* de OE3 y OE4 (ADR-0014) |
| `tone_evaluations` | `test/evals/` | El juicio de tono de F36; `reporting/` la lee |

### 2.4. Sin dueño declarado — 2 tablas

No es un olvido de este documento: **son decisiones que todavía no se tomaron**, y ponerles un paquete acá sería inventarlas.

| Tabla | Por qué sigue abierta |
|---|---|
| `accounts` | Fila única por invariante. Hoy la crea quien instala —`make seed-admin`— y ningún paquete la reclama |
| `transfer_policies` | El umbral es configuración mutable y F12 es del cerebro, pero la regla 3 le prohíbe escribir lo que no es evidencia |
