# ADR-0055 — El puntaje de una conversación: la escala, quién califica y qué pasa al calificar de nuevo

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · la escala de 1 a 5 (regla 2), que califique sólo el vendedor (regla 3) y que una calificación nueva pise la anterior (regla 5) los eligió el grupo el 2026-10-01 |
| **Fecha** | 2026-10-01 |
| **Etapa** | Construcción — Fase 11 (resto del CRUD operativo) |
| **Resuelve** | Cómo se construye F13: qué escala tiene el puntaje, quién lo pone, sobre qué conversaciones y qué pasa si se vuelve a calificar |
| **Requisitos afectados** | F13 · F16 · F22 · Anexo B |
| **Enmienda** | [ADR-0051](0051-bandeja-en-tiempo-real.md), regla 13 — `inbox/` también publica al calificar |
| **Relacionada con** | [ADR-0002](0002-ciclo-de-vida-conversacion-y-recordatorios.md), [ADR-0025](0025-atribucion-por-operador.md), [ADR-0035](0035-reglas-de-la-bandeja-manual.md), [ADR-0037](0037-toda-conversacion-nace-con-vendedor.md), [ADR-0053](0053-estrella-de-seguimiento.md), [ADR-0054](0054-cierre-manual-de-la-conversacion.md) |

---

## 1. Contexto

F13 dice *"El operador califica cada conversación cerrada. Alimenta métricas de efectividad"*. El esquema trae desde `0001` las tres columnas que hacen falta: `quality_score`, `scored_by_operator_id` y `scored_at`. Las dos últimas cierran el hueco que la [matriz de trazabilidad](../01-trazabilidad.md) señalaba para F13 (faltaban el autor y la fecha).

Hasta la Fase 11 no se podía construir: no existía el cierre. [ADR-0054](0054-cierre-manual-de-la-conversacion.md) lo construyó.

F13 no dice qué escala tiene el puntaje, y la columna no tenía ningún `CHECK`. Tampoco dice cuál *"operador"*: el anteproyecto usa la palabra para la persona que atiende, pero el Anexo B distingue tres roles. Y no dice si una calificación se puede cambiar. F16 promedia este puntaje por operador ([ADR-0025](0025-atribucion-por-operador.md)), así que la escala tiene que quedar fija antes de cualquier reporte.

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **`PUT /api/v1/conversations/:id/score`**, con `{"score": n}`, responde 200 con el puntaje que quedó. Abierta a los tres roles. Un cuerpo sin `score`, o con algo que no es un entero, es un 400 `invalid_body` |
| 2 | **La escala es de 1 a 5, como estrellas.** Un número fuera de ella es un 400 `invalid_score`, y el esquema lo rechaza también, con un `CHECK` en `conversations.quality_score`. Elegido por el grupo el 2026-10-01 |
| 3 | **Califica sólo el vendedor de la conversación**, el que figura en `operator_id`, cualquiera sea su rol. Elegido por el grupo el 2026-10-01. Un `supervisor` o un `admin` sobre la conversación de otro, o cualquiera sobre una que cerró en el pool, recibe un 409 `not_the_vendor`. Para un `operator`, la de otro vendedor sigue siendo un 404 `conversation_not_found` ([ADR-0035](0035-reglas-de-la-bandeja-manual.md), regla 3) |
| 4 | **Sólo se califica una conversación resuelta**, como dice F13. Una `active` o `waiting` es un 409 `conversation_not_resolved` |
| 5 | **Calificar de nuevo pisa la calificación anterior**, con su autor y su fecha. No queda historial. Elegido por el grupo el 2026-10-01 |
| 6 | **Calificar no asigna** ([ADR-0035](0035-reglas-de-la-bandeja-manual.md), regla 13) **ni mueve `last_activity_at`**, por lo mismo que la estrella ([ADR-0053](0053-estrella-de-seguimiento.md), regla 7) |
| 7 | **`quality_score` va en la fila de la bandeja**, `null` mientras nadie califica, así que llega con el evento de [ADR-0051](0051-bandeja-en-tiempo-real.md) |
| 8 | **`inbox/` publica `conversation.updated` después del commit** ([ADR-0051](0051-bandeja-en-tiempo-real.md), regla 13) |
| 9 | **El vendedor y el estado están en el predicado del `UPDATE`**, no sólo en la lectura previa |

