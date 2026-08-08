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

- **Hecho:** convenciones (`00-convenciones.md`) · matriz de trazabilidad de F01–F39 y OE1–OE7, con 18 hallazgos (`01-trazabilidad.md`) · 11 decisiones de diseño (`02-decisiones/`) · catálogo de 34 entidades con atributos y relaciones (`DER/entidades.md`, `DER/relaciones.md`).
- **Siguiente:** dibujar el modelo de datos lógico por bloques a partir del catálogo del DER, y escribir las consultas SQL de las métricas de OE1–OE7 para verificarlo (Paso 6: si una métrica no se puede escribir como consulta, falta un dato).
- **Pendientes abiertos:** ver la sección final de `02-decisiones/README.md` (tres, ninguno bloqueante).
