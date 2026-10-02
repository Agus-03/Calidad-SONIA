# ADR-0056 — La transferencia entre operadores: quién reasigna, cómo se resuelven las ausencias y quién escribe las notas internas

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · que el `operator` no reasigne (regla 2), el traspaso en bloque para las ausencias (regla 9), el alcance y la autoría de las notas (reglas 14 y 15) y no reasignar las resueltas (regla 4) los eligió el grupo el 2026-10-01 |
| **Fecha** | 2026-10-01 |
| **Etapa** | Construcción — Fase 11 (resto del CRUD operativo) |
| **Resuelve** | Cómo se construye F26, y tres pendientes del README: si el `operator` reasigna lo suyo, qué pasa con las conversaciones de un vendedor ausente y quién escribe las notas internas |
| **Requisitos afectados** | F26 · F21 · F22 · Anexo B |
| **Enmienda** | [ADR-0035](0035-reglas-de-la-bandeja-manual.md), reglas 15 y 16 y §4 — ya se reasigna, y el `operator` sigue sin poder · [ADR-0037](0037-toda-conversacion-nace-con-vendedor.md) §4, *Lo que queda abierto* — las ausencias se resuelven con el traspaso en bloque · [ADR-0051](0051-bandeja-en-tiempo-real.md), reglas 9 y 13 — el vendedor anterior recibe `conversation.removed`, e `inbox/` publica al reasignar |
| **Relacionada con** | [ADR-0004](0004-asignacion-de-conversaciones.md), [ADR-0025](0025-atribucion-por-operador.md), [ADR-0054](0054-cierre-manual-de-la-conversacion.md), [ADR-0055](0055-puntaje-de-la-conversacion.md) |

---

## 1. Contexto

F26 pide *"reasignación con historial completo, estado del agente IA y notas internas preservados"*. El Anexo B le da la reasignación al Super Agent (*"puede reasignar conversaciones entre operadores"*) y se la niega al User (*"No puede reasignar"*). En SONIA son `supervisor` y `operator`, y el `admin` tiene todo lo del `supervisor`.

El esquema trae desde `0001` las dos tablas que F26 necesita:
- `conversation_assignments`, que ya escriben la toma del pool ([ADR-0035](0035-reglas-de-la-bandeja-manual.md), regla 7) y el reparto ([ADR-0037](0037-toda-conversacion-nace-con-vendedor.md));
- `internal_notes`, que no escribía nadie.

Tres decisiones esperaban esta fase:
- **Si el `operator` reasigna lo suyo.** [ADR-0035](0035-reglas-de-la-bandeja-manual.md), regla 16, la difirió a la Fase 11.
- **Las ausencias del vendedor.** Con el reparto de [ADR-0037](0037-toda-conversacion-nace-con-vendedor.md), un vendedor ausente sigue con sus conversaciones y nadie puede sacárselas. La única salida era SQL a mano (ADR-0035 §4).
- **Las notas internas.** Ningún documento decía quién las lee ni quién las escribe.

## 2. Decisión

### 2.1 Reasignar una conversación

| # | Regla |
|---|---|
| 1 | **`POST /api/v1/conversations/:id/reassign`**, con `{"to_operator_id": …, "reason": …}`, responde 200 con el vendedor nuevo |
| 2 | **Reasignan `supervisor` y `admin`. El `operator` no**, como dice el Anexo B. Elegido por el grupo el 2026-10-01. La ruta le responde 403 |
| 3 | **El destino es una persona activa y no dada de baja de la misma cuenta**, de cualquier rol. Una de otra cuenta es un 404 `operator_not_found`; una desactivada o dada de baja, un 409 `operator_unavailable`; la que ya la tiene, un 409 `already_assigned` |
| 4 | **Sólo se reasignan conversaciones abiertas**, `active` o `waiting`. Una resuelta es un 409 `conversation_resolved`. Elegido por el grupo el 2026-10-01 |
| 5 | **El motivo es obligatorio.** Sin motivo, 400 `invalid_reason` |
| 6 | **Sólo cambia `operator_id`.** El modo del bot, la estrella, los no leídos, el puntaje y las notas quedan como estaban: es el *"preservados"* de F26 |
| 7 | **Cada reasignación escribe una fila en `conversation_assignments`**, en la misma transacción: el vendedor anterior (nulo si venía del pool), el nuevo, quién reasignó y el motivo. La fila nunca se edita (C09) |
| 8 | **El vendedor leído va en el predicado del `UPDATE`.** Si otra reasignación, o la toma del pool, ganó en el medio, no se escribe nada y se responde 409 `conversation_taken` |

