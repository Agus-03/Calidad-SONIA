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
│   ├── fixtures/                       document-cipher.json: el vector cifrado que abren Go y Python (ADR-0064 §4)
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
│   │   │   ├── llm/                    base (puerto, con imágenes desde ADR-0067) · registry (fábrica) · cohere · anthropic · openai · double
│   │   │   ├── embeddings/             base · voyage (voyage-4, 1024) · double · registry · un proveedor, no un puerto (ADR-0067, ADR-0071)
│   │   │   ├── storage/                firebase: lee los bytes que subió el núcleo, con google-cloud-storage (ADR-0071)
│   │   │   └── vectorstore/            qdrant: la API REST con httpx · una colección por agente, coseno (ADR-0010, ADR-0071)
│   │   │
│   │   ├── api/                        el contrato interno que consume el núcleo
│   │   │   ├── routes/                 turns/ · prompts · models · ingest · health
│   │   │   │   └── turns/              route (POST /turns) · signals (F12) · enrichment (F04, F11, F31: nunca cortan el turno)
│   │   │   └── schemas/                Pydantic = el contrato
│   │   │
│   │   ├── classifiers/                F11, F12, F31
│   │   │   ├── extraction.py           devuelve; el núcleo persiste
│   │   │   ├── frustration.py          no se crea hasta que exista el modelo (ADR-0044)
│   │   │   ├── intent.py
│   │   │   └── transfer.py
│   │   │
│   │   ├── infra/                      config · db · enums (GENERADO) · ids · logging · rules
│   │   ├── knowledge/                  F06 · chunking (recursivo, tokens estimados por caracteres, ADR-0065, ADR-0071) · search (top_k de documentos indexed, ADR-0073) · repository
│   │   │
│   │   ├── pipeline/                   un módulo por valor de PipelineStep
│   │   │   ├── runner.py               ejecuta los pasos y escribe el rastro
│   │   │   ├── step.py                 contrato común de un paso
│   │   │   ├── repository/             partido por razón de cambio (regla 2)
│   │   │   │   ├── turn.py             las lecturas del turno: bloque · conversación · mensajes · agente IA
│   │   │   │   ├── context.py          el contexto de F04 y sus versiones (ADR-0048)
│   │   │   │   ├── signals.py          las políticas, la racha y las evaluaciones de F12 (ADR-0045)
│   │   │   │   └── evidence.py         la generación con lo que ensambló (ADR-0039, regla 10) · las intenciones · el rastro
│   │   │   ├── notices.py              el texto fijo de cada corte (O2) · no es un paso
│   │   │   ├── blocklist.py            ┐
│   │   │   ├── guardrail.py            │
│   │   │   ├── triage.py               ├ en el orden en que se ejecutan
│   │   │   ├── retrieval.py            │  (ADR-0016)
│   │   │   ├── grounding.py            │
│   │   │   └── generation.py           ┘
│   │   │                               reranking.py no se crea hasta que exista
│   │   │
│   │   ├── prompts/                    F03, F04 · ÚNICA implementación del ensamblador
│   │   │   ├── assembler.py
│   │   │   ├── budget.py
│   │   │   ├── repository.py           los componentes activos con su versión más nueva · las respuestas rápidas activas de la cuenta, hasta el tope (F07, ADR-0061)
│   │   │   └── rewriter.py
│   │   │
│   │   ├── rules/                      datos versionados, no código
│   │   │   ├── blocklist.yaml   ·   profanity.yaml   ·   notices.yaml
│   │   │   ├── guardrails.yaml  ·   triage.yaml
│   │   │   ├── generation.yaml  ·   pricing.yaml  ·   rewriter.yaml
│   │   │   ├── extraction.yaml · frustration.yaml · intent.yaml
│   │   │   ├── chunking.yaml · image_description.yaml   los dos de la ingesta (ADR-0065, ADR-0067)
│   │   │   └── retrieval.yaml          top_k de la búsqueda y umbral del grounding (ADR-0068, regla 5; ADR-0073)
│   │   │                               frustration.yaml no se crea hasta que exista el modelo (ADR-0044)
│   │   │                               los umbrales por defecto de F12 viven en triage.yaml (ADR-0045)
│   │   │
│   │   ├── tests/                      unitarios · conftest.py
│   │   │                               turns/ · triage/: tests partidos, con sus dobles en doubles.py (regla 2)
│   │   └── worker/                     F06 · ingesta · segundo entrypoint, mismo código (ADR-0064, ADR-0071)
│   │                                   loop: sondea y barre · ingest: el orden de la regla 7 · repository: status, locked_at y document_chunks
│   │                                   crypto: abre lo que selló el núcleo · pdf: texto e imágenes con pypdf · images: la descripción
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
│   │   │   │   ├── repository.go       ai_agents, audit_logs, prompt_components y sus versiones · la baja es deleted_at y active es la pausa · audita el default de una bandeja para whatsapp/ y los cambios de operadores para operators/ (ADR-0062)
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
│   │   │   │   └── controller · edit_controller · labels_controller · params · routes · service · repository · model · attributes
│   │   │   │                           edit_controller: alta, edición y los cuatro cifrados · labels_controller: etiquetar · params: id, filtros y página (regla 2)
│   │   │   │                           attributes: guarda lo que extrae F31 y lo muestra en la ficha (ADR-0047)
│   │   │   │
│   │   │   ├── conversations/          DUEÑO del agregado compartido
│   │   │   │   ├── service.go          turn/, inbox/, whatsapp/ y agents/ escriben por acá · nace en off si el agente IA no puede responder (ADR-0038) · cada mensaje del cliente suma a unread_count y la respuesta de un operador lo borra (ADR-0052)
│   │   │   │   ├── repository.go       recibe el Executor de quien conduce
│   │   │   │   ├── messages_repository.go  los mensajes: escribirlos, ligarlos al bloque y marcarlos enviados o fallidos (regla 2)
│   │   │   │   ├── scope.go            qué contactos alcanza un operator (ADR-0032, regla 2) · la condición la escribe el dueño de la tabla
│   │   │   │   └── model.go
│   │   │   │
│   │   │   ├── documents/              bases de conocimiento y carga · F06 (ABM) · DUEÑO de knowledge_bases y documents
│   │   │   │   ├── controller.go       el multipart de hasta 100 MB · el aviso vigente · el listado con lo que queda de la base
│   │   │   │   ├── routes.go           sólo admin: GET /documents/notice · GET, POST y DELETE /ai-agents/:id/documents
│   │   │   │   ├── service.go          sólo PDF · el objeto cifrado antes que la fila, y borrado si la fila falla (ADR-0064) · el duplicado es un 409 (ADR-0070)
│   │   │   │   ├── notice.go           el texto del aviso de datos personales y su versión (ADR-0066)
│   │   │   │   ├── repository.go       el bloqueo de la base serializa el límite y el duplicado · la baja es deleted_at
│   │   │   │   └── model.go
│   │   │   │
│   │   │   ├── inbox/                  bandeja · F13, F22–F27 · exporta Reachable, CanSee y EventRow (ADR-0063)
│   │   │   │   ├── controller.go       DTO, la bandeja, el hilo, el envío y la sugerencia
│   │   │   │   ├── actions_controller.go  modo, leída, cierre, estrella y puntaje de una conversación
│   │   │   │   ├── params.go           el tope de limit, los filtros de F23 y el cursor opaco (ADR-0036)
│   │   │   │   ├── routes.go           los tres roles en todas menos reasignar y el traspaso, que son de supervisor y admin (ADR-0056): lo que cada uno alcanza lo decide service.go
│   │   │   │   ├── dispatch.go         el vendedor de cada conversación, en la transacción del webhook (ADR-0037)
│   │   │   │   ├── handover.go         F26: reasignar una abierta y el traspaso en bloque de un ausente, con historial y motivo · el historial, con el alcance del hilo (ADR-0056)
│   │   │   │   ├── handover_controller.go  POST /conversations/:id/reassign · POST /inbox/handover · GET /conversations/:id/assignments
│   │   │   │   ├── labels.go           F24: etiquetas del catálogo, que pregunta a labels/, con el alcance del hilo · pedir el estado que ya existe no es error (ADR-0057)
│   │   │   │   ├── labels_controller.go  POST /conversations/:id/labels · DELETE /conversations/:id/labels/:label_id
│   │   │   │   ├── service.go          alcance por rol: quién lee, quién responde y quién maneja el bot · la bandeja y el hilo
│   │   │   │   ├── send.go             toma el pool en la transacción del envío · envía una sola vez (ADR-0020) · la sugerencia que se copia (ADR-0046)
│   │   │   │   ├── actions.go          el modo, el operator sólo de lo suyo (ADR-0037), y prenderlo exige un agente IA que pueda responder (ADR-0038) · la estrella de F25, con el alcance del hilo y sin mover la bandeja (ADR-0053) · la marca de leída, con el alcance de responder (ADR-0052) · el cierre manual, con el alcance del interruptor y rechazado con un turno en vuelo (ADR-0054) · el puntaje de F13, sólo del vendedor y sobre resueltas (ADR-0055)
│   │   │   │   ├── repository.go       la bandeja con su último mensaje, sus fallidos y si su agente IA puede responder · los filtros de F23, siempre después del alcance (ADR-0050) · pone las etiquetas en la fila (ADR-0057)
│   │   │   │   ├── assignments_repository.go  escribe conversation_assignments · el vendedor anterior y el siguiente del reparto · el historial (ADR-0037, ADR-0056)
│   │   │   │   ├── labels_repository.go  escribe conversation_labels (ADR-0057)
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
│   │   │   ├── notes/                  notas internas · F26 · DUEÑO de internal_notes (ADR-0056, ADR-0063)
│   │   │   │   ├── controller.go       GET y POST /conversations/:id/notes · PATCH y DELETE /conversations/:id/notes/:note_id · los códigos de antes
│   │   │   │   ├── routes.go           los tres roles: el alcance lo pide a inbox/ (Reachable)
│   │   │   │   ├── service.go          con el alcance del hilo · sólo su autor las edita o las borra (ADR-0056)
│   │   │   │   ├── repository.go       escribe internal_notes · borrar es lógico (C09)
│   │   │   │   └── model.go
│   │   │   │
│   │   │   ├── operators/              usuarios y roles · F18 · DUEÑO de operators (ADR-0030, ADR-0062)
│   │   │   │   ├── controller.go       login (público) y /me (autenticado): dos tipos, dos zonas
│   │   │   │   ├── routes.go           POST /auth/login fuera de /api/v1: quien se loguea no es actor · el ABM, del admin; la lista, también del supervisor
│   │   │   │   ├── roster.go           el ABM: nunca sin un admin activo · el admin no se toca a sí mismo · contraseñas · audita por agents/
│   │   │   │   ├── roster_controller.go  desactivar y borrar responden cuántas conversaciones le quedan (held_conversations)
│   │   │   │   ├── roster_repository.go  bloquea a los admins antes que la fila · el hash anterior en el predicado de la contraseña
│   │   │   │   ├── service.go          una sola respuesta para las tres formas de fallar (ADR-0030)
│   │   │   │   ├── repository.go       normaliza el email: el índice único es sobre la columna cruda
│   │   │   │   ├── model.go            Operator lleva el hash, así que nunca se serializa
│   │   │   │   └── password.go         bcrypt cost 12 · gasta una comparación si el email no existe
│   │   │   │
│   │   │   ├── platform/               lo transversal · la otra excepción declarada
│   │   │   │   ├── auth/               emite y verifica el token de sesión · sin gin y sin base (ADR-0030)
│   │   │   │   ├── encryption/         AES-256-GCM sobre los cuatro campos sensibles y, con otra llave, los documentos de F06 · la llave se inyecta (ADR-0031, ADR-0064)
│   │   │   │   ├── config/             se lee del entorno y se valida al arrancar
│   │   │   │   ├── db/                 db.go (pool) · executor.go (pool | pgx.Tx)
│   │   │   │   ├── enums/              GENERADO desde contracts/enums.yaml
│   │   │   │   ├── http/               router · route · middleware · errors
│   │   │   │   ├── httpclient/         cliente HTTP con timeouts y reintentos
│   │   │   │   ├── id/                 uuid v7
│   │   │   │   ├── logging/
│   │   │   │   └── storage/            Firebase por la API JSON de Cloud Storage · sube y borra, nunca lee (ADR-0069, ADR-0070) · el puerto lo declara documents/
│   │   │   │
│   │   │   ├── quick_replies/          respuestas rápidas · F09 · DUEÑO de quick_replies (ADR-0060) · paquete quickreplies
│   │   │   │   ├── controller.go       q busca por título y contenido · el operador copia el texto en la caja, no hay ruta de envío
│   │   │   │   ├── routes.go           leen los tres roles, escribe sólo el admin (F09)
│   │   │   │   ├── service.go          las pausadas sólo las ve el admin · el autor sale del token
│   │   │   │   ├── repository.go       strpos y no LIKE · el título repetido es el índice único, sin distinguir mayúsculas
│   │   │   │   └── model.go
│   │   │   │
│   │   │   ├── realtime/               F21: el hub en memoria y el socket · sin tablas (ADR-0051, ADR-0063)
│   │   │   │   ├── hub.go              el fan-out con el alcance del listado (inbox.CanSee) · tickets de un uso · nunca bloquea a quien publica · lo usan inbox/ y reminders/
│   │   │   │   ├── stream.go           POST /inbox/stream-ticket detrás del bearer · GET /inbox/stream, pública: el ticket es la credencial
│   │   │   │   └── routes.go
│   │   │   │
│   │   │   ├── reasoning/              F05: los componentes de cada respuesta · SÓLO LECTURA (ADR-0049, ADR-0063)
│   │   │   │   ├── controller.go       GET /conversations/:id/generations/:generation_id/components
│   │   │   │   ├── routes.go
│   │   │   │   ├── service.go          con el alcance del hilo (inbox.Reachable) · el texto, sólo para el admin
│   │   │   │   ├── repository.go       la generación y las versiones con que se armó
│   │   │   │   └── model.go
│   │   │   │
│   │   │   ├── reminders/              recordatorios · F28 · DUEÑO de reminders (ADR-0058)
│   │   │   │   ├── controller.go       instantes RFC 3339 con su zona (C06) · due sólo se prende con true
│   │   │   │   ├── routes.go           los tres roles: el alcance lo decide contacts/, los cambios el dueño, supervisor y admin
│   │   │   │   ├── service.go          cuelga del contacto que el caller alcanza (ADR-0002 (c)) · mover la fecha lo vuelve pendiente
│   │   │   │   ├── poller.go           cada 30 s marca los vencidos con SKIP LOCKED y avisa al vendedor por el hub (ADR-0041, ADR-0051)
│   │   │   │   ├── repository.go       el destinatario: el vendedor activo de la conversación más reciente, o el dueño
│   │   │   │   └── model.go
│   │   │   │
│   │   │   ├── reporting/              SÓLO LECTURA · F16, F32, F34–F38
│   │   │   │   ├── controller.go
│   │   │   │   ├── service.go
│   │   │   │   └── queries.go          no tiene repository: no escribe nada
│   │   │   │
│   │   │   ├── sales/                  registro de ventas · F14 · DUEÑO de sales (ADR-0059)
│   │   │   │   ├── controller.go       el monto como texto decimal, nunca un número JSON (C08)
│   │   │   │   ├── routes.go           los tres roles: el vendedor lo suyo, supervisor y admin cualquiera, que decide service.go · el alcance lo pide a inbox/ (Reachable, ADR-0063)
│   │   │   │   ├── service.go          acredita siempre al vendedor asignado y copia el agente IA · el pool no admite ventas
│   │   │   │   ├── repository.go       el canal de origen se lee del contacto (F15), no se copia
│   │   │   │   └── model.go
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
| `agents/` | `ai_agents` · `prompt_components` · `prompt_component_versions` · `audit_logs` | `audit_logs` está acotada a cambios de configuración del agente IA, con qué agente IA atiende cada número y sin los componentes del prompt, y a los cambios de operadores (ADR-0007, ADR-0038, ADR-0039, ADR-0062) |
| `contacts/` | `contacts` · `contact_attributes` · `contact_labels` | `contact_attributes` la infiere el cerebro y **la persiste el núcleo** (F31) |
| `conversations/` | `conversations` · `messages` | El **agregado compartido**: `turn/`, `inbox/`, `whatsapp/` y `agents/` escriben por acá |
| `documents/` | `knowledge_bases` · `documents` | Los *chunks* no: los escribe el cerebro |
| `inbox/` | `conversation_assignments` · `conversation_labels` | |
| `labels/` | `labels` | El catálogo; las dos tablas de unión son de quien etiqueta |
| `meta_templates/` | `meta_templates` | |
| `notes/` | `internal_notes` | Salió de `inbox/` (ADR-0063) |
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
