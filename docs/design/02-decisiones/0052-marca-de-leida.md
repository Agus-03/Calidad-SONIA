# ADR-0052 — La marca de leída: qué cuenta como no leído, qué lo borra y quién puede borrarlo

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · qué marca leída (§2.2, regla 4) y quién puede marcarla (regla 6) los eligió el grupo el 2026-09-26 |
| **Fecha** | 2026-09-26 |
| **Etapa** | Construcción — Fase 10 (bandeja en tiempo real) |
| **Resuelve** | El pendiente *"Qué marca una conversación como leída"* del README, y con él el filtro *no leídas* de F23, que devolvía vacío |
| **Requisitos afectados** | F23 · F22 · F21 · Anexo B |
| **Enmienda** | [ADR-0050](0050-filtros-de-la-bandeja.md) §4 — *no leídas* deja de devolver vacío · [ADR-0050](0050-filtros-de-la-bandeja.md) §5 — revierte en parte la alternativa descartada *"Sumar a esta fase la escritura de `unread_count`"* · [ADR-0051](0051-bandeja-en-tiempo-real.md) §4 — la fila del evento ya dice cuántos mensajes hay sin leer, así que con el filtro *no leídas* el frontend sabe si la fila sigue entrando · [ADR-0051](0051-bandeja-en-tiempo-real.md), regla 13 — marcar leída se suma a los que publican |
| **Relacionada con** | [ADR-0020](0020-idempotencia-de-los-mensajes-salientes.md), [ADR-0035](0035-reglas-de-la-bandeja-manual.md), [ADR-0036](0036-paginacion-por-cursor-en-la-bandeja.md), [ADR-0037](0037-toda-conversacion-nace-con-vendedor.md) |

---

## 1. Contexto

F23 pide filtrar la bandeja por *no leídas*. El esquema tiene `conversations.unread_count` y `conversations.last_read_at` desde `0001`, pero hasta la Fase 10 nada las escribía, así que el filtro de [ADR-0050](0050-filtros-de-la-bandeja.md) devolvía siempre vacío.

El anteproyecto no define qué es leer una conversación. Hay que decidir tres cosas:

- **Qué suma:** qué mensajes cuentan como no leídos.
- **Qué borra:** qué acción marca la conversación como leída.
- **Quién puede borrar.** La columna es una sola por conversación, así que la marca es compartida: si uno la borra, la borra para todos.

## 2. Decisión

### 2.1 Qué suma

| # | Regla |
|---|---|
| 1 | **Cada mensaje del cliente suma uno**, en la misma sentencia que mueve la ventana de 24 horas, dentro de la transacción del webhook |
| 2 | **Un reenvío de Meta no suma**: el `wamid` duplicado se rechaza antes de llegar a esa sentencia |
| 3 | **La respuesta del bot no borra nada, y tampoco el texto fijo del sistema** que sale cuando se corta un turno. Que algo automático haya contestado no significa que el vendedor leyó lo que escribió el cliente |

### 2.2 Qué borra

| # | Regla |
|---|---|
| 4 | **`POST /api/v1/conversations/:id/read`**, que el frontend manda cuando el hilo está en pantalla. Pone `unread_count = 0` y `last_read_at = now()`, y responde 204. Elegido por el grupo el 2026-09-26 |
| 5 | **Responder también marca leída**, en la misma transacción que la respuesta: quien contestó, leyó |

### 2.3 Quién puede borrar

| # | Regla |
|---|---|
| 6 | **Puede marcarla leída quien puede responderla** ([ADR-0035](0035-reglas-de-la-bandeja-manual.md), regla 5): quien la tiene asignada, o cualquiera si está en el pool. Elegido por el grupo el 2026-09-26 |
| 7 | **Los rechazos de alcance son los de responder.** Los otros no: una conversación resuelta se marca (regla 8), aunque no se pueda responder. Un `supervisor` o un `admin` sobre la conversación de un vendedor reciben 409 `conversation_taken`; un `operator` sobre la de otro, 404 `conversation_not_found` (ADR-0035, reglas 3 y 6) |
| 8 | **Una conversación sin nada sin leer no se escribe**, y no se le avisa a nadie. Una resuelta se marca como cualquier otra |
| 9 | **Marcar leída no asigna**, igual que cambiar el modo ([ADR-0035](0035-reglas-de-la-bandeja-manual.md), regla 13) o la estrella ([ADR-0053](0053-estrella-de-seguimiento.md), regla 6) |

