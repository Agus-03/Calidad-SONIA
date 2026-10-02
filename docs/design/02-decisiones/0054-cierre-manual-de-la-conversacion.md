# ADR-0054 — El cierre manual de una conversación: quién cierra, qué cambia y qué pasa con un turno en vuelo

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · quién cierra (regla 2), no tocar el modo (regla 4) y rechazar el cierre con un turno en vuelo (regla 7) los eligió el grupo el 2026-10-01 |
| **Fecha** | 2026-10-01 |
| **Etapa** | Construcción — Fase 11 (resto del CRUD operativo) |
| **Resuelve** | El pendiente del README *"El cierre manual de conversaciones"*: quién cierra, qué pasa con el modo del bot y con el vendedor, y qué pasa si el bot todavía está respondiendo |
| **Requisitos afectados** | F23 · F22 · F13 · F34 · OE7 · Anexo B |
| **Enmienda** | [ADR-0051](0051-bandeja-en-tiempo-real.md), regla 13 — `inbox/` también publica al cerrar · [ADR-0050](0050-filtros-de-la-bandeja.md) §1 y §4 — *resueltas* deja de devolver vacío |
| **Relacionada con** | [ADR-0002](0002-ciclo-de-vida-conversacion-y-recordatorios.md), [ADR-0035](0035-reglas-de-la-bandeja-manual.md), [ADR-0037](0037-toda-conversacion-nace-con-vendedor.md), [ADR-0041](0041-temporizador-del-turno-por-poller.md), [ADR-0052](0052-marca-de-leida.md), [ADR-0053](0053-estrella-de-seguimiento.md) |

---

## 1. Contexto

[ADR-0002](0002-ciclo-de-vida-conversacion-y-recordatorios.md) (d) decidió que *"el cierre es manual, por el momento. Lo hace el operador desde la bandeja"*, y (b) que un mensaje sobre una conversación resuelta crea otra nueva. Nunca se construyó: hasta la Fase 10 nada escribía `status = 'resolved'`, y el filtro *resueltas* de F23 devolvía vacío ([ADR-0050](0050-filtros-de-la-bandeja.md) §4).

No es sólo F23. F13 califica conversaciones cerradas, F34 las cuenta y OE7 las usa como denominador. El esquema ya trae `resolved_at` y `resolved_by_operator_id` desde `0001`.

ADR-0002 no dice quién cierra, qué pasa con el bot y el vendedor, ni qué pasa si el bot está armando una respuesta en ese momento. Esto último no es teórico: cuando un turno termina sin respuesta, el núcleo deja la conversación en `waiting` aunque alguien la haya resuelto, y si el cliente ya abrió otra, choca con el índice único que sólo admite una conversación abierta por contacto.

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **`POST /api/v1/conversations/:id/resolve`**, sin cuerpo, responde 204. Abierta a los tres roles |
| 2 | **Cierra quien puede prender o apagar el bot** ([ADR-0037](0037-toda-conversacion-nace-con-vendedor.md), regla 4): el `operator`, las suyas; `supervisor` y `admin`, cualquiera de la cuenta. Elegido por el grupo el 2026-10-01. Fuera de alcance es un 404 `conversation_not_found` ([ADR-0035](0035-reglas-de-la-bandeja-manual.md), regla 3); una del pool, para el `operator`, es un 409 `conversation_not_assigned`: primero tiene que tomarla |
| 3 | **Se cierra desde `active` o desde `waiting`.** Una ya resuelta es un 409 `conversation_resolved`: no se reabre ni se cierra dos veces (ADR-0002 (b)). De dos cierres a la vez, escribe uno y el otro recibe ese 409 |
| 4 | **El cierre no toca el modo del bot.** Elegido por el grupo el 2026-10-01 |
| 5 | **Escribe sólo `status`, `resolved_at` y `resolved_by_operator_id`.** El vendedor se queda, porque el reparto de la próxima conversación del cliente lo busca ([ADR-0037](0037-toda-conversacion-nace-con-vendedor.md), regla 1). No se mueven `last_activity_at` ni `unread_count`, por lo mismo que la estrella ([ADR-0053](0053-estrella-de-seguimiento.md), regla 7) |
| 6 | **Cerrar no asigna** ([ADR-0035](0035-reglas-de-la-bandeja-manual.md), regla 13): asigna responder, y cerrar no es responder |
| 7 | **Con un turno en vuelo, el cierre se rechaza con un 409 `turn_in_progress`.** Cuenta como en vuelo un bloque de [ADR-0041](0041-temporizador-del-turno-por-poller.md) abierto (el cliente sigue escribiendo), o uno cerrado sin `processed_at` ni `error_message` hace menos que `TURN_BUDGET_MS`. Elegido por el grupo el 2026-10-01 |
| 8 | **El frontend no ofrece cerrar mientras espera la respuesta del bot, y el 409 es el respaldo.** La API no expone un campo con el turno en vuelo: el frontend se entera por el 409. Elegido por el grupo el 2026-10-01 |
| 9 | **El chequeo de la regla 7 va en la misma sentencia que escribe el cierre**, no en una lectura previa |
| 10 | **`inbox/` publica `conversation.updated` después del commit** ([ADR-0051](0051-bandeja-en-tiempo-real.md), regla 13). El cliente no recibe ningún aviso |

