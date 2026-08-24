# SONIA — Contexto del proyecto

Sistema de orquestación conversacional sobre WhatsApp Business API con agentes de IA configurables mediante composición modular de prompts. Tesina de Ingeniería de Sistemas — Universidad Católica de Córdoba, 2026–2027.

El proyecto está en **etapa de diseño**: todavía no hay código de aplicación. El trabajo produce documentos y diagramas.

---

## Fuentes de verdad

Jerarquía. Ante conflicto, gana la de más arriba.

| Fuente | Qué define | Ubicación |
|---|---|---|
| Anteproyecto | Requisitos (F01–F39), objetivos (OE1–OE7), roles, alcance | `docs/SONIA_Anteproyecto.pdf` |
| Documentos de diseño | Convenciones y decisiones de modelado | `docs/design/` |
| Diagramas | Modelo de datos y comportamiento | `docs/diagrams/` |

**Regla:** todo análisis, decisión o propuesta debe estar anclado en el anteproyecto. No inventar requisitos, entidades ni métricas que no estén en él. Si falta información, decirlo explícitamente en lugar de completarla.

---

## `z_helps/` NO tiene peso oficial

`z_helps/` está en `.gitignore`. Es material de trabajo personal para retener información mientras se toman decisiones.

- **Su contenido no son decisiones tomadas.** Puede contener borradores, exploraciones y opciones descartadas.
- **No se referencia desde `docs/`** ni desde ningún entregable.
- Se puede leer como contexto; **nunca citar como autoridad**.

---

## Convenciones de modelado

**Antes de producir cualquier modelo de datos, diagrama, SQL o código, leer [`docs/design/00-convenciones.md`](docs/design/00-convenciones.md).**

Las tres reglas que más fácil se violan por costumbre:

1. **`AIAgent`** es el agente de IA. **`Operator`** es la persona. Nunca `Agent` para el bot ni `User` para el humano.
2. **Inglés** para tablas, columnas, enumeraciones, código y comentarios. **Español** para toda la documentación.
3. `snake_case`, tablas en plural, PK `id` de tipo `uuid` (v7), FK `<entidad>_id`.

El resto —marcas de tiempo, enumeraciones, valores monetarios, política de borrado, herramientas de diagramación— está en el documento.

---

## Estructura del código

Éste es el layout acordado. **Enmienda a [ADR-0017](docs/design/02-decisiones/0017-decomposicion-en-despliegues-y-lenguajes.md) §6.1**, que tiene el árbol anterior — el ADR sigue en Propuesta y hay que actualizarlo al firmarlo.

```
sonia/
├── docs/                                  documentación
├── contracts/
│   ├── brain.openapi.json                 contrato del cerebro, generado por FastAPI
│   └── enums.yaml                         las 22 enumeraciones, fuente única para Go y Python
├── deploy/                                docker-compose.yml · core.Dockerfile · brain.Dockerfile
├── .github/workflows/ci.yml
├── .env.example   ·   Makefile
└── src/
    ├── core/                              Go · gin
    │   ├── main.go                        carga y valida la config, arma e inyecta todo
    │   ├── internal/
    │   │   ├── whatsapp/                  F20, F29 · webhook.go (entrada) · client.go (envío a Meta)
    │   │   ├── turn/                      orquestador del turno
    │   │   │   ├── timer.go               agrupa mensajes fragmentados · F10
    │   │   │   ├── orchestrator.go        conduce el turno: cerebro → persistir → enviar
    │   │   │   └── retry.go               3 reintentos configurables
    │   │   ├── agents/                    ABM del agente IA y sus componentes · F01, F02, F17
    │   │   ├── inbox/                     bandeja, WebSockets, etiquetas del chat · F05, F13, F21–F27
    │   │   ├── contacts/                  contactos, segmentación, origen · F15, F30, F33
    │   │   ├── labels/                    catálogo de etiquetas · F19
    │   │   ├── sales/                     registro de ventas · F14
    │   │   ├── reminders/                 recordatorios · F28
    │   │   ├── operators/                 usuarios y roles · F18
    │   │   ├── quick_replies/             respuestas rápidas · F09
    │   │   ├── reporting/                 sólo lectura · F16, F32, F34–F38
    │   │   ├── brainclient/               cliente HTTP del cerebro
    │   │   └── platform/                  http/ (router, middleware, auth) · httpclient/ · db · config · logging
    │   └── migrations/                    único dueño del esquema
    │
    ├── brain/                             Python · FastAPI
    │   ├── pyproject.toml
    │   ├── main.py                        un entrypoint, flag api|worker
    │   ├── api/                           routes/ + schemas/ (Pydantic = el contrato)
    │   ├── pipeline/
    │   │   ├── runner.py                  ejecuta los pasos y escribe pipeline_steps
    │   │   ├── step.py                    contrato común de un paso
    │   │   ├── blocklist.py               guardrail.py · triage.py
    │   │   └── retrieval.py               grounding.py · generation.py
    │   ├── prompts/                       assembler.py (F03) · rewriter.py (F04)
    │   ├── classifiers/                   frustración, intención, transferencia · F11, F12
    │   ├── knowledge/                     chunking, embeddings, búsqueda · F06
    │   ├── worker/                        ingesta de documentos · F06
    │   ├── adapters/
    │   │   ├── llm/                       base.py · anthropic.py · openai.py · cohere.py
    │   │   └── vectorstore/               qdrant.py
    │   ├── rules/                         datos versionados, no código — ver regla 8
    │   │   ├── blocklist.yaml             guardrails.yaml
    │   │   └── triage.yaml                grounding.yaml
    │   ├── infra/                         config.py (Settings) · db · logging
    │   └── tests/
    │
    ├── frontend/                          frontend
    └── test/                              Python · evaluación offline, no se despliega
```

