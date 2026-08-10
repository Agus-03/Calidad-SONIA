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

- **Hecho:** convenciones (`00-convenciones.md`) · matriz de trazabilidad de F01–F39 y OE1–OE7, con 20 hallazgos (`01-trazabilidad.md`) · 14 decisiones de diseño (`02-decisiones/`) · catálogo de 36 entidades con atributos y relaciones, auditado (`DER/entidades.md`, `DER/relaciones.md`) · capa de prompts: las cinco reglas de ensamblado, versionado, ciclo de vida del contexto, redacción de PII y presupuesto de tokens, con ejemplo renderizado completo (`03-capa-prompts.md`, Paso 4).
- **Ojo con el alcance:** ADR-0012 agrega `accounts` como **previsión estructural** de algo que la sección 7.3 declara *trabajo futuro* (multi-tenant). Opera con **una sola fila** y la funcionalidad multi-empresa **no se implementa**. La regla que lo acota está en `DER/relaciones.md` §9 — no quitarla: es lo que evita que el DER contradiga al informe.
- **En curso — Paso 6:** escribir la consulta SQL de cada métrica contra el modelo de papel, sin ejecutarla. **Regla: si una métrica no se puede escribir como consulta, falta un dato y el diseño no está terminado.** Hechas OE3 y OE4 → produjeron H-18 y H-19 y ADR-0014 (entidad `signal_evaluations`). **Faltan OE1, OE2, OE5, OE6, OE7 y F34–F38**, y hay cinco problemas ya identificados sin resolver: la métrica de OE1 sólo puede dar 100 % (no hay lado "debía" independiente del "fue"), el tono tiene tres definiciones distintas en el anteproyecto (5.3 evaluación humana 85 % · OE2 LLM-as-judge 80 % · OE7 85 %), *"consulta dentro del dominio"* de OE6 no está definida y la definición obvia es circular, la atribución por operador de F35 admite tres criterios, y F38 necesita convertir a hora local **antes** de agrupar (C06 sólo contempla la capa de presentación). Entregable: `docs/design/05-consultas-de-metricas.sql`.
- **También pendiente del Paso 5:** **dibujar** el DER en Miro. El modelado está completo: usar `DER/guia-de-dibujo.md` (bloques, atributos y relaciones ordenados para transcribir) y verificar contra `docs/diagrams/der-logico-scaffold.mmd`. Exportar fechado a `docs/diagrams/`. No bloquea al Paso 6, porque la fuente de verdad es el catálogo (ADR-0013).
- **Fuente de verdad del modelo:** el catálogo (`DER/entidades.md` + `relaciones.md`), **no el tablero de Miro** — ADR-0013 enmienda C10 en ese punto. Se cambia primero en el catálogo y después en el tablero. `guia-de-dibujo.md` y el andamio `.mmd` son derivados: se regeneran, no se editan a mano.
- **Pendientes abiertos:** ver la sección final de `02-decisiones/README.md` (siete; el único que toca al Paso 6 es la **regla de atribución entre señales simultáneas** — sin ella no se escribe la precisión fina de F36; el de asignación de agente y el de "cambio de intención" se deciden en el Paso 11, no en el modelo) · dos extensiones identificadas en el Paso 4 sin modelar todavía (reranking del RAG, guardrails de ciberseguridad) — ver `03-capa-prompts.md` §3.
