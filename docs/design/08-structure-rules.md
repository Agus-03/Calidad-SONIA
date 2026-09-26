# Reglas de estructura del código

Detalla a [ADR-0017](02-decisiones/0017-decomposicion-en-despliegues-y-lenguajes.md) §6.1, cuyo árbol se actualizó al firmarse el ADR el 2026-09-05. Ante conflicto entre los dos, gana este archivo.

**El árbol completo, archivo por archivo, y el dueño de cada una de las 37 tablas están en [`../struct.md`](../struct.md).** Lo de acá abajo es el resumen; ante conflicto, gana este archivo y hay que corregir el otro.

**`docs/struct.md` es el mapa y nada más: NUNCA se le agrega estado de avance.** No va qué existe hoy, qué falta, qué está a medias, ni cobertura ni fechas de verificación. Dice **dónde va cada cosa**, no qué está hecho —y por eso lista archivos que todavía no existen—. El motivo: un mapa con estado adentro caduca en cada commit y deja de ser confiable justo para lo que sirve. El avance vive en `ESTADO.md`, que es bitácora y se espera que cambie.

```
sonia/
├── docs/                                  documentación
├── contracts/
│   ├── brain.openapi.json                 contrato del cerebro, generado por FastAPI
│   ├── enums.yaml                         las 22 enumeraciones, fuente única para Go y Python
│   └── generate.py                        baja enums.yaml a Go y a Python · `make contracts`
├── deploy/                                docker-compose.yml · core.Dockerfile · brain.Dockerfile
├── .github/workflows/ci.yml
├── .env.example   ·   Makefile
└── src/
    ├── core/                              Go · gin
    │   ├── main.go                        sólo `main()`: la señal y el código de salida
    │   ├── internal/
    │   │   ├── app/                       el arranque · app.go (`Run`) · deps.go (el grafo de objetos)
    │   │   ├── whatsapp/                  F20, F29 · controller.go (entrada del webhook) · client.go (envío a Meta)
    │   │   ├── meta_templates/            plantillas de Meta: sincroniza y sirve · F08, F37
    │   │   ├── turn/                      orquestador del turno
    │   │   │   ├── collector.go           agrupa mensajes fragmentados en el bloque abierto · F10 (ADR-0041)
    │   │   │   ├── poller.go              cierra los bloques vencidos y arranca el turno · F10 (ADR-0041)
    │   │   │   ├── orchestrator.go        conduce el turno: cerebro → persistir → enviar
    │   │   │   └── retry.go               3 reintentos configurables
    │   │   ├── conversations/             DUEÑO de conversations y messages · el agregado compartido
    │   │   ├── agents/                    ABM del agente IA y sus componentes · F01, F02, F17
    │   │   ├── documents/                 bases de conocimiento y documentos: ABM y carga · F06
    │   │   ├── inbox/                     bandeja, WebSockets, etiquetas del chat · F05, F13, F21–F27
    │   │   ├── contacts/                  contactos, segmentación, origen · F15, F30, F33
    │   │   ├── labels/                    catálogo de etiquetas · F19
    │   │   ├── sales/                     registro de ventas · F14
    │   │   ├── reminders/                 recordatorios · F28
    │   │   ├── operators/                 usuarios y roles · F18
    │   │   ├── quick_replies/             respuestas rápidas · F09
    │   │   ├── reporting/                 sólo lectura · F16, F32, F34–F38
    │   │   ├── brainclient/               cliente HTTP del cerebro
    │   │   └── platform/                  http/ (router, middleware, auth) · httpclient/ · storage/ · db (pool + Executor) · id · config · logging · enums (generado)
    │   ├── migrations/                    único dueño del esquema · DDL + invariantes
    │   └── seeds/                         datos de demo, idempotentes, nunca en producción
    │
    ├── brain/                             Python · FastAPI
    │   ├── pyproject.toml                 Poetry · absorbe la config de pytest · poetry.lock se commitea
    │   ├── main.py                        un entrypoint, flag api|worker
    │   ├── api/                           routes/ + schemas/ (Pydantic = el contrato)
    │   ├── pipeline/
    │   │   ├── runner.py                  ejecuta los pasos y escribe pipeline_steps
    │   │   ├── step.py                    contrato común de un paso
    │   │   ├── blocklist.py               guardrail.py · triage.py
    │   │   ├── notices.py                 el texto fijo de cada corte (O2) — no es un paso
    │   │   └── retrieval.py               grounding.py · generation.py
    │   ├── prompts/                       assembler.py (F03) · rewriter.py (F04)
    │   ├── classifiers/                   frustración, intención, transferencia, extracción · F11, F12, F31
    │   ├── knowledge/                     chunking, embeddings, búsqueda · F06
    │   ├── worker/                        ingesta de documentos · F06
    │   ├── adapters/
    │   │   ├── llm/                       base.py (puerto) · registry.py (fábrica) · anthropic.py
    │   │   ├── vectorstore/               qdrant.py
    │   │   └── storage/                   lectura de los bytes que subió el núcleo
    │   ├── rules/                         datos versionados, no código — ver regla 9
    │   │   ├── blocklist.yaml             profanity.yaml · notices.yaml · guardrails.yaml · triage.yaml · grounding.yaml
    │   │   └── rewriter.yaml              frustration.yaml · intent.yaml · transfer.yaml · extraction.yaml
    │   ├── infra/                         config.py (Settings) · db · logging · enums.py (generado)
    │   └── tests/
    │
    ├── frontend/                          frontend
    └── test/
        ├── karate/                        tests de API end-to-end · 8.4
        ├── evals/                         Python · offline, no se despliega · OE2–OE4, OE6, OE7 · F36
        └── datasets/                      200 conversaciones (OE3) · 500 conversaciones (OE4)
```

