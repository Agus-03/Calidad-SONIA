# ADR-0010 — Qdrant como base vectorial

| Campo | Valor |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 2026-08-06 |
| **Etapa** | Diseño — Paso 3 |
| **Requisitos afectados** | F06, OE6 · sección 8.4 |
| **Relacionada con** | [ADR-0005](0005-base-de-conocimiento.md) |

---

## 1. Contexto

La sección 8.4 del anteproyecto deja la elección abierta: *"Vector Store (pgvector o Qdrant) — Búsqueda semántica sobre la base de conocimiento del negocio cooperante"*.

[ADR-0005](0005-base-de-conocimiento.md) fijó la estructura —un agente, una base, muchos documentos, muchos fragmentos— y quedó escrita de forma compatible con cualquiera de las dos opciones. Pero la elección condiciona el aislamiento entre agentes, el modelo de embeddings y la topología de despliegue, así que necesita decidirse antes de modelar la ingesta.

## 2. Decisión

**Qdrant**, con **una colección por agente**. El identificador de la colección vive en `knowledge_bases`.

PostgreSQL sigue siendo la fuente de verdad de los metadatos (`documents`, `document_chunks`); Qdrant almacena los vectores.

## 3. Justificación

- **Aislamiento por agente, garantizado por la estructura.** F06 establece una base de conocimiento por agente, y toda búsqueda RAG está siempre acotada a un agente. Con una colección por agente ese aislamiento es estructural: no depende de que una consulta recuerde filtrar. Dar de baja un agente es borrar su colección.
- **El filtrado por agente es el caso donde un índice compartido rinde peor.** Restringir una búsqueda por vecinos aproximados a un subconjunto obliga a elegir entre filtrar antes (y degradar el rendimiento) o filtrar después (y degradar la exhaustividad). Separar por colección elimina el dilema.
- **No compite por recursos con la base operativa.** Al ser un servicio aparte, reindexar una base de conocimiento no degrada el inbox en tiempo real (F21). Con el vector store dentro de PostgreSQL, la reindexación compite con las consultas transaccionales de la bandeja.
- **Escalado independiente** del motor de búsqueda respecto de la base operativa.

## 4. Consecuencias

### Positivas

- El aislamiento entre agentes deja de ser una responsabilidad de la aplicación.
- El rendimiento del inbox queda protegido de la carga de ingesta e indexado.
- Borrar o reconstruir la base de un agente es una operación acotada.

### Negativas / costos asumidos

- **Un servicio más** que desplegar, monitorear y respaldar. En el entorno del cooperante (Fase 4) eso es fricción real de operación.
- **Se pierde el join transaccional con PostgreSQL.** La consistencia entre los metadatos y los vectores es **eventual**: la ingesta tiene que manejarla explícitamente, decidir el orden de escritura y contemplar qué pasa si una de las dos falla. Es el costo más importante de esta decisión.
- **Dos backups y dos ciclos de vida** que hay que mantener sincronizados.
- **Para el volumen del cooperante, pgvector probablemente alcanzaría.** Conviene decirlo con honestidad: la decisión no se justifica por escala —100 MB por agente no es un problema de escala— sino por aislamiento y por no degradar la base operativa. Si en la defensa preguntan "¿por qué un servicio más?", esa es la respuesta, y no "porque es más rápido".

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **pgvector** | Una sola base, un solo backup, un solo servicio, y consistencia transaccional entre metadatos y vectores — ventajas reales y nada despreciables. Descartada por el filtrado por agente y por la competencia de recursos con la base operativa. **Reconsiderable** si el despliegue en el entorno del cooperante hiciera costoso sostener un servicio adicional: el cambio afectaría la ingesta y el campo `collection_name`, no el resto del modelo. |
| **Un índice compartido con filtro por `agent_id`** (en cualquiera de los dos) | Convierte el aislamiento en una condición que alguien puede olvidar en una consulta, y expone la base de conocimiento de un agente a otro por un descuido. |
