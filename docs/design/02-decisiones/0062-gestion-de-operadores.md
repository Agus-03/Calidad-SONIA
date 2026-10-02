# ADR-0062 — La gestión de operadores: quién la hace, cómo se recupera una contraseña y qué pasa con lo que un vendedor deja

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · las contraseñas (reglas 8 a 10), qué pasa con las conversaciones de quien se desactiva o se borra (regla 7) y auditar los cambios (regla 11) los eligió el grupo el 2026-10-02 |
| **Fecha** | 2026-10-02 |
| **Etapa** | Construcción — Fase 11 (resto del CRUD operativo) |
| **Resuelve** | Cómo se construye F18: quién da de alta, edita, desactiva y borra operadores; cómo se cambia y se recupera una contraseña; qué protege a la cuenta de quedarse sin `admin`; y qué pasa con las conversaciones de quien deja de atender |
| **Requisitos afectados** | F18 · F26 · Anexo B · H-00 |
| **Enmienda** | [ADR-0007](0007-alcance-del-registro-de-auditoria.md) §2 — los cambios de operadores se auditan en `audit_logs` · [ADR-0037](0037-toda-conversacion-nace-con-vendedor.md) §4 — desactivar a un vendedor deja de ser *"SQL a mano"* |
| **Relacionada con** | [ADR-0030](0030-autenticacion-de-operadores.md), [ADR-0035](0035-reglas-de-la-bandeja-manual.md), [ADR-0038](0038-alta-baja-y-pausa-del-agente-ia.md), [ADR-0056](0056-transferencia-entre-operadores.md) |

---

## 1. Contexto

F18 dice *"Roles: User (operador básico), Super Agent (puede reasignar), Admin (gestión completa)"*, y el Anexo B le da al `admin` la gestión de usuarios. La tabla `operators` existe desde `0001`, con rol, `active` y borrado lógico. [ADR-0030](0030-autenticacion-de-operadores.md) construyó el login, y el único camino para crear a alguien era la semilla del primer `admin`.

Desactivar a un vendedor era *"SQL a mano"*, como dejó escrito [ADR-0037](0037-toda-conversacion-nace-con-vendedor.md) §4. El anteproyecto no dice cómo se recupera una contraseña olvidada, ni qué pasa con las conversaciones de quien deja de atender. Tampoco dice si se auditan los cambios de una persona: [ADR-0007](0007-alcance-del-registro-de-auditoria.md) acotó la auditoría a la configuración de los agentes IA.

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **El ABM vive en `operators/`, en un servicio aparte del login** (`Roster`). Rutas: `GET` y `POST /api/v1/operators`, `PATCH` y `DELETE /api/v1/operators/:id`, `PUT /api/v1/operators/:id/password` y `PUT /api/v1/me/password` |
| 2 | **Da de alta, edita, desactiva, borra y resetea contraseñas sólo el `admin`** (Anexo B) |
| 3 | **La lista la leen `supervisor` y `admin`**, porque el `supervisor` reasigna (F26) y necesita saber a quién. El `operator` no la ve. Se filtra por rol y por `active`, y se pagina por número de página |
| 4 | **Se edita nombre, apellido, correo, rol y `active`**, con un `PATCH` parcial. El correo se guarda en minúsculas y es único entre los no borrados: si está tomado, 409 `email_taken` |
| 5 | **El `admin` no puede cambiar su propio rol ni su `active`, borrarse ni resetear su propia contraseña por la ruta del `admin`**: 409 `not_on_yourself`. Sí puede editar su nombre y su correo |
| 6 | **La cuenta nunca queda sin un `admin` activo.** Quitarle el rol o `active` al último, o borrarlo, es un 409 `last_admin`. Los `admin` activos se bloquean en orden de id antes que la fila que se cambia, así que dos `admin` que se degradan entre sí al mismo tiempo se esperan y no se quedan los dos sin el rol |
| 7 | **Se puede desactivar o borrar a quien tiene conversaciones abiertas, y la respuesta dice cuántas le quedan** (`held_conversations`), para que el `admin` las mueva con el traspaso en bloque de [ADR-0056](0056-transferencia-entre-operadores.md). Elegido por el grupo el 2026-10-02. Las conversaciones no se mueven solas |
| 8 | **El `admin` fija la contraseña inicial al dar de alta**, de al menos 8 caracteres y como mucho 72 bytes, el límite de bcrypt ([ADR-0030](0030-autenticacion-de-operadores.md)). Elegido por el grupo el 2026-10-02 |
| 9 | **Quien olvida su contraseña la recupera a través del `admin`**, que le fija una nueva con `PUT /operators/:id/password` y se la comunica. No hay recuperación por correo: el sistema no tiene un servicio de correo. Elegido por el grupo el 2026-10-02 |
| 10 | **Cada uno cambia la suya con `PUT /me/password`, probando la actual.** Si no coincide, 400 `wrong_current_password`. Elegido por el grupo el 2026-10-02 |
| 11 | **Cada cambio deja una fila en `audit_logs`, en la transacción del cambio**, escrita a través de `agents/`, que es el dueño de la tabla. `entity_type` es `operators`; `action` es `create`, `update` o `delete`; `changes_json` guarda `{before, after}` de los datos públicos, y para una contraseña sólo `{"password": "reset"}` o `{"password": "changed"}`. El hash nunca entra en la auditoría. Elegido por el grupo el 2026-10-02 |
| 12 | **Borrar es lógico (C09), pone `active` en falso y libera el correo**, como prevé el índice de `0001`. Un cambio de rol, una desactivación o un borrado rigen desde la petición siguiente, porque el rol y `active` se leen en cada una ([ADR-0030](0030-autenticacion-de-operadores.md), regla 2), **salvo en un socket de la bandeja ya abierto** (§4) |
| 13 | **Una contraseña cambiada entre la lectura y la escritura no se pisa**: el hash anterior va en el predicado del `UPDATE`, y si cambió es un 409 `password_changed` |