### Las reglas que la sostienen

1. **Un paquete = las tablas que escribe.** Si algo no tiene lugar obvio, se mira qué tabla toca. Un paquete sin tablas propias es una bolsa y no se crea.
2. **Plano hasta que duela.** Cuando un archivo pasa las ~400 líneas se parte en varios archivos; recién ahí se vuelve carpeta. No se crean subcarpetas de capas dentro de un paquete: eso recrea el árbol capa-primero que el corte por contexto evita.
3. **Sólo `core/migrations/` toca el esquema.** El cerebro hace DML, nunca DDL. Y el cerebro **nunca** escribe `conversations` ni `messages`: devuelve la respuesta y el núcleo la persiste y la envía.
4. **El primer nivel de `internal/` es el *bounded context*, no la capa.** Cada paquete tiene su `controller.go` (HTTP: parsea, valida, responde), `service.go`, `repository.go` y `model.go`. En `platform/http/` va sólo lo transversal —router, middleware, auth, errores— y es la única excepción declarada.
5. **`pipeline/` tiene un módulo por valor de `PipelineStep`**, para que una fila de `pipeline_steps` mapee a un archivo. `reranking` no se crea hasta que exista.
6. **Un solo dueño de la palabra "orquestador":** el del turno, en `core/internal/turn/`. Lo del cerebro es *el pipeline*.
7. **Los módulos del anteproyecto no son carpetas.** Ningún módulo es monolingüe, así que ninguno tiene carpeta propia: viven en el informe, y el mapeo requisito → paquete es la tabla de ADR-0017 §6.
8. **Lo que decide, se versiona: va en `rules/`, no en código.** Listas de bloqueo, reglas del guardrail y **los prompts internos** (triage, guardrail, grounding, el rewriter de F04, los clasificadores) son YAML con campo `version`. El motivo es `pipeline_steps.version`: ajustar un guardrail invalida las mediciones previas, y la versión de un literal de Python es el commit de git — que no entra en un `WHERE`. **Los seis componentes del prompt del agente no están acá:** los edita el admin y viven en `prompt_components` (F01).
9. **La config se lee del entorno, no de un archivo.** `infra/config.py` (Python, `BaseSettings` + `lru_cache`) y `platform/config` (Go, cargado una vez en `main.go` e inyectado — nunca un global). El `.env` es comodidad de desarrollo que carga `docker-compose`; en producción hay variables, no archivo. **Se valida al arrancar:** si falta un secreto, el proceso no levanta.
10. **En Python, ningún paquete de primer nivel puede llamarse como uno de la stdlib.** Por eso `infra/` y no `platform/`: con layout plano, `platform` tapa al módulo homónimo de la biblioteca estándar. Antes de crear un paquete, verificar que el nombre esté libre.

---

## Estilo de trabajo

- **Justificar las decisiones**: explicar el *por qué*, no sólo el *qué*.
- **Señalar las contradicciones** del anteproyecto cuando aparezcan, en vez de resolverlas en silencio.
- **Escribir también las consecuencias negativas** de una decisión, no sólo sus ventajas.
- Documentación en español; identificadores técnicos en inglés.

---

## Notas prácticas

- El anteproyecto es un PDF con capa de texto: se lee con `pdftotext -layout` (disponible en `/mingw64/bin`). La herramienta `Read` falla sobre PDFs porque falta `pdftoppm`.
- Diagramación: **Mermaid** por defecto; **Miro** para el DER y el diagrama de clases.

---

## Estado

Ver [`docs/design/ESTADO.md`](docs/design/ESTADO.md) — qué pasos están cerrados, qué quedó diferido y las trampas de alcance a no repetir.