### 2.4 Qué se ve

| # | Regla |
|---|---|
| 10 | **`unread_count` va en la fila del listado**, y por eso también en el evento `conversation.updated` de [ADR-0051](0051-bandeja-en-tiempo-real.md) |
| 11 | **Marcar leída publica el cambio** para que la bandeja de los demás se actualice. **No mueve `last_activity_at`**: leer no es actividad del canal, y mover la fila correría los cursores en circulación ([ADR-0036](0036-paginacion-por-cursor-en-la-bandeja.md)) |

## 3. Justificación

- **Un pedido explícito (regla 4), y no leer el hilo.** Que `GET …/messages` escribiera la marca rompería la semántica de HTTP: un GET no cambia nada. Además, marcaría también al paginar hacia atrás en un hilo viejo. El frontend es el único que sabe si el hilo está en pantalla.
- **Quién responde es quién borra (regla 6).** El contador es un aviso para el vendedor. Si el `supervisor` pudiera borrarlo con sólo mirar, el vendedor perdería el aviso de mensajes que nunca vio. Usar la regla de responder evita inventar un tercer alcance: la bandeja ya tiene uno para leer y otro para responder.
- **Contar en la misma sentencia (regla 1)** hace que el contador no pueda desalinearse del mensaje: los dos se confirman juntos o ninguno.
- **Responder marca leída (regla 5)** porque, si no, el vendedor tendría que marcar a mano cada conversación que acaba de contestar.
- **Revierte en parte un descarte de [ADR-0050](0050-filtros-de-la-bandeja.md) §5**, que dejó afuera escribir `unread_count` en esta fase porque ningún requisito la ubicaba acá. El grupo la sumó a la Fase 10 junto con F21, porque la marca y el evento en tiempo real se tocan en el mismo lugar. El cierre manual, la otra mitad de aquel descarte, sigue afuera: va a la Fase 11.

## 4. Consecuencias

### Positivas

- El filtro *no leídas* de F23 funciona, y la bandeja puede mostrar el número de mensajes sin leer.
- El evento de F21 lleva el contador, así que con el filtro *no leídas* el frontend ya no tiene que volver a pedir la página.
- El `supervisor` puede revisar la conversación de un vendedor sin borrarle el aviso.

### Negativas / costos asumidos

- **La marca es compartida.** Cuando el vendedor la lee, deja de contar como no leída también para el `supervisor`, aunque él no la haya visto. Una marca por persona necesitaría una tabla nueva.
- **Una conversación del pool la puede borrar cualquier operador** con sólo abrirla, sin tomarla. Los demás pierden el aviso aunque nadie la haya atendido.
- **El frontend tiene que acordarse de marcar.** Si no manda el `POST`, el contador sólo baja cuando alguien responde.
- **El `supervisor` que pide marcar la de un vendedor recibe un 409.** El frontend tiene que saber no pedirlo, o ignorar ese rechazo.
- **Las conversaciones que existían antes de esta fase arrancan en cero**, aunque tengan mensajes del cliente sin contestar: la columna nunca se había escrito.
- **`last_read_at` no dice lo mismo según quién lo escribió.** Con el `POST`, no se mueve si no había nada sin leer (regla 8). Al responder, se escribe siempre (regla 5), haya o no algo sin leer. Así que es la última vez que alguien respondió o que borró un aviso pendiente, no la última vez que alguien abrió la conversación.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Marcar leída al leer el hilo** (`GET …/messages`) | Un GET que escribe, y que también marca al paginar hacia atrás |
| **Marcar leída sólo al responder** | Mirar sin contestar la dejaría como no leída. El vendedor no tendría forma de decir *"ya la vi"* |
| **Que la marque cualquiera que la vea** | Un `supervisor` que revisa le borra el aviso al vendedor |
| **Una marca por Operator**, en una tabla nueva | Resuelve la marca compartida, pero agrega una tabla y una migración para algo que F23 no pide por persona |
| **Que la respuesta del bot también marque leída** | El vendedor perdería el aviso de lo que escribió el cliente, aunque nunca lo haya leído |
