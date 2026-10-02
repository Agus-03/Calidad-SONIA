# ADR-0058 — Los recordatorios: a quién avisan, cómo y quién los cambia

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · que avise al vendedor asignado (regla 6), por el socket y con una lista propia (reglas 7 y 8), y quién edita y borra (regla 4) los eligió el grupo el 2026-10-02. Que no haga falta un estado *hecho* (regla 5) lo validó el grupo el mismo día |
| **Fecha** | 2026-10-02 |
| **Etapa** | Construcción — Fase 11 (resto del CRUD operativo) |
| **Resuelve** | Cómo se construye F28: quién crea, edita y borra un recordatorio, a quién avisa cuando vence, por qué medio y cuándo deja de marcar la bandeja |
| **Requisitos afectados** | F28 · F23 · F21 · F32 · Anexo B |
| **Enmienda** | [ADR-0050](0050-filtros-de-la-bandeja.md) §1 y §4 — *con recordatorio* deja de devolver vacío, y con él los siete filtros de F23 tienen datos · [ADR-0051](0051-bandeja-en-tiempo-real.md) §2.3 — un segundo tipo de evento, `reminder.due`, dirigido a una persona y no a una fila |
| **Relacionada con** | [ADR-0002](0002-ciclo-de-vida-conversacion-y-recordatorios.md), [ADR-0027](0027-nucleo-de-instancia-unica.md), [ADR-0032](0032-acceso-a-contactos-y-datos-sensibles.md), [ADR-0037](0037-toda-conversacion-nace-con-vendedor.md), [ADR-0041](0041-temporizador-del-turno-por-poller.md), [ADR-0056](0056-transferencia-entre-operadores.md) |

---

## 1. Contexto

F28 dice *"Alertas vinculadas a contactos con fecha. Filtrables en la bandeja y notifican al operador asignado"*. [ADR-0002](0002-ciclo-de-vida-conversacion-y-recordatorios.md) (c) colgó los recordatorios del contacto, con la conversación como dato opcional, y el esquema trae `reminders` con `due_at`, `notified` y `notified_at`. El filtro *con recordatorio* de [ADR-0050](0050-filtros-de-la-bandeja.md) ya existía y devolvía vacío.

Faltaba todo lo demás: el paquete `reminders/` que [`struct.md`](../../struct.md) preveía, quién puede tocar un recordatorio, y la mitad que más pesa: **avisar cuando vence**, que exige algo que corra solo en el tiempo. F28 no dice quién es *"el operador asignado"*: puede ser quien creó el recordatorio o el vendedor del contacto.

## 2. Decisión

### 2.1 El recordatorio

| # | Regla |
|---|---|
| 1 | **Un paquete nuevo, `reminders/`, dueño de la tabla.** Rutas: `POST` y `GET /api/v1/contacts/:id/reminders`, `GET /api/v1/reminders`, `PATCH` y `DELETE /api/v1/reminders/:id`. Abiertas a los tres roles |
| 2 | **Crea quien alcanza al contacto**, con el alcance de [ADR-0032](0032-acceso-a-contactos-y-datos-sensibles.md), que decide `contacts/`. Quien lo crea queda como su dueño, en `operator_id`. Un contacto fuera de alcance es un 404 `contact_not_found` |
| 3 | **La conversación es opcional y tiene que ser de ese contacto** (ADR-0002 (c)): si no, 400 `conversation_not_of_contact`. Una descripción vacía es un 400 `invalid_description`, y una fecha que no está en el futuro, un 400 `due_at_in_the_past` |
| 4 | **Edita y borra su dueño, y además `supervisor` y `admin`** sobre cualquiera de la cuenta. Elegido por el grupo el 2026-10-02. Otro `operator` recibe un 403 `not_the_owner` |
| 5 | **Borrar es la forma de darlo por hecho.** El borrado es lógico (C09): sale del contacto y del filtro, y la fila queda. **No hay un estado *hecho* aparte**: F28 pide alertas con fecha que se puedan filtrar, no un estado de completado. Validado por el grupo el 2026-10-02 |

### 2.2 El aviso

| # | Regla |
|---|---|
| 6 | **Avisa al vendedor asignado al contacto**: el vendedor activo de la conversación más reciente del contacto que tenga uno, leído **cuando vence**. Si el contacto no tiene ninguno, avisa a su dueño. Elegido por el grupo el 2026-10-02 |
| 7 | **Por el socket de [ADR-0051](0051-bandeja-en-tiempo-real.md)**: un evento `reminder.due` con el recordatorio, sólo a esa persona y en todas sus conexiones abiertas. Elegido por el grupo el 2026-10-02 |
| 8 | **Y con una lista propia**: `GET /api/v1/reminders` devuelve los que le llegan a quien pregunta, con la misma regla 6, y `?due=true` sólo los vencidos. Es lo que lee quien no estaba conectado cuando venció. Elegido por el grupo el 2026-10-02 |
| 9 | **Un poller marca los vencidos y después avisa.** Corre cada 30 segundos y toma hasta 100 por vuelta, con una sola sentencia y `SKIP LOCKED`, como el del turno ([ADR-0041](0041-temporizador-del-turno-por-poller.md), regla 4). Un recordatorio se avisa una sola vez. Corre en la única instancia del núcleo ([ADR-0027](0027-nucleo-de-instancia-unica.md)) y se detiene junto con el del turno al apagarse |
| 10 | **Mover la fecha lo vuelve pendiente**: si `due_at` cambia, `notified` vuelve a falso, en la misma sentencia, y avisa otra vez cuando venza. Editar sólo el texto no lo cambia |