## 3. Justificación

- **El alcance del interruptor (regla 2), y no el de responder ni el del hilo.** Cerrar es un acto sobre la atención, como prender o apagar el bot: el vendedor responde por lo suyo, y quienes supervisan al equipo pueden cerrar lo que quedó colgado. El alcance de responder ([ADR-0052](0052-marca-de-leida.md)) no dejaría al `supervisor` cerrar la de un vendedor ausente, y el del hilo ([ADR-0053](0053-estrella-de-seguimiento.md)) dejaría a cualquier `operator` cerrar una del pool que nadie atendió.
- **El modo no se toca (regla 4)** porque la conversación cerrada no vuelve a recibir mensajes: el próximo mensaje abre otra, que nace con su propio modo (ADR-0002 (b), [ADR-0038](0038-alta-baja-y-pausa-del-agente-ia.md), regla 15). Apagarlo sería una escritura sin efecto.
- **El vendedor se queda (regla 5)** porque [ADR-0037](0037-toda-conversacion-nace-con-vendedor.md), regla 1, le da la próxima conversación del cliente al vendedor de la anterior. Sacarlo al cerrar mandaría esa conversación al reparto.
- **Rechazar con un turno en vuelo (regla 7)** porque las otras dos salidas son peores. Dejarlo como estaba hace que el final del turno reabra una conversación resuelta, y puede romper el índice único si el cliente ya abrió otra. Cerrar igual y que el turno se calle obligaba a tocar `turn/`, que hoy nunca lee `status`.
- **El tope de `TURN_BUDGET_MS`** distingue un turno que todavía puede terminar de uno que mató una caída. Pasado ese plazo, el orquestador ya canceló su contexto, así que el bloque sin marcar es un turno muerto y no debe trabar el cierre para siempre.
- **En la misma sentencia (regla 9)**, para que un turno que termina justo antes del cierre se vea, en vez de confiar en una lectura de hace un instante.

## 4. Consecuencias

### Positivas

- El filtro *resueltas* de F23 tiene datos, y F13, F34 y OE7 tienen conversaciones cerradas para leer.
- Queda registrado quién cerró y cuándo.
- Un turno en vuelo ya no puede reabrir una conversación que alguien acaba de cerrar.

### Negativas / costos asumidos

- **Queda una ventana que la regla 7 no cubre.** El mensaje del cliente se guarda en la transacción del webhook, y su bloque se arma en otra, inmediatamente después. Un cierre que cae entre las dos no ve ningún turno y pasa. En modo autónomo el bot responde igual sobre la conversación resuelta; si el turno no responde, intenta dejarla en `waiting` y, si el cliente ya abrió otra, el índice único lo rechaza y el turno queda marcado como fallido. Cerrarla exige tocar `turn/`.
- **El cierre puede quedar trabado hasta `TURN_BUDGET_MS`** después de que un turno se cayó, aunque nadie lo esté ejecutando.
- **Mientras el cliente escribe, no se puede cerrar.** El bloque abierto cuenta como turno en vuelo, aunque el operador quiera cerrar justamente porque el cliente se despidió.
- **El frontend no sabe de antemano si hay un turno en vuelo** (regla 8). Puede bloquear el botón mientras espera la respuesta que él mismo ve llegar, pero un turno que empezó en otra pestaña o por otro mensaje sólo aparece como el 409.
- **`conversations/` lee `message_batches`, que es de `turn/`.** La [regla 1 de la estructura](../08-structure-rules.md) prohíbe escribir la tabla de otro, no leerla, y `SetMode` ya lee `ai_agents` en su predicado; pero el agregado queda acoplado a cómo el turno marca sus bloques.
- **El cierre no tiene vuelta atrás.** Un cierre por error no se deshace: la conversación queda resuelta y el próximo mensaje abre otra (ADR-0002 (b)).
- **[H-25](../01-trazabilidad.md) sigue a la vista**: `resolved_by_operator_id` le acredita el cierre a quien apretó el botón, aunque la haya atendido la IA.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Cerrar igual con un turno en vuelo, y que el turno no responda ni reabra** | Exige que `turn/` lea `status` en cada paso, y una respuesta que ya salió no se puede retirar |
| **No hacer nada con el turno en vuelo** | El final del turno reabre la resuelta y puede violar el índice único por contacto |
| **Un campo `turn_in_progress` en la fila de la bandeja** | Haría que el frontend supiera de antemano, pero agrega un dato a la fila y a cada evento de ADR-0051. El grupo prefirió el 409 |
| **El alcance de responder** ([ADR-0052](0052-marca-de-leida.md)) | El `supervisor` no podría cerrar la conversación de un vendedor ausente |
| **El alcance del hilo** ([ADR-0053](0053-estrella-de-seguimiento.md)) | Cualquier `operator` podría cerrar una del pool que nadie atendió |
| **Apagar el bot al cerrar** | La conversación cerrada no vuelve a recibir mensajes: sería una escritura sin efecto |
| **Sacar al vendedor al cerrar** | Mandaría la próxima conversación del cliente al reparto, en contra de [ADR-0037](0037-toda-conversacion-nace-con-vendedor.md), regla 1 |
| **Un `PATCH` al estado** con `{"status": "resolved"}` | Sugiere que se puede volver a `active`, y ADR-0002 (b) no lo permite |
