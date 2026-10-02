# ADR-0051 — La bandeja en tiempo real: cómo se conecta el operador, qué se le empuja y cuándo

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · la librería (§2.1) y la autenticación por ticket (§2.2) las eligió el grupo el 2026-09-26 · **§4 y regla 13 enmendados por [ADR-0052](0052-marca-de-leida.md)** (2026-09-26): la fila del evento lleva `unread_count`, así que el costo de los filtros queda sólo para *por etiqueta* y *con recordatorio*, y marcar leída se suma a los que publican · **regla 13 enmendada por [ADR-0054](0054-cierre-manual-de-la-conversacion.md)** (2026-10-01): cerrar una conversación también publica · **regla 13 enmendada por [ADR-0055](0055-puntaje-de-la-conversacion.md)** (2026-10-01): calificar una conversación también publica |
| **Fecha** | 2026-09-26 |
| **Etapa** | Construcción — Fase 10 (bandeja en tiempo real) |
| **Resuelve** | Cómo implementa el núcleo F21: con qué librería, cómo se autentica una conexión que el navegador abre sin header `Authorization`, qué eventos existen, quién los publica y qué pasa cuando algo falla |
| **Requisitos afectados** | F21 · F22 · F23 · F25 · F27 · Anexo B |
| **Enmienda** | [ADR-0030](0030-autenticacion-de-operadores.md), regla 2 y §4 — en el socket, `role` y `active` se leen una vez al abrirlo y no en cada evento, así que la revocación no es inmediata para un socket abierto (§4) · [ADR-0038](0038-alta-baja-y-pausa-del-agente-ia.md) §4, *"Pausar deja al cliente sin respuesta automática"* — ahora la pausa y la baja sí le avisan en el momento al vendedor de cada conversación cuyo bot apagaron (§2.5) |
| **Relacionada con** | [ADR-0017](0017-decomposicion-en-despliegues-y-lenguajes.md), [ADR-0027](0027-nucleo-de-instancia-unica.md), [ADR-0029](0029-el-acuse-del-webhook-no-espera-al-turno.md), [ADR-0030](0030-autenticacion-de-operadores.md), [ADR-0035](0035-reglas-de-la-bandeja-manual.md), [ADR-0036](0036-paginacion-por-cursor-en-la-bandeja.md), [ADR-0050](0050-filtros-de-la-bandeja.md) |

---

## 1. Contexto

F21 pide que el backend *"recibe webhooks de Meta y los distribuye en tiempo real a los operadores conectados mediante WebSockets"*. [ADR-0017](0017-decomposicion-en-despliegues-y-lenguajes.md) lo ubicó en el núcleo Go, dentro de `inbox/`, y [ADR-0027](0027-nucleo-de-instancia-unica.md) declaró que el fan-out vive en la memoria de una única instancia.

Hasta la Fase 10 la bandeja no avisaba nada: el frontend tenía que volver a pedir la primera página para enterarse ([ADR-0036](0036-paginacion-por-cursor-en-la-bandeja.md) §4).

Quedaban cuatro preguntas abiertas:

- **La librería.** La biblioteca estándar de Go no trae un servidor WebSocket, y cada dependencia nueva del núcleo es una decisión de arquitectura.
- **La autenticación.** El navegador no puede mandar el header `Authorization` al abrir un WebSocket, y todas las rutas de `/api/v1` lo exigen ([ADR-0030](0030-autenticacion-de-operadores.md)).
- **Qué se empuja.** F21 habla de *webhooks*, pero la bandeja también cambia cuando responde el bot, cuando responde un operador, cuando se cambia el modo o la estrella, y cuando se pausa un agente IA.
- **A quién.** Cada rol ve una parte distinta de la bandeja ([ADR-0035](0035-reglas-de-la-bandeja-manual.md), regla 1).

## 2. Decisión

### 2.1 Librería