## 3. Justificación

- **Una escala de 1 a 5 (regla 2)** es la que cualquiera reconoce como estrellas, y deja un punto medio. **El `CHECK` en el esquema** hace que un puntaje fuera de la escala no pueda entrar por ningún camino, porque F16 promedia la columna directamente.
- **Sólo el vendedor (regla 3)** porque es quien atendió la conversación, y F13 le pide a quien atiende que juzgue su conversación. Un `supervisor` que califica las de su equipo estaría evaluando a otro, y eso es otra métrica, que F13 no pide.
- **Sólo resueltas (regla 4)** porque F13 dice *"cerrada"*: el puntaje juzga un trabajo terminado. [ADR-0002](0002-ciclo-de-vida-conversacion-y-recordatorios.md) ya lo había usado como uno de los motivos para que las conversaciones se cierren.
- **Pisar (regla 5)** porque el esquema tiene un solo puntaje por conversación, y el grupo prefirió dejar corregir un error antes que fijar el primero.
- **En el predicado (regla 9)**, para que el chequeo no dependa de una lectura de hace un instante: el día que F26 reasigne, una conversación puede cambiar de vendedor entre la lectura y la escritura.

## 4. Consecuencias

### Positivas

- F13 existe con un contrato cerrado, y F16 tiene un puntaje con una escala fija para promediar.
- El autor y la fecha de la calificación quedan registrados, que era el hueco de la matriz de trazabilidad.
- El frontend ve el puntaje en la fila sin abrir la conversación.

### Negativas / costos asumidos

- **Una conversación que se cerró en el pool no la puede calificar nadie.** No tiene vendedor, y la regla 3 no deja calificar a otro. Con [ADR-0037](0037-toda-conversacion-nace-con-vendedor.md) casi toda conversación tiene vendedor, pero las que quedan en el pool —porque no había vendedores— quedan fuera del promedio de F16.
- **Un vendedor desactivado no puede calificar las suyas**, y nadie más puede hacerlo por él.
- **El vendedor se califica a sí mismo.** El puntaje mide cómo cree el vendedor que le fue, no cómo le fue: F16 promedia una autoevaluación.
- **Una calificación nueva borra la anterior** (regla 5). No se puede saber si un puntaje se cambió ni cuántas veces, y un vendedor puede subir sus puntajes después de un reporte sin que quede rastro.
- **Las conversaciones sin calificar no tienen puntaje.** Si el vendedor no califica, el promedio de F16 sale de las que sí calificó, que no tienen por qué ser representativas.
- **El `CHECK` entró en `0001`.** Por la regla de [`migrations/README.md`](../../../src/core/migrations/README.md), todavía se puede reescribir, pero cada base existente tiene que recrearse (`docker compose down -v`), porque el núcleo no arranca con un checksum distinto.
- **Si F26 reasigna una conversación resuelta**, el puntaje que puso el vendedor anterior queda, y el nuevo vendedor puede pisarlo.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Una escala de 1 a 10, o de 0 a 100** | El grupo eligió de 1 a 5, como estrellas |
| **Que califiquen también `supervisor` y `admin`**, con el alcance del cierre | Evaluarían el trabajo de otro, que es una métrica distinta de la de F13 |
| **Que califique cualquiera que vea la conversación** | Mezcla en un mismo promedio la opinión del vendedor y la de quien supervisa |
| **Una sola calificación, sin poder cambiarla** | El grupo prefirió poder corregir un error |
| **Guardar el historial de calificaciones** en una tabla nueva | Agrega una tabla y una migración que F13 no pide; el grupo aceptó perder el historial |
| **Calificar conversaciones abiertas** | F13 dice *"cerrada"* |
| **`PATCH`**, como la estrella y el modo | Calificar de nuevo reemplaza el puntaje entero (regla 5), y eso es lo que dice `PUT` |
