# ADR-0050 — Los filtros de la bandeja: qué lee cada uno, cómo se combinan y cómo conviven con el cursor

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · la opción de construir los siete filtros ahora, aunque cuatro den vacío hasta que algo escriba lo que leen (§2.3), la eligió el grupo el 2026-09-26 · **§4 y §5 enmendados por [ADR-0052](0052-marca-de-leida.md)** (2026-09-26): *no leídas* deja de devolver vacío, porque cada mensaje del cliente suma y leer o responder lo borra, y la alternativa de §5 que descartaba escribir `unread_count` en esta fase queda revertida en esa mitad |
| **Fecha** | 2026-09-26 |
| **Etapa** | Construcción — Fase 10 (bandeja en tiempo real) |
| **Resuelve** | Qué lee cada filtro de F23, cómo se combinan, qué pasa con los que todavía no tienen datos, y el pendiente de [ADR-0036](0036-paginacion-por-cursor-en-la-bandeja.md): cómo convive el cursor con los filtros |
| **Requisitos afectados** | F23 · F22 · F24 · F25 · F28 · Anexo B |
| **Enmienda** | [ADR-0035](0035-reglas-de-la-bandeja-manual.md), *Lo que queda abierto* — la fila *"Qué filtros ofrece la bandeja"*, que los mandaba a la Fase 10, queda resuelta · [ADR-0036](0036-paginacion-por-cursor-en-la-bandeja.md), *Lo que queda abierto* — ningún filtro cambia el orden, así que el cursor de la bandeja sirve igual para un listado filtrado y no hace falta un cursor propio |
| **Relacionada con** | [ADR-0002](0002-ciclo-de-vida-conversacion-y-recordatorios.md), [ADR-0003](0003-etiquetas-catalogo-y-relaciones.md), [ADR-0004](0004-asignacion-de-conversaciones.md), [ADR-0035](0035-reglas-de-la-bandeja-manual.md) |

---

## 1. Contexto

F23 pide *"filtros combinables: no leídas, asignadas, en espera, resueltas, por etiqueta, con recordatorio, destacadas"*. No dice qué lee cada uno ni qué significa *combinables*.

Al llegar a la Fase 10, el esquema tiene una columna o una tabla para cada filtro, pero **cuatro de ellos no tienen nada que los alimente**:

| Filtro | Qué lee | Quién lo escribe |
|---|---|---|
| No leídas | `conversations.unread_count` | Nadie todavía → **desde [ADR-0052](0052-marca-de-leida.md)**, cada mensaje del cliente suma, y leer o responder lo borra |
| Resueltas | `conversations.status = 'resolved'` | Nadie todavía: el cierre manual de [ADR-0002](0002-ciclo-de-vida-conversacion-y-recordatorios.md) (d) no está construido |
| Por etiqueta | `conversation_labels` | F24, en la **Fase 11** |
| Con recordatorio | `reminders` | F28, en la **Fase 11** |

Los otros tres sí tienen datos: *asignadas* (`operator_id`, desde la Fase 5), *en espera* (`status = 'waiting'`, que escribe el turno desde la Fase 9) y *destacadas* (`starred`, F25, desde esta fase).

[ADR-0036](0036-paginacion-por-cursor-en-la-bandeja.md) dejó abierto cómo convive el cursor con los filtros: un filtro que no cambia el orden no lo afecta, y uno que lo cambia necesita su propio cursor.

## 2. Decisión

### 2.1 Qué lee cada filtro

