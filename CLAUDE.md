# SONIA — Contexto del proyecto

Sistema de orquestación conversacional sobre WhatsApp Business API con agentes de IA configurables mediante composición modular de prompts. Tesina de Ingeniería de Sistemas — Universidad Católica de Córdoba, 2026–2027.

El trabajo produce documentos de diseño, diagramas y el código de los dos servicios de `src/`. Qué está hecho y qué falta no se escribe acá: caduca en cada commit.

---

## Fuentes de verdad

Jerarquía. Ante conflicto, gana la de más arriba.

| Fuente | Qué define | Ubicación |
|---|---|---|
| Anteproyecto | Requisitos (F01–F39), objetivos (OE1–OE7), roles, alcance | `docs/SONIA_Anteproyecto.pdf` |
| Documentos de diseño | Convenciones y decisiones de modelado | `docs/design/` |
| Diagramas | Modelo de datos y comportamiento | `docs/diagrams/` |
| Estructura del código | Árbol completo y dueño de cada tabla | [`docs/struct.md`](docs/struct.md) |

**Regla:** todo análisis, decisión o propuesta debe estar anclado en el anteproyecto. No inventar requisitos, entidades ni métricas que no estén en él. Si falta información, decirlo explícitamente en lugar de completarla.

### La numeración de los requisitos es la del Anexo A

**El Anexo B cita los requisitos corridos en uno** respecto del Anexo A: dice *"agentes IA (F18), usuarios (F19), etiquetas (F20) y webhooks (F21)"*, y en el Anexo A esos cuatro son **F17, F18, F19 y F20**. Es un desfase de numeración entre anexos, no una contradicción de contenido: los módulos coinciden.

- **Toda la documentación y el código citan la numeración del Anexo A.** Un permiso del Anexo B se cita por el nombre del módulo, o con el número del Anexo A.
- **Nunca copiar un número del Anexo B sin traducirlo**: apuntaría al requisito equivocado.
- Registrado como **H-38** en [`docs/design/01-trazabilidad.md`](docs/design/01-trazabilidad.md), y va como nota al pie en el informe.

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

El resto (marcas de tiempo, enumeraciones, valores monetarios, política de borrado, herramientas de diagramación) está en el documento.

---

## Estructura del código

Layout de alto nivel; el árbol completo (con las 12 reglas que lo sostienen) está en [`docs/design/08-structure-rules.md`](docs/design/08-structure-rules.md). El árbol archivo por archivo y el dueño de cada una de las 37 tablas están en [`docs/struct.md`](docs/struct.md). Ante conflicto entre estos tres, gana `docs/design/08-structure-rules.md`.

```
sonia/
├── docs/          documentación
├── contracts/     contrato del cerebro (OpenAPI) · enums.yaml compartido por Go y Python
├── deploy/        docker-compose.yml · Dockerfiles
├── .github/       ci.yml
└── src/
    ├── core/      Go · gin — webhook de WhatsApp, turno, esquema (único dueño de las migraciones)
    ├── brain/     Python · FastAPI — pipeline, prompts, clasificadores, RAG (sólo DML)
    ├── frontend/  frontend (framework a definir, ADR-0017)
    └── test/      karate (E2E) · evals (offline) · datasets
```

---

## Estilo de trabajo

- **Justificar las decisiones**: explicar el *por qué*, no sólo el *qué*.
- **Señalar las contradicciones** del anteproyecto cuando aparezcan, en vez de resolverlas en silencio.
- **Escribir también las consecuencias negativas** de una decisión, no sólo sus ventajas.
- Documentación en español; identificadores técnicos en inglés.

### Cómo trabajar con el equipo

Reglas que salieron de trabajar con nosotros. Valen para cualquiera del grupo.

1. **Preguntar no es aprobar.** Si se pide una explicación, se explica y se espera. Explicar y aplicar en la misma tanda saca la decisión de las manos de quien preguntó.
2. **Un pedido = un cambio.** Nada de arreglos colaterales de paso: si aparece un enlace roto o un dato viejo, se menciona al final como pregunta. Un diff con cosas que nadie pidió deja de ser revisable.
3. **Respuestas breves, y el sí o el no primero.** Nadie tiene tiempo de leer tres pantallas para saber si la respuesta era sí. El detalle va después, y sólo si hace falta.
4. **Separar qué decide quién.** Marcar explícitamente qué ya está decidido, qué se resuelve solo y qué necesita una decisión del grupo. Preguntar únicamente por lo último.
5. **Commits de una sola oración**, sin cuerpo. El razonamiento vive en los documentos, no en el historial de git.
6. **El `.env` no se lee.** Tiene secretos reales. Para verificar configuración están `.env.example`, `deploy/docker-compose.yml` y los dos archivos de config.
7. **Actualizar el CI ante tests validados.** Cada vez que se genere un test y sea validado por el usuario, verificar y actualizar el workflow de CI (`.github/workflows/ci.yml`) para asegurar que quede integrado en la suite de pruebas unitarias correspondiente respetando las buenas prácticas (aislado, hermético y sin dependencias externas innecesarias).

---

## Notas prácticas

- El anteproyecto es un PDF con capa de texto: se lee con `pdftotext -layout` (disponible en `/mingw64/bin`). La herramienta `Read` falla sobre PDFs porque falta `pdftoppm`.
- Diagramación: **Mermaid** por defecto; **Miro** para el DER y el diagrama de clases.

---

## Estado

Ver [`z_helps/ESTADO.md`](z_helps/ESTADO.md) — qué pasos están cerrados, qué quedó diferido y las trampas de alcance a no repetir.

> **Ojo con la tensión:** `ESTADO.md` está en `z_helps/`, que este archivo declara sin peso oficial y que **no se cita como autoridad**. Como bitácora de trabajo encaja; como registro de decisiones tomadas, no. Si se lo va a citar en el informe, tiene que volver a `docs/`.