| # | Regla |
|---|---|
| 1 | **`github.com/coder/websocket`.** Recibe un `context` en cada operación, como el resto del núcleo, y su API es chica. Elegida por el grupo el 2026-09-26 |

### 2.2 Autenticación: un ticket de un solo uso

| # | Regla |
|---|---|
| 2 | **`POST /api/v1/inbox/stream-ticket`**, detrás del bearer y abierta a los tres roles, devuelve un ticket aleatorio de 32 bytes que **vence a los 30 segundos y sirve una sola vez**. Elegido por el grupo el 2026-09-26 |
| 3 | **`GET /inbox/stream?ticket=…`** abre el socket. **Está fuera de `/api/v1`**, como el webhook, porque el router exige el bearer en toda esa zona. El ticket es la credencial |
| 4 | **El ticket se gasta al presentarse**, sea válido o no. Uno desconocido, usado o vencido es un 401 `invalid_ticket`, antes de abrir el socket |
| 5 | **Al abrir el socket, el operador se lee otra vez de la base**, con la misma función que usa el bearer. Si lo desactivaron entre el ticket y el socket, recibe el 401 `session_no_longer_valid` de siempre |
| 6 | **No se chequea el origen.** Una página de otro sitio no puede conseguir un ticket sin el bearer del operador, y eso es lo que el chequeo de origen protegería |
| 7 | **Los tickets viven en memoria**, igual que las conexiones (ADR-0027). Los vencidos se barren cada vez que se emite uno nuevo |

### 2.3 Qué se empuja

| # | Regla |
|---|---|
| 8 | **`conversation.updated`**, con **la misma fila que dibuja el listado** (`GET /conversations`): último mensaje, fallidos, vendedor, modo, estrella y los demás campos de F22. No lleva ningún dato sensible, porque la fila del listado no los lleva (ADR-0035, regla 4) |
| 9 | **`conversation.removed`**, sólo con el `id`, **cuando un operador toma una conversación del pool al responderla**. Se manda a los que dejaron de verla, que la tenían en pantalla un instante antes |
| 10 | **No hay un evento por mensaje.** Si el hilo de esa conversación está abierto, el frontend vuelve a pedir su primera página al recibir el evento; el cursor de ADR-0036 hace que eso sea barato |

### 2.4 A quién

| # | Regla |
|---|---|
| 11 | **Cada evento se filtra por conexión con la regla de alcance del listado** ([ADR-0035](0035-reglas-de-la-bandeja-manual.md), reglas 1 y 3): la cuenta, y además la suya o la del pool para el `operator`, o todas para `supervisor` y `admin`. Un `operator` nunca recibe una conversación que no podría abrir |
| 12 | **Los filtros de F23 no se aplican en el servidor.** El socket manda todo lo que el rol alcanza, y el frontend decide si la fila entra en el filtro que tiene puesto ([ADR-0050](0050-filtros-de-la-bandeja.md)) |

### 2.5 Quién publica, y cuándo

| # | Regla |
|---|---|
| 13 | **Se publica después del commit, nunca adentro de la transacción.** Publica el que conduce la escritura: |
| | · `whatsapp/`, al terminar el webhook, **una vez por conversación** aunque el payload traiga varios mensajes |
| | · `turn/`, **una vez al terminar el turno**, también si falló, porque cada escritura del turno confirmó por su cuenta |
| | · `inbox/`, al responder (después del envío, para que la fila ya diga enviado o fallido), al cambiar el modo y al cambiar la estrella ([ADR-0053](0053-estrella-de-seguimiento.md)) · **y al marcar leída, agregado por [ADR-0052](0052-marca-de-leida.md)** · **y al cerrar, agregado por [ADR-0054](0054-cierre-manual-de-la-conversacion.md)** · **y al calificar, agregado por [ADR-0055](0055-puntaje-de-la-conversacion.md)** |
| | · `agents/`, al pausar o dar de baja un agente IA: una vez por cada conversación cuyo bot apagó. **Esto enmienda ADR-0038 §4** |
| 14 | **Publicar nunca bloquea a quien publica.** Los avisos entran a una cola de 256; si está llena, el aviso se descarta y queda en el log. El webhook no espera a la bandeja, igual que no espera al turno ([ADR-0029](0029-el-acuse-del-webhook-no-espera-al-turno.md)) |
| 15 | **Un solo worker lee la fila y reparte.** Hace dos lecturas por aviso: la cuenta de la conversación y su fila del listado |

