# ADR-0038 — Alta, baja y pausa del agente IA

| Campo | Valor |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 2026-09-13 |
| **Etapa** | Construcción — Fase 6 (identidad del agente IA) |
| **Resuelve** | Qué significa dar de baja un agente IA, qué pasa con lo que lo nombraba, cómo se evita guardar un agente IA que no puede responder y qué de la fila se ofrece antes de la Fase 9 |
| **Requisitos afectados** | F17 · F01 · F02 y F31 (qué no se ofrece todavía) |
| **Relacionada con** | [ADR-0007](0007-alcance-del-registro-de-auditoria.md), [ADR-0017](0017-decomposicion-en-despliegues-y-lenguajes.md), [ADR-0035](0035-reglas-de-la-bandeja-manual.md), [ADR-0037](0037-toda-conversacion-nace-con-vendedor.md), [C09](../00-convenciones.md) |

---

## 1. Contexto

F17 pide *"alta, baja y configuración de agentes desde el panel admin"*. Hasta la Fase 6, el único agente IA que existía era el de la semilla (`src/core/seeds/0001_demo.sql`): el camino del turno ya lo leía de la base desde la Fase 1 —la bandeja lo nombra en `default_ai_agent_id` y el cerebro lee la fila y sus componentes—, pero nadie podía crear otro.

Construir el ABM destapó cinco preguntas que ni F17 ni el catálogo responden:

1. **`ai_agents` tiene dos columnas de estado**, `active` (Anexo C) y `deleted_at` (C09), y F17 dice "baja" sin decir cuál.
2. **Una baja deja cosas que nombran al agente IA**: las bandejas que lo tienen de default, y las conversaciones abiertas que le pertenecen. Y hoy un agente IA con `active = false` **sigue respondiendo**, porque nadie lee esa columna.
3. **Un agente IA nuevo no atiende a nadie** hasta que una bandeja lo nombre, y el ABM de bandejas (D5) quedó diferido en la Fase 4.
4. **Un modelo sin tarifa hace fallar cada turno.** El cerebro rechaza antes de llamar al proveedor todo par proveedor/modelo que no esté en `rules/pricing.yaml`, y ese rechazo sólo queda en el log. El núcleo no conoce esa lista.
5. **Dos columnas de la fila no tienen efecto todavía**: `default_mode` —`EnsureOpen` crea toda conversación en `autonomous` sin leerla— y `extraction_enabled`, el *toggle* de F31, que es de la Fase 9.

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **La baja es `deleted_at`** (C09) y ninguna lectura de `agents/` devuelve un agente IA dado de baja. **`active` es una pausa**: conserva la configuración y se revierte con un `PATCH` |
| 2 | **Un agente IA pausado o dado de baja no responde.** El turno lee su estado de `ai_agents` al empezar —no del webhook que lo disparó— y, si no puede responder, se saltea igual que con `mode = off`: no abre lote ni llama al cerebro. La conversación queda con su vendedor ([ADR-0037](0037-toda-conversacion-nace-con-vendedor.md)) |
| 3 | **La baja vacía `inboxes.default_ai_agent_id`** de las bandejas que lo nombraban, **en la misma transacción** y por el repositorio de `whatsapp/`, dueño de `inboxes` (regla 1). Las conversaciones nuevas de esas bandejas nacen sin agente IA, en `off`. **La pausa no toca las bandejas** |
| 4 | **Las conversaciones abiertas conservan su `ai_agent_id`** después de la baja: es el agente IA que respondió en ellas, y la regla 2 alcanza para que no vuelva a hacerlo |
| 5 | **Un agente IA llega a una bandeja por `PATCH /api/v1/inboxes/:id`**, sólo sobre `default_ai_agent_id` y sólo para `admin`, en `whatsapp/`. Se decide acá y se construye como tarea aparte de la fase |
| 6 | **`llm_provider` y `llm_model` se validan contra la lista con tarifa del cerebro**, `GET /models`, que lee el mismo `pricing.yaml` que el turno. Se consulta al crear y cuando cambia el modelo, **antes de abrir la transacción**. Un par sin tarifa es un 400 `unpriced_model`; **si el cerebro no responde, 503 `models_unavailable` y no se guarda nada** |
| 7 | **`default_mode` y `extraction_enabled` no forman parte de la API hasta la Fase 9.** Enviarlos es un 400 `unknown_field`, y no aparecen en la respuesta. El alta escribe `default_mode = autonomous` —el modo que `EnsureOpen` ya asume— y `extraction_enabled = false` |
| 8 | **Las cinco rutas son sólo de `admin`**, incluida la lectura |
| 9 | **Cada cambio deja una fila en `audit_logs`, en la transacción del cambio**: `action` es `create`, `update` o `delete`; `entity_type` es `ai_agents`; `changes_json` guarda `{before, after}` con la configuración editable, y en la baja también `cleared_inbox_ids`. **Un guardado sin cambios no escribe nada**, ni la fila ni su auditoría. `ip_address` queda en `NULL` |
| 10 | **El alta nace activa** y no acepta `active`: pausar es una edición con su propia fila de auditoría |
| 11 | **El listado no pagina** |

