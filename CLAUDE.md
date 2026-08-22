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

- **Hecho:** convenciones (`00-convenciones.md`) · matriz de trazabilidad de F01–F39 y OE1–OE7, con 25 hallazgos (`01-trazabilidad.md`) · 17 decisiones de diseño (`02-decisiones/`), 16 aceptadas y ADR-0017 en propuesta · catálogo de 37 entidades con atributos y relaciones, auditado (`DER/entidades.md`, `DER/relaciones.md`) · capa de prompts: las cinco reglas de ensamblado, versionado, ciclo de vida del contexto, redacción de PII y presupuesto de tokens, con ejemplo renderizado completo (`03-capa-prompts.md`, Paso 4).
- **Ojo con el alcance:** ADR-0012 agrega `accounts` como **previsión estructural** de algo que la sección 7.3 declara *trabajo futuro* (multi-tenant). Opera con **una sola fila** y la funcionalidad multi-empresa **no se implementa**. La regla que lo acota está en `DER/relaciones.md` §9 — no quitarla: es lo que evita que el DER contradiga al informe.
- **Paso 6 — cerrado:** escribir la consulta SQL de cada métrica contra el modelo de papel, sin ejecutarla. **Regla: si una métrica no se puede escribir como consulta, falta un dato y el diseño no está terminado.** Hechas **OE1, OE3 y OE4** → H-18 a H-21, ADR-0014 (`signal_evaluations`) y ADR-0015 (`prompt_component_versions.active` + `generations.ai_agent_id`, con enmienda a la Regla 2). Recorridos también **OE2/OE7 (tono), OE6 y F35/F16**, que produjeron H-22 a H-25: **los tres de tipo `definición` quedaron diferidos al Paso 11** —el anteproyecto nombra la métrica y no define su denominador— y lo ya razonado está en `02-decisiones/README.md` para no rediscutirlo. **H-23 se resolvió con ADR-0016 (`pipeline_steps`)**: el equipo definió el pipeline de respuesta —*blocked-list → guardrail → triage → RAG → grounding (reranking si hace falta) → blocked-list → guardrail → respuesta*— del que el catálogo modelaba **un solo paso**, y ahora se registra el rastro completo por turno. De paso destraba OE6: el veredicto de `triage` se emite **antes** de recuperar, así que sirve de denominador sin circularidad. **Ojo:** la enumeración `PipelineStep` se fijó **antes** de dibujar el pipeline (Paso 8) y es esperable que cambie; y `pipeline_steps` es la tabla que más rápido crece después de `messages` — necesita retención en el Paso 7. **Entregable escrito: `docs/design/05-consultas-de-metricas.sql`** — completas OE1, OE3, OE7 (autónoma), F34, F36 a/b, F37 y F38; parciales OE4 y OE7/F36 en su mitad de tono; bloqueadas OE2, OE6, F35 y F16, **cada una con la decisión que la desbloquea nombrada en su encabezado — esa lista es la agenda del Paso 11**. OE5 y la previsualización de OE1 quedan declaradas fuera del SQL (cobertura de tests y prueba funcional). De paso quedó **enmendado C06**: la conversión a hora local ocurre *antes* de agrupar, no sólo en presentación — si no, el pico horario de F38 sale corrido tres horas.
- **Arquitectura — decisión abierta (ADR-0017, en propuesta):** la sección 8.4 nunca eligió lenguaje (*"Python o GO"*) y 8.5 dice *"tres módulos **integrados**"*, no tres servicios. La propuesta es **dos deployables contra un único PostgreSQL** —núcleo operativo en Go (gateway, timer, WebSockets, ABM) y cerebro en Python (prompts, RAG, LLM, clasificadores)—, con los tres módulos como *bounded contexts* y no como procesos. **Lo innegociable es la base compartida:** con base por servicio, la cadena de trazabilidad de OE2 deja de ser un join y se rompe el aporte de la tesina. **Requiere acuerdo de los tres antes de firmarse.**
- **Dos causas, no una, cuando una métrica no se puede escribir** (enmienda al criterio del Paso 6, ver `DER/relaciones.md` §11): si **falta un dato** se modela ahora, porque descubrirlo con datos productivos encima es irreparable; si **falta una definición** se registra el hallazgo y se difiere, porque modelar antes de decidir deja que el modelo decida en silencio.
- **El patrón de los tres hallazgos resueltos** conviene tenerlo a mano al verificar los que faltan: en los tres casos el modelo guardaba **el resultado de una decisión y no la evidencia sobre la que se tomó**, y el síntoma era siempre una métrica que sólo podía devolver el número deseado.
- **Paso 5 — el DER ya está dibujado en Miro; falta exportarlo fechado a `docs/diagrams/`** (`der-logico-AAAA-MM-DD.png` y `.pdf`, C10). Ojo al verificar: son **37 entidades, 73 relaciones y 22 enumeraciones** — `signal_evaluations` y `pipeline_steps` se agregaron después de dibujarlo. Usar `DER/guia-de-dibujo.md` (bloques, atributos y relaciones ordenados para transcribir) y verificar contra `docs/diagrams/der-logico-scaffold.mmd`. Exportar fechado a `docs/diagrams/`. No bloquea al Paso 6, porque la fuente de verdad es el catálogo (ADR-0013).
- **Fuente de verdad del modelo:** el catálogo (`DER/entidades.md` + `relaciones.md`), **no el tablero de Miro** — ADR-0013 enmienda C10 en ese punto. Se cambia primero en el catálogo y después en el tablero. `guia-de-dibujo.md` y el andamio `.mmd` son derivados: se regeneran, no se editan a mano.
- **Pendientes abiertos:** ver la sección final de `02-decisiones/README.md` (siete; el único que toca al Paso 6 es la **regla de atribución entre señales simultáneas** — sin ella no se escribe la precisión fina de F36; el de asignación de agente y el de "cambio de intención" se deciden en el Paso 11, no en el modelo) · dos extensiones identificadas en el Paso 4 sin modelar todavía (reranking del RAG, guardrails de ciberseguridad) — ver `03-capa-prompts.md` §3.