### 2.6 Cuando algo falla

| # | Regla |
|---|---|
| 16 | **Cada conexión tiene una cola de 32 mensajes.** Si se llena, la conexión se corta con *policy violation*, en vez de trabar a las demás |
| 17 | **Ping cada 25 segundos**, por debajo del timeout de inactividad habitual de los proxies |
| 18 | **No se reenvía nada al reconectar.** El frontend vuelve a pedir la primera página de la bandeja |
| 19 | **Al apagarse el núcleo, los sockets se cierran con *going away* antes que el servidor HTTP**, porque `Shutdown` no espera a una conexión secuestrada. Ese código es lo que le dice al frontend que reconecte |

## 3. Justificación

- **`coder/websocket` y no `gorilla/websocket`.** Las dos resuelven el protocolo. `coder/websocket` recibe un `context` en cada lectura, escritura y ping, así que los timeouts y el apagado se escriben igual que en el resto del núcleo. Con `gorilla/websocket` habría que manejar deadlines a mano.
- **El ticket y no el JWT en la URL.** Una URL queda escrita en los logs de acceso del núcleo y de cualquier proxy en el camino. Un ticket que vence a los 30 segundos y se gasta al usarse no sirve de nada leído de un log. Mandar el JWT en `Sec-WebSocket-Protocol` también lo sacaba de los logs, pero es un uso forzado de ese header.
- **Leer al operador otra vez (regla 5)** aplica la regla 2 de [ADR-0030](0030-autenticacion-de-operadores.md) **al abrir el socket**: un operador desactivado no puede abrir uno nuevo. Pero sólo al abrirlo: lo que cambia después no se relee, y eso **enmienda** esa regla y la *revocación inmediata* de su §4 (ver §4 de este ADR). Releer al operador por cada evento costaría una lectura más por conexión y por aviso.
- **La misma fila que el listado (regla 8)** hace que el frontend tenga una sola forma de dibujar una conversación, y que el evento no pueda mostrar nada que el listado no muestre.
- **Después del commit (regla 13)**, porque un evento publicado adentro de una transacción que después se deshace anunciaría algo que no pasó, y el frontend lo dibujaría.
- **Una vez al final del turno**, y no uno por cada escritura, porque el operador necesita ver lo que el turno dejó, no sus pasos intermedios.
- **Sin reenvío (regla 18).** Guardar eventos para reenviarlos exigiría una tabla nueva y un número de secuencia. La primera página ya es el estado actual, y pedirla es barato.
- **`conversation.removed` sólo al tomar el pool respondiendo (regla 9).** Es el único cambio en que se sabe con certeza que otros operadores tenían la fila en pantalla. Mandarlo por cualquier cambio de vendedor le avisaría a un operador de conversaciones que nunca vio, y [ADR-0035](0035-reglas-de-la-bandeja-manual.md), regla 3, busca justamente que no se entere de que existen.

## 4. Consecuencias

### Positivas

- La bandeja se actualiza sola: un mensaje del cliente, una respuesta del bot o de un operador, un cambio de modo o de estrella, y la pausa de un agente IA llegan a quien corresponde sin refrescar.
- Un evento nunca muestra más que el listado, ni a más gente que el listado.
- El webhook y el turno no esperan a la bandeja.
- La pausa de un agente IA avisa en el momento, que era el costo que ADR-0038 dejaba para esta fase.

### Negativas / costos asumidos

