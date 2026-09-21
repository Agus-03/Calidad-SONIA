# ADR-0029 — El acuse del webhook no espera al turno

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · §2 enmendada por [ADR-0041](0041-temporizador-del-turno-por-poller.md), regla 7: el turno ya no arranca con cada mensaje sino cuando el bloque cierra, y quien lo arranca es el poller y no el handler del webhook. Lo demás se sostiene entero — 200 inmediato, contexto propio, una goroutine por turno sin cola, y el apagado que espera a los turnos en vuelo |
| **Fecha** | 2026-09-06 |
| **Etapa** | Construcción — Fase 1 (esqueleto de punta a punta) |
| **Resuelve** | Cuándo devuelve 200 el webhook de Meta, y quién ejecuta el turno |
| **Requisitos afectados** | F02, F10, F20, F29 |

---

## 1. Contexto

El webhook de Meta y la respuesta al cliente son **dos llamadas HTTP distintas, en direcciones opuestas**:

1. **Meta → núcleo:** `POST /webhooks/whatsapp`. Trae el mensaje del cliente. El núcleo contesta **200 con el cuerpo vacío** — un acuse de recibo que no lleva ninguna respuesta.
2. **Núcleo → Meta:** `POST /{version}/{phone_number_id}/messages`. Lleva la respuesta, y es un request nuevo que abre el núcleo cuando quiere.

Nada obliga a que la segunda ocurra antes de la primera. La pregunta era si el núcleo, aun así, debía **retener el 200 hasta terminar el turno**, aprovechando que el request ya está abierto y evitando así ejecutar el turno fuera del handler.

Retenerlo significaba meter en la espera de Meta el salto al cerebro —hasta 10 s por intento, con hasta tres intentos— más el envío —hasta 5 s—. En el caso normal 2 a 4 segundos; en el peor, más de treinta.

## 2. Decisión

**El handler del webhook persiste el mensaje entrante y devuelve 200 inmediatamente. El turno se ejecuta después, en una goroutine del mismo proceso, con un contexto propio.**

Tres reglas la acompañan:

- **El contexto del turno no es el del request.** El del request se cancela al responder, así que el turno recibe uno desacoplado, con el presupuesto total del turno como plazo.
- **El apagado ordenado espera a los turnos en vuelo**, con tope, del mismo modo en que `server.Shutdown` ya espera los requests abiertos.
- **Una goroutine dentro del proceso, nunca una cola ni un worker aparte.** ADR-0017 (e) fija que el salto núcleo↔cerebro es sincrónico y sin cola en el medio, y [ADR-0027](0027-nucleo-de-instancia-unica.md) fija el núcleo como instancia única.

## 3. Justificación

- **Retener el 200 modela mal el dominio, y se rompe en dos de los tres modos de `AgentMode`.** En `suggestion` la respuesta la manda un operador y en `off` no hay respuesta automática: en ambos casos no existe nada que esperar, y un acuse que dependiera de generar la respuesta fallaría siempre. La regla tiene que ser la misma para los tres modos.
- **Un webhook es un aviso de entrega, no una pregunta.** Meta no sabe que del otro lado hay un LLM: si el 200 tarda, asume que el servidor está caído, reintenta la entrega y, si el patrón se sostiene, puede dejar de enviar eventos.
- **Retenerlo no protegía de nada.** Se evaluó como forma de sobrevivir a una caída del proceso a mitad del turno, y no lo logra: Meta reintenta el webhook, la idempotencia por `wamid` saltea el reintento y el mensaje se pierde igual. **La pérdida es idéntica con el 200 retenido y con el 200 anticipado**, así que retenerlo sólo agregaba la espera.
- **La alternativa se descarta entera en la Fase 7.** El temporizador de F10 obliga a que el turno arranque cuando el bloque cierra y no cuando llega el mensaje, o sea fuera del handler. Construir hoy la versión sincrónica es escribir código para tirarlo.

## 4. Consecuencias

### Positivas

- El acuse sale en milisegundos, que es el comportamiento que Meta espera de un webhook.
- La misma ruta sirve para los tres modos de `AgentMode`, sin una rama por modo.
- La goroutine del turno es la forma que la Fase 7 necesita, así que el timer de F10 se suma sin rehacer el flujo.

### Negativas / costos asumidos

- **Aparece una clase de concurrencia nueva.** Hasta acá la única goroutine del núcleo era la del servidor. Ahora hay una por turno, con su contexto y su registro para el apagado, y los errores de concurrencia son los que peor se reproducen.
- **Un turno perdido en una caída ya no deja a nadie esperando.** Con el 200 ya dado, Meta considera el evento entregado y no vuelve a intentarlo: si el proceso muere, ese turno no se retoma ni avisa. **La recuperación queda sin resolver**, y es el pendiente que [ADR-0027](0027-nucleo-de-instancia-unica.md) ya tenía abierto para la Fase 7: `message_batches.processed_at IS NULL` con `closed_at IS NOT NULL` es la marca de un turno cerrado y no procesado ([`07-secuencia-del-turno.md`](../07-secuencia-del-turno.md) §5), y todavía nadie la lee al arrancar. No empeora respecto de la alternativa, pero tampoco mejora.
- **Un turno en vuelo alarga el apagado.** Cada despliegue espera hasta el tope antes de cortar.
- **El log deja de leerse por request.** El turno ocurre después de que el `X-Request-Id` del webhook cerró, así que la correlación entre el mensaje entrante y su respuesta hay que sostenerla con el `conversation_id` en cada línea.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Retener el 200 hasta terminar el turno** | Hace esperar a Meta 2 a 4 segundos en el caso normal y más de treinta en el peor, sin comprar nada a cambio: la pérdida ante una caída es la misma. Y no funciona en los modos `suggestion` ni `off`, donde no hay respuesta automática que esperar. |
| **Una cola y un worker aparte** | Prohibido por ADR-0017 (e): el salto núcleo↔cerebro es sincrónico y sin cola. Agrega un componente de infraestructura que ningún requisito de F01–F39 pide. |
| **Esperar a la Fase 7 para separar el acuse del turno** | La separación no depende del temporizador: es correcta desde el primer mensaje. Postergarla obliga a escribir una versión sincrónica que la Fase 7 tira entera. |