## 3. Justificación

- **Regla 1.** Con una sola columna se pierde una de las dos operaciones. `deleted_at` es la baja porque C09 ya lo fija para las entidades de configuración, y porque `generations.ai_agent_id` y `messages.sender_ai_agent_id` siguen apuntando a la fila: OE1 la necesita. `active` no tenía uso, y la pausa sí: es lo que permite parar al agente IA mientras se corrige un componente sin perder su configuración ni su historia.
- **Regla 2.** Una pausa es una decisión del administrador, no una falla. La otra salida —que el cerebro rechace al agente IA pausado y el lote quede `failed`— registraría como error cada mensaje de una pausa deliberada. Leer el estado al empezar el turno, y no confiar en el `ai_agent_id` que trae el webhook, hace que una pausa o una baja posterior al mensaje gane igual. El lugar ya existía: `Target` es la lectura que decide si el turno responde, y la de `mode = off` vive ahí.
- **Regla 3.** Una bandeja que nombra a un agente IA dado de baja haría nacer cada conversación nueva con un agente IA que nunca responde, en `autonomous`: la bandeja la mostraría como atendida por el bot. Rechazar la baja con un 409 mientras una bandeja lo use obligaba al administrador a desvincularlo primero, y el ABM de bandejas todavía no existe. Se vacía en la misma transacción para que ninguna conversación nazca con el agente IA una vez confirmada la baja.
- **Regla 4.** Reasignarlas a otro agente IA es una decisión que nadie tomó, y apagarlas (`mode = off`) sería escribir en el agregado compartido desde `agents/`. Con la regla 2, conservar el `ai_agent_id` no tiene efecto sobre las respuestas y preserva la evidencia.
- **Regla 5.** Sin esa ruta, el alta de F17 no cambia nada y la prueba de cierre de la fase no podría usar un agente IA creado por la API. Se descartaron un flag de CLI y el SQL a mano porque dejan el enganche real fuera de lo que se prueba.
- **Regla 6.** Un agente IA guardado con un modelo sin tarifa es la falla silenciosa que el turno ya evita por su lado: el administrador no la ve y el cliente no recibe respuesta. Preguntarle al cerebro, y no copiar la lista al núcleo, es lo que hace que la validación y el turno no puedan discrepar. Se pregunta antes de la transacción porque tener una fila bloqueada mientras se espera al otro servicio haría esperar al cerebro a cada guardado de ese agente IA; ADR-0017 (i).3 ya prohíbe que una transacción cruce ese borde. **No se cachea** la lista, porque `pricing.yaml` cambia con un despliegue del cerebro y no del núcleo.
- **Regla 7.** Un campo que se guarda sin efecto le dice al administrador algo que no es cierto: podría poner un agente IA en `off` y verlo responder igual. Rechazarlo por nombre, en vez de ignorarlo, distingue "no existe todavía" de "se guardó".
- **Regla 8.** F17 ubica toda la gestión *"desde el panel admin"* y el Anexo B se la da al Admin. A diferencia de las etiquetas —que el operador necesita leer para etiquetar ([ADR-0032](0032-acceso-a-contactos-y-datos-sensibles.md), regla 5)—, ninguna pantalla de `operator` ni de `supervisor` necesita la configuración de un agente IA.
- **Regla 9.** Es el formato que ADR-0007 dejó sin fijar. Se guarda la configuración y no la fila entera porque las marcas de tiempo cambian en cada escritura y harían distintos a todos los antes y después. Un guardado sin cambios no escribe porque ADR-0007 existe para proteger a OE2, y una fila por cada vez que alguien aprieta "guardar" enterraría los cambios que importan. **`ip_address` va en `NULL`** porque, sin un proxy de confianza configurado, la dirección del cliente es la que el llamador escriba en `X-Forwarded-For`: una IP falsificable es peor evidencia que ninguna.
- **Regla 11.** Una cuenta configura unos pocos agentes IA. Un número de página sobre cinco filas es un control que nadie usa, y el criterio de [ADR-0036](0036-paginacion-por-cursor-en-la-bandeja.md), regla 9, habla de catálogos que pueden crecer.