### 2.3 El filtro

| # | Regla |
|---|---|
| 11 | **El filtro *con recordatorio* no cambia** ([ADR-0050](0050-filtros-de-la-bandeja.md), regla 6): marca la conversación cuyo contacto tiene algún recordatorio no borrado, vencido o no |

## 3. Justificación

- **El vendedor asignado y no el dueño (regla 6)**, porque *"el operador asignado"* es, en el resto del sistema, el vendedor de la conversación ([ADR-0037](0037-toda-conversacion-nace-con-vendedor.md)). Leerlo cuando vence, y no al crearlo, hace que el aviso siga a las reasignaciones y al traspaso de [ADR-0056](0056-transferencia-entre-operadores.md): si el vendedor se fue, el aviso le llega a quien heredó sus conversaciones. El dueño queda como respaldo para un contacto que nunca tuvo vendedor.
- **Las dos vías (reglas 7 y 8)** porque cubren casos distintos. El evento llega en el momento a quien está conectado; la lista es lo que ve quien no lo estaba, porque el socket no reenvía nada al reconectar ([ADR-0051](0051-bandeja-en-tiempo-real.md), regla 18).
- **Marcar y después avisar (regla 9)**, porque marcar y responder en una sola sentencia es lo que garantiza que un recordatorio se avise una vez. Si el aviso se pierde, el recordatorio sigue en la lista.
- **Un poller, como el del turno (regla 9)**, porque la fecha vive en la fila y un reinicio no pierde nada. Treinta segundos alcanzan: F28 fecha un recordatorio, no lo cronometra.
- **Sin estado *hecho* (regla 5)**, porque F28 no lo pide y el esquema ya ofrece una forma de cerrar un recordatorio: borrarlo, con la fila preservada. Agregarlo habría sido una columna nueva en `0001` sin un requisito detrás.
- **El dueño y los roles de arriba (regla 4)**, porque el recordatorio es trabajo de una persona, y el `supervisor` es quien reorganiza el trabajo del equipo.

## 4. Consecuencias

### Positivas

- F28 existe completo: alertas con fecha, filtrables y avisadas.
- Con *con recordatorio* alimentado, los siete filtros de F23 tienen datos.
- El aviso sigue al vendedor, también después de una reasignación o un traspaso.
- Un aviso perdido no se pierde del todo: queda en la lista.

### Negativas / costos asumidos

- **Un aviso puede llegar hasta 30 segundos tarde**, y más si se juntan más de 100 vencidos en una vuelta.
- **Quien lo crea puede no recibirlo.** Si el contacto tiene vendedor, el aviso es del vendedor, aunque el recordatorio lo haya escrito un `supervisor` o un compañero. Para el autor, el recordatorio sólo aparece en la ficha del contacto.
- **Un aviso que llega con el socket cerrado no se repite.** El recordatorio ya quedó marcado, así que sólo aparece en la lista. El frontend tiene que pedirla al conectarse.
- **El filtro *con recordatorio* sigue marcando los vencidos y ya avisados** hasta que alguien los borra (regla 11). Si nadie borra, la conversación queda marcada para siempre.
- **El destinatario se calcula con una subconsulta por recordatorio**, en el poller y en la lista. No hay un índice para el poller: la tabla es chica y la regla general de [`migrations/README.md`](../../../src/core/migrations/README.md) es indexar cuando una consulta sea lenta.
- **Un vendedor desactivado no recibe nada**, y el aviso cae en el vendedor activo de una conversación anterior o en el dueño. Si ninguno está activo, el aviso le llega igual al dueño desactivado, que no puede entrar.
- **El borrado no tiene vuelta atrás desde la API**: un recordatorio borrado por error se vuelve a crear.
- **El poller corre aunque no haya nadie conectado**: una consulta cada 30 segundos.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Avisar a quien lo creó** | Después de una reasignación, el aviso le llegaría a quien ya no atiende al cliente |
| **Fijar el destinatario al crearlo** | El mismo problema, congelado en la fila |
| **Sólo el evento, sin lista** | Quien no estaba conectado nunca se enteraría: el socket no reenvía |
| **Sólo la lista, sin evento** | F28 dice que *notifican*: una lista que hay que ir a mirar no notifica |
| **Un estado *hecho*** | F28 no lo pide; borrar ya cierra el recordatorio y preserva la fila |
| **Un temporizador en memoria por recordatorio** | Se pierde con cada reinicio, el mismo problema que [ADR-0041](0041-temporizador-del-turno-por-poller.md) resolvió para el turno |
| **Que sólo `supervisor` y `admin` creen recordatorios** | El recordatorio es el seguimiento del propio vendedor, y el grupo eligió que lo cree quien alcanza al contacto |