### 2.2 Las ausencias: el traspaso en bloque

| # | Regla |
|---|---|
| 9 | **`POST /api/v1/inbox/handover`**, con `{"from_operator_id": …, "to_operator_id": …, "reason": …}`, pasa **todas las conversaciones abiertas** de un vendedor a otro en una sola transacción, con una fila de historial por conversación. Responde 200 con cuántas movió, cero incluido. Elegido por el grupo el 2026-10-01 |
| 10 | **Mismo permiso, mismo destino y mismo motivo que la reasignación** (reglas 2, 3 y 5). **El origen sólo tiene que ser de la cuenta**: puede estar desactivado o dado de baja, porque es justamente cuando sus conversaciones quedan trabadas. Origen y destino iguales es un 400 `same_operator` |
| 11 | **No hay un estado *ausente*.** Para que el reparto deje de darle conversaciones a alguien, se lo sigue desactivando ([ADR-0037](0037-toda-conversacion-nace-con-vendedor.md) §4), y el traspaso mueve lo que ya tenía |

### 2.3 El historial y los eventos

| # | Regla |
|---|---|
| 12 | **`GET /api/v1/conversations/:id/assignments`** devuelve el historial, del más viejo al más nuevo, con los nombres de las personas. Tiene el alcance del hilo ([ADR-0035](0035-reglas-de-la-bandeja-manual.md), regla 2) y no pagina |
| 13 | **Al reasignar, el vendedor anterior recibe `conversation.removed`, y nadie más.** Si venía del pool, los que la veían en el pool lo reciben, como en la regla 9 de [ADR-0051](0051-bandeja-en-tiempo-real.md). El nuevo vendedor y los roles de arriba reciben `conversation.updated` |

### 2.4 Las notas internas

| # | Regla |
|---|---|
| 14 | **Las lee y las escribe quien puede leer la conversación** (el alcance del hilo), también sobre una resuelta. Elegido por el grupo el 2026-10-01. Rutas: `GET` y `POST /api/v1/conversations/:id/notes` |
| 15 | **Sólo su autor las edita o las borra**, ni siquiera un `admin` las de otro: 403 `not_the_author`. Elegido por el grupo el 2026-10-01. Rutas: `PATCH` y `DELETE /api/v1/conversations/:id/notes/:note_id` |
| 16 | **El borrado es lógico** (`deleted_at`, C09): la nota sale del hilo y la fila queda |
| 17 | **Una nota vacía es un 400 `invalid_content`**, y una que no existe, que está borrada o que es de otra conversación, un 404 `note_not_found` |
| 18 | **Las notas no se publican por el hub** y nunca llegan al cliente: no cambian la fila de la bandeja |

## 3. Justificación