| # | Filtro | Parámetro | Regla |
|---|---|---|---|
| 1 | No leídas | `unread=true` | `unread_count > 0` |
| 2 | Asignadas | `assigned=true` | `operator_id IS NOT NULL`: la conversación tiene vendedor, sea quien sea |
| 3 | En espera | `status=waiting` | El estado del Anexo C |
| 4 | Resueltas | `status=resolved` | El estado del Anexo C. `status` acepta los tres valores de `ConversationStatus`, también `active` |
| 5 | Por etiqueta | `label_id=<uuid>` | La conversación tiene esa etiqueta, y la etiqueta no está borrada. Una etiqueta por consulta |
| 6 | Con recordatorio | `reminder=true` | **El contacto** de la conversación tiene al menos un recordatorio no borrado, sin importar su fecha ni si ya se notificó |
| 7 | Destacadas | `starred=true` | `starred` (F25) |

### 2.2 Cómo se combinan

| # | Regla |
|---|---|
| 8 | **Los filtros se combinan con AND.** Dos filtros juntos devuelven lo que cumple los dos |
| 9 | **Todo filtro se aplica dentro del alcance del rol** ([ADR-0035](0035-reglas-de-la-bandeja-manual.md), regla 1): achica lo que el rol ve, nunca lo agranda. Para el `operator`, *asignadas* son las suyas, porque no ve las de otros |
| 10 | **Una bandera se prende sólo con `true`.** Cualquier otro valor, incluido `false`, es un 400 `invalid_filter`: para no filtrar se omite el parámetro. Un `status` fuera de los tres es 400 `invalid_status`, y un `label_id` que no es un uuid, 400 `invalid_label_id` |
| 11 | **Ningún filtro cambia el orden.** La bandeja filtrada se ordena igual que la completa, por `last_activity_at DESC, id DESC`, y pagina con **el mismo cursor** de ADR-0036. El cursor no lleva el filtro: el frontend manda el mismo filtro con cada página, y si lo cambia vuelve a la primera |
| 12 | **No hay índices nuevos.** Rige la regla general de [`migrations/README.md`](../../../src/core/migrations/README.md): se indexa cuando una consulta sea lenta, no antes |

### 2.3 Los filtros sin datos se construyen igual

| # | Regla |
|---|---|
| 13 | **Los siete filtros existen desde esta fase**, aunque *no leídas*, *resueltas*, *por etiqueta* y *con recordatorio* devuelvan vacío hasta que algo escriba lo que leen. Elegido por el grupo el 2026-09-26 |

## 3. Justificación

- **AND es la única lectura de *combinables* que un filtro de bandeja admite sin decir nada más.** Con OR, sumar un filtro agrandaría el listado, y ninguna bandeja se usa así. Si hiciera falta OR, sería un requisito nuevo.
- **La regla 9 es la misma que protege el hilo.** Si un filtro pudiera ampliar el alcance, *asignadas* le mostraría al `operator` las conversaciones de otros, que el Anexo B le prohíbe ver.
- ***Asignadas* como *"tiene vendedor"* sirve a los tres roles con una sola regla.** Para el `operator` coincide con *"las mías"* por la regla 9, y para `supervisor` y `admin` separa lo que tiene dueño del pool ([ADR-0004](0004-asignacion-de-conversaciones.md)). Con [ADR-0037](0037-toda-conversacion-nace-con-vendedor.md), casi toda conversación tiene vendedor, así que el filtro sirve sobre todo para encontrar lo que quedó en el pool.
- **El recordatorio se lee por contacto porque cuelga del contacto** ([ADR-0002](0002-ciclo-de-vida-conversacion-y-recordatorios.md) (c)). Un recordatorio creado en una conversación ya resuelta sigue marcando la conversación nueva del mismo cliente, que es para lo que F28 lo pide. **No se mira la fecha ni `notified`**, porque ninguno dice que el recordatorio se atendió: el esquema no tiene un estado *"hecho"*, y lo único que lo cierra es borrarlo.
- **La regla 10 prefiere un 400 a adivinar**, por la misma razón que ADR-0036 rechaza un cursor mal formado. Si `unread=1` se leyera como *"sin filtro"*, el frontend dibujaría la bandeja entera con el nombre del filtro encima.
- **La regla 11 cierra el pendiente de ADR-0036 sin cursor nuevo.** El cursor sólo depende del orden, y el filtro sólo quita filas: la página siguiente sigue siendo *"las que vienen después de esta"*, dentro de las que cumplen el filtro. Un orden propio para *en espera* —por tiempo de espera— es lo único que habría pedido otro cursor, y F23 no lo pide.
- **La regla 13 fija ya el contrato con el frontend**, que es de otro integrante y avanza en paralelo. Las consultas son baratas y el día que F24 y F28 escriban sus tablas, los filtros funcionan sin tocar la API.

