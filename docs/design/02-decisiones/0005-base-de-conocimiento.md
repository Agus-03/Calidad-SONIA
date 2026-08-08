# ADR-0005 — Base de conocimiento: una por agente, con múltiples documentos

| Campo | Valor |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 2026-08-06 |
| **Etapa** | Diseño — Paso 3 |
| **Resuelve** | Hallazgo H-06 de la [matriz de trazabilidad](../01-trazabilidad.md) |
| **Requisitos afectados** | F06, OE6 |

---

## 1. Contexto

F06 habla de *"ingesta de **documentos** del negocio (límite 100 MB **por agente**)"*. El Anexo C modela `knowledge_base` como **un solo archivo**: tiene `filename` y `size_bytes`. Y `embeddings_count` es un entero suelto que no permite referenciar ningún fragmento concreto.

Hay además una relación duplicada: `agents.knowledge_base_id` y `knowledge_base.agent_id` expresan el mismo vínculo en dos lugares.

## 2. Decisión

**Una jerarquía de tres niveles:**

```
ai_agents 1 ── 1 knowledge_bases 1 ── * documents 1 ── * document_chunks
```

- **Un agente tiene una base de conocimiento.** `knowledge_bases.ai_agent_id` es la **única** dirección de la relación; se elimina `ai_agents.knowledge_base_id`.
- **La base agrupa muchos documentos.** `documents` conserva los metadatos por archivo (nombre, tipo, tamaño, estado de ingesta).
- **Cada documento se fragmenta en `document_chunks`**, que son las unidades recuperables.
- **La base se mapea a una colección del vector store.** El identificador de la colección vive en `knowledge_bases`.
- **El límite de 100 MB de F06 se controla a nivel de base** (suma de sus documentos), no por archivo.

## 3. Justificación

- **F06 dice "documentos" en plural, y el límite es "por agente"**, no por archivo. El modelo actual no puede expresar ninguna de las dos cosas.
- **Una colección de vectores por agente** hace que dar de baja un agente sea borrar una colección, y que toda búsqueda RAG quede acotada al agente por construcción, sin filtrado posterior sobre un índice compartido.
- **`document_chunks` es lo que habilita el registro de recuperación** que OE6 necesita. Sin poder decir *qué fragmento* se recuperó y *con qué score*, no hay forma de distinguir "falló el RAG" de "falló el LLM" — y OE6 mide exactamente eso: 90 % de las consultas del dominio resueltas sin escalada.
- **Una relación en un solo lugar**, según la convención C05. Dos direcciones para el mismo vínculo son una garantía de inconsistencia.

## 4. Consecuencias

### Positivas

- F06 se vuelve implementable tal como está escrito.
- Habilita el registro de recuperación que OE6 necesita para ser medible.
- El aislamiento por agente queda garantizado por la estructura, no por una condición de filtrado que alguien podría olvidar.

### Negativas / costos asumidos

- **Dos entidades más**, y un pipeline de ingesta con estados (pendiente → procesando → indexado → fallido) que hay que construir y probar.
- **Consistencia eventual entre los metadatos y los vectores.** PostgreSQL guarda los metadatos y el vector store los embeddings: no hay transacción que abarque a los dos. La ingesta tiene que manejarlo explícitamente y decidir el orden de escritura.
- La estructura **asume un vector store con colecciones**. Si se optara por uno sin ese concepto, `collection_name` se reemplaza por un filtro y el resto del modelo no cambia — el costo del cambio es bajo.

> **Vector store:** resuelto en [ADR-0010](0010-qdrant-como-base-vectorial.md) — **Qdrant**, con una colección por agente. Esta decisión se escribió antes de esa elección y sigue siendo compatible con cualquier vector store: sólo el campo `collection_name` es específico de uno con colecciones.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Una base por documento** (statu quo del Anexo C) | El límite de F06 es por agente, no por archivo, y no hay dónde referenciar fragmentos. |
| **Base compartida entre varios agentes** (N:M) | Ningún requisito la pide hoy. Es la extensión natural si el cooperante quisiera reutilizar la misma documentación en varios agentes; agregarla después no obliga a rehacer lo demás. |
| **Chunks sin entidad propia**, sólo en el vector store | Ahorra una tabla, pero deja el registro de recuperación de OE6 sin nada a lo que apuntar desde PostgreSQL. |
