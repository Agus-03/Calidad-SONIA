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

*(actualizar al cerrar cada paso de la etapa de diseño)*

- **Hecho:** convenciones (`00-convenciones.md`) · matriz de trazabilidad de F01–F39 y OE1–OE7, con 31 hallazgos (`01-trazabilidad.md`) · 17 decisiones de diseño (`02-decisiones/`), 16 aceptadas y ADR-0017 en propuesta · catálogo de 37 entidades con atributos y relaciones, auditado (`DER/entidades.md`, `DER/relaciones.md`) · capa de prompts: las cinco reglas de ensamblado, versionado, ciclo de vida del contexto, redacción de PII y presupuesto de tokens, con ejemplo renderizado completo (`03-capa-prompts.md`, Paso 4).
- **Ojo con el alcance:** ADR-0012 agrega `accounts` como **previsión estructural** de algo que la sección 7.3 declara *trabajo futuro* (multi-tenant). Opera con **una sola fila** y la funcionalidad multi-empresa **no se implementa**. La regla que lo acota está en `DER/relaciones.md` §9 — no quitarla: es lo que evita que el DER contradiga al informe.
- **Paso 6 — cerrado:** escribir la consulta SQL de cada métrica contra el modelo de papel, sin ejecutarla. **Regla: si una métrica no se puede escribir como consulta, falta un dato y el diseño no está terminado.** Hechas **OE1, OE3 y OE4** → H-18 a H-21, ADR-0014 (`signal_evaluations`) y ADR-0015 (`prompt_component_versions.active` + `generations.ai_agent_id`, con enmienda a la Regla 2). Recorridos también **OE2/OE7 (tono), OE6 y F35/F16**, que produjeron H-22 a H-25: **los tres de tipo `definición` quedaron diferidos al Paso 11** —el anteproyecto nombra la métrica y no define su denominador— y lo ya razonado está en `02-decisiones/README.md` para no rediscutirlo. **H-23 se resolvió con ADR-0016 (`pipeline_steps`)**: el equipo definió el pipeline de respuesta —*blocked-list → guardrail → triage → RAG → grounding (reranking si hace falta) → blocked-list → guardrail → respuesta*— del que el catálogo modelaba **un solo paso**, y ahora se registra el rastro completo por turno. De paso destraba OE6: el veredicto de `triage` se emite **antes** de recuperar, así que sirve de denominador sin circularidad. **Ojo:** la enumeración `PipelineStep` se fijó **antes** de dibujar el pipeline (Paso 8) y es esperable que cambie; y `pipeline_steps` es la tabla que más rápido crece después de `messages` — necesita retención en el Paso 7. **Entregable escrito: `docs/design/05-consultas-de-metricas.sql`** — completas OE1, OE3, OE7 (autónoma), F34, F36 a/b, F37 y F38; parciales OE4 y OE7/F36 en su mitad de tono; bloqueadas OE2, OE6, F35 y F16, **cada una con la decisión que la desbloquea nombrada en su encabezado — esa lista es la agenda del Paso 11**. OE5 y la previsualización de OE1 quedan declaradas fuera del SQL (cobertura de tests y prueba funcional). De paso quedó **enmendado C06**: la conversión a hora local ocurre *antes* de agrupar, no sólo en presentación — si no, el pico horario de F38 sale corrido tres horas.
- **Paso 8 — borrador escrito, las cinco preguntas cerradas (`07-secuencia-del-turno.md`):** secuencia del turno con el camino feliz más tres ramas —modo sugerencia (F02), corte temprano por blocked-list o guardrail (ADR-0016) y cerebro caído—, con **quién escribe qué y en qué orden** en cada una. Produjo **H-26 a H-31**, y con ellos un **tipo de hallazgo nuevo, `consulta`**: ni falta un dato ni falta una definición, falta usar bien un dato que ya está. Decidido: **`GenerationStatus` gana `blocked`** (los cinco valores viejos describen revisión humana, y el guardrail de salida es posterior a la enumeración) · **`message_batches.error_message`** · **tres reintentos configurables por variable de entorno** antes de declarar el turno fallido · **`transfer_events` no se escribe en modo sugerencia** (registra la *acción*; la evaluación queda en `signal_evaluations`) · y el `WHERE` de **OE7 corregido**. Diferidos al Paso 7: la **idempotencia de los salientes** (H-29) y **dónde viven los bytes de los documentos** (H-31 — `documents` no tiene ruta ni clave de objeto para un archivo de hasta 100 MB).
  - **Revisar el documento contra ADR-0017 encontró más que escribirlo.** El ADR quedó corregido en tres puntos y sigue en Propuesta: **(i).2 pasa de "una tabla, un servicio" a "una columna, un servicio"** —la regla anterior era falsa contra el propio diagrama, porque el núcleo tiene que escribir `generations.message_id`—, **declara el tercer escritor** (el instrumental offline, dueño de `conversation_annotations` y `tone_evaluations`) y **corrige (i).3**, que afirmaba que `generations.status` ya tenía los valores necesarios cuando le faltaba `blocked`. La regla de la excepción es temporal y conviene recordarla textual: **el cerebro es dueño de la fila hasta que responde; desde ahí es del núcleo.**
  - **H-27 es el que conviene recordar:** OE7 estaba marcada `[OK]` y salía **inflada** — contaba las sugerencias *aceptadas* como resolución autónoma, porque filtraba por `sender_type = 'operator'` y una sugerencia aceptada produce un mensaje de la IA. No faltaba ningún dato: `reviewed_by_operator_id` estaba desde el Paso 5. **Una consulta validada contra el modelo no está validada contra el comportamiento.**
  - **Regla de estados que hay que sostener:** `generations.status` describe el **filtrado** (¿pasó sin revisión, la revisó alguien, la frenó el sistema?), **no la entrega** — eso vive en `messages.status`. Por eso `auto_sent` se escribe antes de enviar y no miente, y por eso no hizo falta un sexto valor. La contrapartida: la generación se escribe **después** del guardrail de salida, así que una caída *durante* el guardrail la pierde y deja sólo sus `pipeline_steps`.
  - **Los contadores no se movieron: 37 entidades, 73 relaciones, 22 enumeraciones.** Un paso de comportamiento que obliga a crear entidades es un Paso 5 incompleto; éste agregó un valor de enumeración y una columna.