- **El `operator` no reasigna (regla 2)** porque el Anexo B lo dice textual. Desviarse obligaría a declararlo en el informe, y ninguna necesidad lo pide: el `supervisor` existe justamente para mover trabajo entre vendedores.
- **El traspaso en bloque, y no un estado *ausente* (reglas 9 a 11)**, porque resuelve el problema real —conversaciones trabadas— sin agregar un concepto que el anteproyecto no nombra ni una migración. Desactivar ya corta el reparto.
- **El origen puede estar desactivado (regla 10)** porque, si no, el caso que motiva el traspaso —un vendedor que se fue— sería el único que no se puede resolver.
- **No se reasignan las resueltas (regla 4)** porque una resuelta no vuelve a recibir mensajes ([ADR-0002](0002-ciclo-de-vida-conversacion-y-recordatorios.md) (b)). Lo que importa de ella es quién la atendió: la venta ([ADR-0025](0025-atribucion-por-operador.md)) y el puntaje ([ADR-0055](0055-puntaje-de-la-conversacion.md)) quedan a su nombre.
- **El motivo es obligatorio (regla 5)** porque F26 pide *"historial completo"*, y una fila sin motivo dice quién y cuándo, pero no por qué.
- **`conversation.removed` sólo al vendedor anterior (regla 13).** [ADR-0051](0051-bandeja-en-tiempo-real.md) no lo mandaba en cualquier cambio de vendedor para no contarle a un `operator` que existen conversaciones que nunca vio. El vendedor anterior sí la vio, y es el único que la tiene en pantalla.
- **El alcance del hilo para las notas (regla 14)** porque una nota sirve para pasarle contexto a quien atienda después, y eso incluye al `supervisor`. **Sólo el autor edita (regla 15)** porque la nota va firmada: si otro pudiera cambiarla, la firma mentiría.

## 4. Consecuencias

### Positivas

- F26 existe: se reasigna con historial completo, y el bot, la estrella, los no leídos, el puntaje y las notas se preservan.
- Una conversación de un vendedor ausente ya no queda trabada, y deja de hacer falta SQL a mano.
- Las notas internas tienen dueño y reglas.

### Negativas / costos asumidos

- **El vendedor no puede pasarle una conversación a un compañero.** Tiene que pedírselo a un `supervisor`, y en un equipo sin `supervisor` conectado la conversación espera.
- **El traspaso lo mueve todo a una sola persona.** Repartir lo de un vendedor ausente entre varios exige reasignar de a una.
- **Sin estado *ausente*, una ausencia corta exige desactivar al vendedor.** Mientras está desactivado no puede entrar al sistema, y al volver hay que reactivarlo a mano.
- **Una conversación resuelta queda a nombre de un vendedor que se fue.** No se puede reasignar, así que tampoco se puede calificar (ADR-0055, regla 3).
- **Reasignar no avisa al vendedor nuevo más que con el evento de la fila.** No hay notificación aparte: si no tiene la bandeja abierta, se entera al abrirla.
- **El reparto de [ADR-0037](0037-toda-conversacion-nace-con-vendedor.md) lee el historial**, y cada reasignación le suma una fila al vendedor nuevo. Eso puede correr el orden del turno rotativo.
- **Las notas no llegan en tiempo real** (regla 18). Otro operador con la conversación abierta no ve una nota nueva hasta que vuelve a pedirlas.
- **Ni el historial ni las notas paginan.** Con el volumen esperado alcanza; una conversación con cientos de notas devolvería todas.
- **Nadie puede borrar una nota inapropiada de otro**, ni siquiera el `admin`. La única salida es SQL a mano.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Que el `operator` reasigne las suyas** | Contradice el Anexo B, y el grupo eligió respetarlo |
| **Un estado *ausente* en `operators`**, que el reparto saltee | Es un concepto y una migración que el anteproyecto no pide. El traspaso resuelve las conversaciones trabadas, que era el problema |
| **Reasignar también las resueltas** | Una resuelta no recibe más mensajes, y su venta y su puntaje tienen que quedar con quien la atendió |
| **Motivo opcional** | Dejaría un historial que dice quién y cuándo, pero no por qué |
| **Mandar `conversation.removed` a todos los que dejan de verla**, como al tomar el pool | Le contaría a cada `operator` que existe una conversación que nunca vio ([ADR-0035](0035-reglas-de-la-bandeja-manual.md), regla 3) |
| **Que un `admin` edite o borre cualquier nota** | La nota va firmada, y el grupo eligió que sólo su autor la cambie |
| **Publicar las notas por el hub** | Agregaría un evento que no es una fila de la bandeja, y ADR-0051 tiene uno solo por diseño |