## Las reglas que la sostienen

1. **Un paquete = las tablas de las que es dueño.** Si algo no tiene lugar obvio, se mira qué tabla toca. Un paquete sin tablas propias es una bolsa y no se crea. **Escribir en la tabla de otro se hace por su repositorio, dentro de la transacción de quien conduce** — nunca con SQL propio. `conversations` y `messages` son el **agregado compartido** que ADR-0017 §1.1 identificó: las escriben `turn/` (el entrante y la respuesta de la IA), `inbox/` (la del operador, más `starred`, `mode` y `operator_id`) y `whatsapp/` (los webhooks de estado), así que su dueño es `conversations/` y los tres pasan por él.
2. **Plano hasta que duela.** Cuando un archivo pasa las ~400 líneas se parte en varios archivos; recién ahí se vuelve carpeta. No se crean subcarpetas de capas dentro de un paquete: eso recrea el árbol capa-primero que el corte por contexto evita.
3. **Sólo `core/migrations/` toca el esquema.** El cerebro hace DML, nunca DDL. Y **el cerebro escribe únicamente tablas de evidencia** —`pipeline_steps`, `generations`, `retrieval_logs`, `signal_evaluations`, `detected_intents`, `document_chunks`—: nunca `conversations`, `messages` ni `contact_attributes`. Lo que no es evidencia lo **devuelve**, y el núcleo lo persiste y lo envía. `migrations/` es DDL más invariantes estructurales (la fila única de `accounts`); `seeds/` son datos de demo, idempotentes, corridos por el `Makefile` y **nunca por la aplicación al arrancar**.
4. **El primer nivel de `internal/` es el *bounded context*, no la capa.** Cada paquete tiene su `controller.go` (HTTP: parsea, valida, responde), `routes.go` (su tabla de rutas: método, path, roles y handler en una fila), `service.go`, `repository.go` y `model.go`. En `platform/` va sólo lo transversal —router, middleware, auth, errores, cliente HTTP, almacenamiento de objetos, base, config, logging— y es una de las **dos** excepciones declaradas. La otra es **`app/`, el arranque**: `app.go` tiene `Run(ctx)` —config, pool, router, servidor, apagado ordenado— y `deps.go` el grafo de objetos, que es lo único que crece al sumar un contexto. Está fuera de `package main` porque `package main` no se puede importar y un test que levante el proceso entero necesita importarlo; en `main.go` quedan sólo `main()`, la señal y el código de salida. **Un archivo que crece se parte en más archivos del mismo paquete** (`dto_create.go`, `dto_update.go`), nunca en un paquete hermano: en Go eso invita al ciclo de imports y parte el contexto en dos.
5. **La transacción la abre quien conduce, no el repositorio.** Los repositorios reciben un `Executor` de `platform/db` —que abstrae el pool y `pgx.Tx`— en vez de guardarse el pool, para que una transacción pueda cruzar paquetes. Lo exige el turno: `07-secuencia-del-turno.md` pide que `message_batches.processed_at` se escriba **en la misma transacción** que el mensaje saliente, `generations.message_id` y `transfer_events`; si no, un reinicio no puede distinguir un turno terminado de uno que nunca se procesó. **Ninguna transacción cruza el borde entre los dos servicios** — eso sigue prohibido por ADR-0017 (i).3.
6. **`pipeline/` tiene un módulo por valor de `PipelineStep`**, para que una fila de `pipeline_steps` mapee a un archivo. `reranking` no se crea hasta que exista.
7. **Un solo dueño de la palabra "orquestador":** el del turno, en `core/internal/turn/`. Lo del cerebro es *el pipeline*.
8. **Los módulos del anteproyecto no son carpetas.** Ningún módulo es monolingüe, así que ninguno tiene carpeta propia: viven en el informe, y el mapeo requisito → paquete es la tabla de ADR-0017 §6.
9. **Lo que decide, se versiona: va en `rules/`, no en código.** Listas de bloqueo, reglas del guardrail y **los prompts internos** (triage, guardrail, grounding, el rewriter de F04, los clasificadores) son YAML con campo `version`. El motivo es `pipeline_steps.version`: ajustar un guardrail invalida las mediciones previas, y la versión de un literal de Python es el commit de git — que no entra en un `WHERE`. **Un clasificador sin prompt también va acá:** `frustration.yaml` guarda la referencia al modelo entrenado, su versión y el umbral ([ADR-0044](02-decisiones/0044-senal-de-frustracion-diferida-a-la-fase-16.md)). **Los seis componentes del prompt del agente no están acá:** los edita el admin y viven en `prompt_components` (F01).
10. **La config se lee del entorno, no de un archivo.** `infra/config.py` (Python, `BaseSettings` + `lru_cache`) y `platform/config` (Go, cargado una vez en `main.go` e inyectado — nunca un global). El `.env` es comodidad de desarrollo que carga `docker-compose`; en producción hay variables, no archivo. **Se valida al arrancar:** si falta un secreto, el proceso no levanta.
11. **Cuatro fronteras tienen puerto y adaptador, y sólo cuatro:** proveedor de LLM (8.4 nombra cuatro; OE2 los compara), base vectorial (ADR-0010), canal de WhatsApp (Twilio → Meta, agendado en el Anexo D) y **almacenamiento de objetos** (el proveedor —Supabase, DigitalOcean, otro— está sin decidir). En el resto, un repositorio alcanza: no se inventan puertos donde no hay dos implementaciones a la vista.
12. **En Python, ningún paquete de primer nivel puede llamarse como uno de la stdlib.** Por eso `infra/` y no `platform/`: con layout plano, `platform` tapa al módulo homónimo de la biblioteca estándar. Antes de crear un paquete, verificar que el nombre esté libre.
