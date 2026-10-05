# ADR-0078 — La evaluación de recuperación no compara modelos de embeddings

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · el modelo de embeddings es `voyage-4` y no se compara con otros, decidido por el grupo el 2026-10-04 (*"si tiene que ver con elegir modelo, se hace con voyage, no cambia eso"*) y ratificado el 2026-10-05 |
| **Fecha** | 2026-10-05 |
| **Etapa** | Construcción — Fase 12 (conocimiento) |
| **Resuelve** | La contradicción que encontró la revisión de diseño del cierre de la Fase 12: [ADR-0075](0075-medicion-de-oe6-y-evaluacion-de-recuperacion.md) registró que la comparación de modelos de [ADR-0067](0067-modelo-de-embeddings-e-imagenes.md), regla 3, entraba en la evaluación diferida, mientras la marca en ADR-0067 decía que quedaba para decidir |
| **Requisitos afectados** | F06 · OE6 |
| **Enmienda** | [ADR-0067](0067-modelo-de-embeddings-e-imagenes.md), **regla 3** — deja de aplicar: no se compara `voyage-4` con `embed-multilingual-v3.0` ni con `text-embedding-3-small`, y con eso la mención de esa comparación en su estado y en sus alternativas descartadas · [ADR-0075](0075-medicion-de-oe6-y-evaluacion-de-recuperacion.md), **celda *Enmienda*** — su enmienda de ADR-0067, regla 3 (*"la comparación de modelos usa la misma evaluación diferida"*), queda sin objeto · [ADR-0065](0065-fragmentacion-de-documentos.md), **§4** — la evaluación ya no sirve *"para comparar modelos de embeddings"* |
| **Relacionada con** | [ADR-0018](0018-proveedor-de-llm-y-embeddings.md), [ADR-0065](0065-fragmentacion-de-documentos.md), [ADR-0067](0067-modelo-de-embeddings-e-imagenes.md), [ADR-0075](0075-medicion-de-oe6-y-evaluacion-de-recuperacion.md) |

---

## 1. Contexto

[ADR-0067](0067-modelo-de-embeddings-e-imagenes.md), regla 3, decidió arrancar con `voyage-4` y compararlo después con `embed-multilingual-v3.0` de Cohere y con `text-embedding-3-small` de OpenAI, usando la evaluación de recuperación de [ADR-0065](0065-fragmentacion-de-documentos.md).

El 2026-10-04, al fijar los criterios de esa evaluación, el grupo respondió: *"Si tiene que ver con elegir modelo, se hace con voyage, no cambia eso"*. [ADR-0075](0075-medicion-de-oe6-y-evaluacion-de-recuperacion.md) lo registró al revés, como si la comparación siguiera dentro de la evaluación diferida. La marca en ADR-0067 decía que quedaba para decidir al armarla.

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **El modelo de embeddings es `voyage-4` a 1024 dimensiones, y no se compara con otros.** La evaluación diferida de ADR-0075 mide la recuperación con `voyage-4` y nada más |
| 2 | **Cambiar de modelo sigue siendo posible**, pero necesita un ADR propio que lo justifique, y reindexar ([ADR-0064](0064-ingesta-de-documentos.md), regla 11) |

## 3. Justificación

- **Es lo que decidió el grupo.** La respuesta del 2026-10-04 dice que la elección del modelo está hecha.
- **Una comparación sobre un solo documento elegiría el modelo con poca evidencia.** Con el anteproyecto como único documento de prueba ([ADR-0075](0075-medicion-de-oe6-y-evaluacion-de-recuperacion.md), regla 4), la *recall@k* sale alta para cualquier modelo, y la diferencia entre tres no diría mucho.
- **Comparar cuesta dos proveedores más.** Haría falta una clave de Cohere y otra de OpenAI, y mandarles el documento de prueba, para una decisión que el grupo ya tomó.

## 4. Consecuencias

### Positivas

- La evaluación diferida es más chica: una sola corrida por configuración, con un solo proveedor.
- El documento de prueba no sale a Cohere ni a OpenAI.

### Negativas / costos asumidos

- **`voyage-4` no se valida contra nada.** Que recupere bien en español se supone por la recomendación de Anthropic y por ser la generación actual ([ADR-0067](0067-modelo-de-embeddings-e-imagenes.md) §3), no por una medición.
- **Si `voyage-4` recupera mal, no hay un candidato medido** con el que reemplazarlo. Cambiar exige primero una comparación que hoy no se hace.
- **Contradice una regla aceptada de ADR-0067** y una enmienda de ADR-0075. Se registran como enmienda.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Mantener la comparación dentro de la evaluación diferida**, como registró ADR-0075 | El grupo decidió que el modelo es Voyage |
| **Dejarla para decidir al armar la evaluación**, como decía la marca en ADR-0067 | Deja abierta una decisión que el grupo ya tomó |
