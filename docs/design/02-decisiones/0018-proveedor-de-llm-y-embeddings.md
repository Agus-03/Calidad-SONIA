# ADR-0018 — Proveedor de LLM primario y de respaldo, y modelo de embeddings

| Campo | Valor |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 2026-09-04 |
| **Etapa** | Diseño — decisiones heredadas de la sección 8.4 |
| **Resuelve** | Pendiente *"proveedor de LLM primario y de respaldo"* y *"modelo de embeddings"* de la sección 8.4 |
| **Requisitos afectados** | OE2, OE7 · F04, F06, F11, F12, F31 · ADR-0010, ADR-0017 |

---

## 1. Contexto

La sección 8.4 nombra tres candidatos —GPT-4o, Claude y Cohere— y **no elige**. La decisión no es cosmética por dos motivos:

- **OE2 mide consistencia de tono ≥ 80 %**, y el tono depende del modelo. Sin proveedor fijado, la métrica no tiene contra qué medirse.
- **La dimensión de la colección de Qdrant la fija el modelo de embeddings** (ADR-0010), y se define al crear la colección. Cambiarlo después obliga a re-vectorizar toda la base de conocimiento, que puede llegar a 100 MB por agente (F06).

ADR-0017 ya declaró al proveedor de LLM como una de las cuatro fronteras con puerto y adaptador (`adapters/llm/base.py` + `registry.py`).

## 2. Decisión

- **Proveedor primario: Cohere.**
- **Proveedor de respaldo: Anthropic.**
- **Embeddings: un modelo de Cohere.**
- **Queda abierto el modelo exacto de embeddings y su dimensión.** Hay que fijarlo **antes de crear la colección de Qdrant**, y escribirlo junto a esta decisión.

El respaldo se resuelve por el puerto ya previsto: es un adaptador y una variable de configuración, no un refactor.

## 3. Justificación

- **Los dos están entre los tres que nombra 8.4**, así que la elección no sale del anteproyecto.
- **Dos proveedores distintos vuelven real al puerto de ADR-0017.** Con una sola implementación, el puerto es especulativo y su regla 11 ("no se inventan puertos donde no hay dos implementaciones a la vista") no se cumpliría; con dos, se prueba solo.
- **Embeddings del mismo proveedor que el primario** reduce una cuenta, un SDK y un modo de falla.

> **Nota honesta:** el motivo de fondo por el que se eligió Cohere sobre los otros dos candidatos **no quedó registrado en la discusión que produjo este ADR**. Conviene agregarlo antes de la defensa: una decisión sin su porqué es, seis meses después, indistinguible de un accidente.

## 4. Consecuencias

### Positivas

- OE2 tiene un modelo concreto contra el cual medir, y `generations.llm_provider`/`llm_model` congelan cuál se usó en cada turno.
- El puerto de LLM tiene dos implementaciones reales desde el inicio.
- Una sola cuenta y un solo SDK para generación y embeddings.

### Negativas / costos asumidos

- **Cambiar el modelo de embeddings obliga a reindexar todo.** Es lo más caro de revertir de este ADR, y por eso el modelo exacto es un pendiente con fecha límite: antes de crear la colección.
- **Generación y recuperación comparten proveedor.** Una caída de Cohere afecta las dos: el respaldo de Anthropic cubre la generación, **no la búsqueda semántica**.
- Fijar un primario **no exime de correr la comparación entre proveedores** que 8.4 y OE2 insinúan; simplemente define el que corre en producción.
- Hasta que no se fije el modelo exacto, **la fase de RAG está bloqueada**: no se puede crear la colección.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Un único proveedor sin respaldo** | OE7 mide producción real: una caída del proveedor deja al sistema sin ninguna respuesta, y la salida es una variable de entorno si el puerto existe. |
| **Embeddings de otro proveedor** | Agrega una cuenta, un SDK y un modo de falla más sin beneficio identificado. Se revisa si la calidad multilingüe en español no alcanza. |
| **Fijar el modelo exacto ahora, sin evaluarlo** | La dimensión es lo más caro de revertir del modelo: conviene elegirlo con una prueba de recuperación en español encima, no de memoria. |