## 3. Justificación

- **Sólo el `admin` (regla 2)** porque el Anexo B le da la gestión de usuarios y nombra al `supervisor` sólo para reasignar.
- **Recuperar a través del `admin` (regla 9)** porque es lo único que funciona sin servicio de correo. Además, quien la olvidó es alguien del equipo, que el `admin` conoce y puede verificar en persona.
- **Pedir la actual para cambiar la propia (regla 10)** para que una sesión robada no alcance para quedarse con la cuenta. Por la misma razón, el `admin` no resetea la suya por su ruta (regla 5).
- **Las guardas del `admin` (reglas 5 y 6)** porque una cuenta sin `admin` activo sólo se recupera desde fuera de la aplicación, corriendo la semilla con otro correo o con SQL a mano.
- **Avisar y no mover (regla 7)** porque a quién van esas conversaciones lo decide el `admin`, y ya existe la herramienta para hacerlo ([ADR-0056](0056-transferencia-entre-operadores.md)). Devolverlas al pool perdería el vendedor que el cliente conoce.
- **Auditar (regla 11)** porque un cambio de rol decide qué puede hacer cada uno, y `operators` no tiene ni `deleted_by` ni `updated_by`: sin `audit_logs`, nadie podría decir quién le dio el rol `admin` a alguien. Se escribe a través de `agents/`, como ya hace `whatsapp/`, porque la tabla es de ese paquete (regla 1 de `08-structure-rules`).

## 4. Consecuencias

### Positivas

- F18 existe: el equipo se administra desde la pantalla y no con SQL.
- Una contraseña olvidada tiene camino de vuelta sin tocar la base.
- Cada cambio sobre una persona deja quién, qué y cuándo.

### Negativas / costos asumidos

- **El `admin` conoce la contraseña que fija**, al dar de alta y al resetear. No hay un cambio obligatorio en el primer ingreso: la persona tiene que cambiarla por su cuenta con `/me/password`. Forzarlo exige una columna nueva y bloquear las demás rutas hasta que la cambie.
- **Un reset no cierra la sesión abierta.** El token sólo lleva `sub` y no hay *logout* del lado del servidor ([ADR-0030](0030-autenticacion-de-operadores.md), regla 7): quien tenía una sesión con la contraseña vieja la conserva hasta que vence, a lo sumo 12 horas. Desactivar sí corta en la petición HTTP siguiente, aunque no en un socket abierto (abajo).
- **Desactivar, bajar el rol o borrar no corta un socket ya abierto.** El socket de la bandeja lee el rol y `active` una sola vez, al abrirse ([ADR-0051](0051-bandeja-en-tiempo-real.md), que enmendó la regla 2 de [ADR-0030](0030-autenticacion-de-operadores.md)): quien fue desactivado o degradado sigue recibiendo los eventos de su alcance anterior hasta que reconecta. Con F18 construido, desactivar es una operación de todos los días, así que este costo deja de ser teórico.
- **Si el único `admin` olvida su contraseña, no hay salida desde la aplicación.** Hay que correr la semilla con otro correo, que crea un `admin` nuevo, y desde ahí resetear la del anterior; la semilla nunca pisa a uno que ya existe. Conviene tener siempre dos.
- **Las conversaciones de quien se desactiva quedan a su nombre** hasta que el `admin` haga el traspaso (regla 7). Si se olvida, quedan trabadas, igual que antes.
- **El `admin` no puede degradarse a sí mismo**, aunque haya otro `admin`. Tiene que hacerlo otro.
- **La auditoría crece con cada alta, edición y contraseña**, y [ADR-0007](0007-alcance-del-registro-de-auditoria.md) la había acotado a propósito. Se acepta porque son cambios poco frecuentes.
- **Bloquear a todos los `admin` en cada cambio de rol o de `active`** serializa esas operaciones en la cuenta. Con un equipo chico no se nota.
- **Los tests del paquete tardan más**: bcrypt con costo 12 bajo `-race` lleva el paquete a varios minutos.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Recuperar la contraseña por correo** | No hay servicio de correo, y el anteproyecto no lo prevé |
| **Que sólo el `admin` maneje las contraseñas** | Cada persona no podría cambiar la que le fijaron, y el `admin` la conocería siempre |
| **Rechazar la desactivación mientras haya conversaciones** | Obliga a traspasar antes de poder cortar el acceso de alguien que se va, que es lo urgente |
| **Devolver sus conversaciones al pool** | El cliente pierde el vendedor que conoce, y el `admin` ya puede elegir a quién van |
| **Que el `supervisor` también gestione** | El Anexo B se lo da sólo al `admin` |
| **No auditar** | Nadie podría decir quién cambió un rol o borró a alguien |
| **Escribir `audit_logs` desde `operators/`** | La tabla es de `agents/` (regla 1 de `08-structure-rules`) |