- **Una dependencia nueva en el núcleo**, que pasa a tener seis directas.
- **Un aviso puede perderse.** Si la cola de 256 se llena —por ejemplo, al pausar un agente IA con cientos de conversaciones—, los avisos que no entran se descartan. La bandeja queda desactualizada hasta el próximo cambio de esa conversación o hasta que el frontend reconecte.
- **Cortar al cliente lento también le pierde eventos**, y lo obliga a reconectar y volver a pedir la primera página.
- **Un operador desactivado mientras está conectado sigue recibiendo eventos** hasta que su socket se corta. El bearer lo frena en el próximo pedido HTTP, pero el socket abierto no se revisa.
- **Lo mismo con un cambio de rol.** El alcance se fija al abrir el socket: un `supervisor` al que le bajan el rol a `operator` sigue recibiendo las conversaciones de todo el equipo, que el Anexo B ya no le deja ver, hasta que reconecta.
- **El ticket igual pasa por la URL**, y queda en los logs de acceso. Sólo sirve 30 segundos y una vez, pero está.
- **Si el reparto de ADR-0037 le da a un vendedor una conversación que estaba en el pool, los demás no reciben `conversation.removed`.** Pasa cuando el cliente vuelve a escribir en una conversación que había quedado en el pool porque no había vendedores. Los demás siguen viendo esa fila hasta que refrescan, y si intentan responderla reciben el 409 `conversation_taken` (ADR-0035, regla 8).
- **Con los filtros *no leídas*, *por etiqueta* o *con recordatorio*, el frontend no puede saber con el evento si la fila sigue entrando en el filtro**, porque la fila no trae esos datos. Tiene que volver a pedir la primera página filtrada. **→ Enmendado por [ADR-0052](0052-marca-de-leida.md):** la fila ya trae `unread_count`, así que el costo queda para *por etiqueta* y *con recordatorio*.
- **Editar o dar de baja un contacto no publica nada.** `contacts/` no conoce el hub, y la fila lleva el nombre, el apellido y el teléfono (ADR-0035, regla 4). Las bandejas abiertas muestran los datos viejos hasta el próximo evento de esa conversación, y la de un contacto dado de baja sigue a la vista hasta que el frontend refresca.
- **El evento no dice si hay una sugerencia esperando** ([ADR-0046](0046-modo-sugerencia.md)). En modo sugerencia, el frontend tiene que pedirla al recibir el evento de esa conversación.
- **Cada aviso cuesta dos lecturas a la base**, aunque nadie esté conectado.
- **Todo se pierde al reiniciar el núcleo**: tickets, conexiones y avisos en cola, porque viven en memoria (ADR-0027).
- **Al apagarse, el núcleo espera la respuesta de cierre de cada navegador**, hasta el tope del apagado ordenado.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **`gorilla/websocket`** | Resuelve lo mismo, pero sin `context`: los timeouts y el apagado se manejan con deadlines a mano, distinto del resto del núcleo |
| **El JWT en el query string** | Es lo más simple, pero el token completo queda en los logs de acceso y de cualquier proxy, y dura lo que dura la sesión |
| **El JWT en `Sec-WebSocket-Protocol`** | No queda en los logs, pero usa un header de negociación de protocolo para otra cosa, y el frontend tiene que conocer el truco |
| **Server-Sent Events** en lugar de WebSockets | Resolverían el empuje en un solo sentido, pero F21 nombra WebSockets |
| **Un evento por cada mensaje, además de la fila** | Duplica lo que la fila ya trae en `last_message`, y abre la pregunta de en qué orden llegan los dos. El hilo abierto se refresca con su primera página |
| **Publicar dentro de la transacción** | Un rollback anunciaría un cambio que no ocurrió |
| **Reenviar los eventos perdidos al reconectar** | Exige guardar los eventos con un número de secuencia. La primera página ya es el estado actual |
| **`LISTEN`/`NOTIFY` de PostgreSQL** para el fan-out | Es la salida que ADR-0027 dejó escrita para cuando haya varias instancias. Con una sola, agrega un camino más sin que nada lo pida |