## 4. Consecuencias

### Positivas

- F17 queda construido y probado de punta a punta, contra el cerebro real, en el smoke.
- Pausar un agente IA tiene efecto real, y dejarlo de pausar no requiere reconfigurar nada.
- Un modelo mal escrito se rechaza al guardar, frente al administrador, en vez de hacer fallar el primer turno de un cliente.
- Cada cambio de configuración deja quién, qué y cuándo en la misma transacción, que es lo que ADR-0007 pedía para OE2.

### Negativas / costos asumidos

- **Pausar deja al cliente sin respuesta automática.** El vendedor asignado tiene que contestar, y hoy nada le avisa: los avisos en tiempo real son de la Fase 10.
- **Una pausa que llega a mitad de un turno no lo detiene.** El estado se lee al empezar: si el administrador pausa entre esa lectura y la llamada al cerebro, ese turno responde igual. La ventana es de segundos.
- **Con el cerebro caído no se puede crear un agente IA ni cambiarle el modelo.** Renombrarlo o pausarlo sí, porque no consulta la lista.
- **La lista es la de tarifas, no la de proveedores utilizables.** Un par con tarifa cuyo proveedor no tiene adaptador, o no tiene su clave configurada en el despliegue, pasa la validación y falla en el turno igual que hoy. Los dos proveedores con tarifa tienen adaptador; la clave es configuración de cada despliegue y no se valida.
- **El vendedor puede "prender" el bot de una conversación cuyo agente IA fue dado de baja**, porque el endpoint de modo ([ADR-0035](0035-reglas-de-la-bandeja-manual.md), regla 12) sólo pregunta si la conversación tiene `ai_agent_id`, y la regla 4 lo conserva. El turno la saltea igual (regla 2), así que el interruptor dice `autonomous` y nadie responde. **Queda como pendiente.**
- **Quién dio de baja un agente IA no está en la fila.** C09 pide `deleted_by_operator_id` y `ai_agents` no lo tiene en el catálogo; el dato vive en `audit_logs`.
- **`audit_logs` no guarda la IP** hasta que el despliegue defina un proxy de confianza.
- **El contrato del cerebro crece una ruta** (`GET /models`) que el núcleo consume desde el ABM y no desde el turno. Sigue siendo unidireccional: el núcleo pregunta y el cerebro responde.
- **F01 queda parcial en esta fase.** La parte de *"cuándo delegar a un humano"* es `transfer_policies`, que el plan ubica en la Fase 9 y que todavía no tiene dueño ([`struct.md`](../../struct.md) §2.4).

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **La baja es `active = false`**, sin usar `deleted_at` | Pierde la pausa, y contradice C09, que fija `deleted_at` como la baja de las entidades de configuración |
| **Rechazar la baja con 409 mientras una bandeja la use** | Obliga a desvincular primero, y no hay con qué: el ABM de bandejas todavía no existe. El equipo eligió vaciar el default en la misma transacción |
| **Dejar fallar el turno** de un agente IA pausado (404 del cerebro, lote `failed`) | Registra como error cada mensaje de una pausa deliberada, y no toca `turn/` a cambio de un rastro que miente |
| **La baja pone `mode = off`** en las conversaciones abiertas del agente IA | Escribe el agregado compartido desde `agents/`, y la pausa igual necesitaría una de las otras dos salidas |
| **No validar el modelo**, o copiar la lista al núcleo | La primera deja la falla silenciosa; la segunda crea dos listas que divergen con el primer cambio de `pricing.yaml` |
| **Exponer `default_mode` y cablear `EnsureOpen` ya**, rechazando `suggestion` como ADR-0035 | Adelanta una parte de F02 que el plan ubica en la Fase 9, en una tarea que no lo pedía. El costo de esperar es que el alta escribe un modo fijo |
| **Vincular el agente IA a la bandeja con un flag de CLI o con SQL** | Deja el enganche real fuera de lo que se prueba, que es justo el defecto que el criterio de cierre de la fase quiere evitar |