- **Arquitectura — decisión abierta (ADR-0017, en propuesta; revisada 2026-08-22):** la sección 8.4 nunca eligió lenguaje (*"Python o GO"*) y 8.5 dice *"tres módulos **integrados**"*, no tres servicios. La propuesta es **tres desplegables contra un único PostgreSQL** —núcleo operativo en Go (gateway, timer, WebSockets, ABM), cerebro en Python **en dos modos de arranque, `api` y `worker`** (prompts, RAG, LLM, clasificadores, ingesta), y frontend *A DEFINIR*— más el instrumental de evaluación, que no se despliega. Los tres módulos son *bounded contexts*, no procesos. **Lo innegociable es la base compartida:** con base por servicio, la cadena de trazabilidad de OE2 deja de ser un join y se rompe el aporte de la tesina. **Requiere acuerdo de los tres antes de firmarse.**
  - **La revisión del 22/08 salió de recorrer los 39 requisitos uno por uno** y está declarada en el §0 del ADR. Encontró **un hueco** —la ingesta de F06, con documentos de hasta 100 MB, no tenía proceso donde ocurrir— y agregó **el ensamblador único en Python** (si se reimplementa en Go, OE1 mide un render distinto del que ve el administrador), **tres reglas de escritura sobre la base compartida** y **el nombre de la arquitectura**. El §6 del ADR mapea los 39 requisitos a lenguaje y paquete: **26 Go · 7 Python · 4 partidos · 2 documentales**, y **ningún módulo del anteproyecto es monolingüe** — es la razón de fondo por la que "un lenguaje por módulo" no tiene respuesta.
  - **La regla de escritura que hay que sostener:** el cerebro escribe **sólo tablas de evidencia** y **nunca `conversations` ni `messages`** — devuelve la respuesta y el núcleo la persiste y la envía. Es lo que evita un mensaje guardado que nunca se envió, o enviado dos veces.
  - **Nombre para el informe: *arquitectura basada en servicios*.** **No** llamarla "microservicios" (comparten base) ni "monolito distribuido" (es un término peyorativo, nombra un antipatrón).
  - **Ojo:** el ADR declara `documents.locked_at`, pero **el catálogo todavía no lo tiene** — se agrega cuando ADR-0017 pase a Aceptada, no antes.
- **Dos causas, no una, cuando una métrica no se puede escribir** (enmienda al criterio del Paso 6, ver `DER/relaciones.md` §11): si **falta un dato** se modela ahora, porque descubrirlo con datos productivos encima es irreparable; si **falta una definición** se registra el hallazgo y se difiere, porque modelar antes de decidir deja que el modelo decida en silencio.
- **El patrón de los tres hallazgos resueltos** conviene tenerlo a mano al verificar los que faltan: en los tres casos el modelo guardaba **el resultado de una decisión y no la evidencia sobre la que se tomó**, y el síntoma era siempre una métrica que sólo podía devolver el número deseado.
- **Paso 5 — el DER ya está dibujado en Miro; falta exportarlo fechado a `docs/diagrams/`** (`der-logico-AAAA-MM-DD.png` y `.pdf`, C10). Ojo al verificar: son **37 entidades, 73 relaciones y 22 enumeraciones** — `signal_evaluations` y `pipeline_steps` se agregaron después de dibujarlo. Usar `DER/guia-de-dibujo.md` (bloques, atributos y relaciones ordenados para transcribir) y verificar contra `docs/diagrams/der-logico-scaffold.mmd`. Exportar fechado a `docs/diagrams/`. No bloquea al Paso 6, porque la fuente de verdad es el catálogo (ADR-0013).
- **Fuente de verdad del modelo:** el catálogo (`DER/entidades.md` + `relaciones.md`), **no el tablero de Miro** — ADR-0013 enmienda C10 en ese punto. Se cambia primero en el catálogo y después en el tablero. `guia-de-dibujo.md` y el andamio `.mmd` son derivados: se regeneran, no se editan a mano.
- **Pendientes abiertos:** ver la sección final de `02-decisiones/README.md` (siete; el único que toca al Paso 6 es la **regla de atribución entre señales simultáneas** — sin ella no se escribe la precisión fina de F36; el de asignación de agente y el de "cambio de intención" se deciden en el Paso 11, no en el modelo) · dos extensiones identificadas en el Paso 4 sin modelar todavía (reranking del RAG, guardrails de ciberseguridad) — ver `03-capa-prompts.md` §3.