## 4. Consecuencias

### Positivas

- Los siete filtros de F23 existen, con un contrato de API cerrado.
- El cursor de ADR-0036 sirve para toda la bandeja, filtrada o no: una sola regla de paginación.
- Un filtro nunca le muestra a un rol lo que no le corresponde, porque se aplica después del alcance.

### Negativas / costos asumidos

- **Cuatro filtros devuelven vacío hasta que algo los alimente.** ~~*No leídas* y *resueltas* no tienen fase asignada: nada escribe `unread_count` ni cierra una conversación.~~ **→ Enmendado por [ADR-0052](0052-marca-de-leida.md):** *no leídas* ya se alimenta, y *resueltas* quedó en la Fase 11, con el cierre manual (pendiente del README). *Por etiqueta* y *con recordatorio* esperan a F24 y F28 (Fase 11). **Una bandeja vacía con el filtro puesto se lee igual que "no hay ninguna"**, y el frontend tiene que saberlo para no presentarlo como un resultado.
- ***En espera* no se ordena por tiempo de espera.** Se ordena por última actividad, como el resto. Para ver primero la que más espera, el frontend tiene que ordenar la página que recibió, y eso no ordena las páginas siguientes.
- **Cambiar de filtro vuelve a la primera página.** Un cursor de un listado usado con otro filtro continúa desde una posición que no significa nada en el nuevo, y la API no lo detecta.
- **Una sola etiqueta por consulta.** Filtrar por *"Ventas o Reclamos"* necesita dos consultas.
- ***Con recordatorio* marca también recordatorios viejos** que nadie borró, porque no hay estado *"hecho"*. Si F28 agrega uno, esta regla se revisa.
- **No hay contadores por filtro** (*"12 en espera"*). F23 no los pide, y el cursor no da totales (ADR-0036).
- **Sin índices, un filtro sobre muchas filas recorre la bandeja entera.** Con el volumen de 7.3 no pesa; si pesa, el índice se agrega por la regla general.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Construir sólo los tres filtros con datos** y dejar los otros cuatro para la Fase 11 | Deja el contrato con el frontend incompleto y obliga a cambiar la API en la Fase 11. El grupo prefirió cerrarlo ahora y asumir los filtros vacíos |
| **Sumar a esta fase la escritura de `unread_count` y el cierre de conversaciones** | Son dos funcionalidades nuevas, sin ADR, colgadas de una tarea de filtros. Agrandan la fase sin un requisito que las ubique acá. **→ Revertida en parte por [ADR-0052](0052-marca-de-leida.md)**: el grupo sumó a esta fase la escritura de `unread_count`; el cierre sigue afuera |
| **Combinar con OR** | Sumar un filtro agrandaría el listado. Ninguna lectura de F23 lo pide |
| **Aceptar `false` como *"filtrar lo contrario"*** (`assigned=false` para el pool) | F23 enumera filtros, no sus negaciones: sería agregar filtros que el requisito no pide |
| **Un cursor propio por filtro**, con el orden de cada uno | Sólo *en espera* tendría un orden distinto, y F23 no lo pide. Dos cursores son dos reglas de paginación que pueden discrepar |
| **El recordatorio por conversación** (`reminders.conversation_id`) | Contradice [ADR-0002](0002-ciclo-de-vida-conversacion-y-recordatorios.md) (c): perdería los recordatorios de conversaciones ya resueltas del mismo cliente |
