# ADR-0059 — El registro de ventas: a quién se acredita, quién la marca y cómo se guarda el monto

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · acreditar al vendedor asignado (regla 2), quién marca (regla 3), qué es *"el agente que la gestionó"* (regla 6), varias ventas en cualquier estado (regla 5) y dejar el pendiente de F35 para cuando se construya F35 (regla 11) los eligió el grupo el 2026-10-02 |
| **Fecha** | 2026-10-02 |
| **Etapa** | Construcción — Fase 11 (resto del CRUD operativo) |
| **Resuelve** | Cómo se construye F14: a quién se acredita una venta, quién puede marcarla, a qué agente y canal se vincula, y cómo viaja el monto sin perder precisión |
| **Requisitos afectados** | F14 · F15 · F35 · F16 · Anexo B |
| **Enmienda** | [ADR-0037](0037-toda-conversacion-nace-con-vendedor.md) §4, *Lo que queda abierto* — F14 ya está construido, y el numerador de F35 queda para cuando se construya F35 |
| **Relacionada con** | [ADR-0025](0025-atribucion-por-operador.md), [ADR-0035](0035-reglas-de-la-bandeja-manual.md), [ADR-0054](0054-cierre-manual-de-la-conversacion.md), [ADR-0056](0056-transferencia-entre-operadores.md) |

---

## 1. Contexto

F14 dice *"El operador marca una conversión de venta y la vincula al canal de origen y al agente que la gestionó"*. El esquema trae `sales` desde `0001`: la conversación, el operador (obligatorio), el agente IA (opcional), el monto en `numeric(14,2)` y la moneda con ARS por defecto. Faltaba el paquete `sales/` que [`struct.md`](../../struct.md) preveía.

Es la tarea de la fase que más pesa en las métricas: `sales.operator_id` es el numerador de F35 (*"ventas generadas, monto atribuido y tasa de conversión por operador"*) y alimenta F16. [ADR-0025](0025-atribucion-por-operador.md) fijó el numerador de la tasa en `sales.operator_id` y el denominador en quien mandó más mensajes. [ADR-0037](0037-toda-conversacion-nace-con-vendedor.md) §4 dejó escrito que eso puede dar una tasa mayor al 100 %, y lo difirió a cuando se construyeran F14 y F35.

F14 no dice a quién se acredita la venta si la marca otro, ni qué es *"el agente que la gestionó"*: en el anteproyecto, *agente* nombra al agente IA y, a veces, a la persona.

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **Un paquete nuevo, `sales/`, dueño de la tabla.** Rutas: `POST` y `GET /api/v1/conversations/:id/sales`, `PATCH` y `DELETE /api/v1/sales/:id`. Abiertas a los tres roles |
| 2 | **La venta se acredita siempre al vendedor asignado a la conversación cuando se marca** (`sales.operator_id`), aunque la marque otro. Elegido por el grupo el 2026-10-02 |
| 3 | **La marca el vendedor de la conversación, y además `supervisor` y `admin`** sobre cualquiera de la cuenta, el alcance del cierre ([ADR-0054](0054-cierre-manual-de-la-conversacion.md)). Elegido por el grupo el 2026-10-02. Para un `operator`, la conversación de otro vendedor no existe: 404 `conversation_not_found` |
| 4 | **Una conversación del pool no admite ventas**: no hay vendedor a quien acreditarlas. Es un 409 `conversation_not_assigned`, para cualquier rol: primero alguien la toma |
| 5 | **Se marca en cualquier estado, y una conversación admite varias ventas.** Elegido por el grupo el 2026-10-02 |
| 6 | ***"El agente que la gestionó"* es el agente IA de la conversación**, copiado a `sales.ai_agent_id` al marcar. La persona queda en `operator_id`, así que la venta guarda a los dos. Elegido por el grupo el 2026-10-02 |
| 7 | **El canal de origen no se copia**: se lee del contacto (`source_channel`, F15) y viaja en la respuesta |
| 8 | **El monto viaja como texto decimal** (`"1234.50"`), nunca como número JSON: positivo, con hasta doce enteros y dos decimales, el `numeric(14,2)` del esquema (C08). Si no, 400 `invalid_amount`. La base lo convierte, sin punto flotante en el medio |
| 9 | **La moneda es un código ISO 4217 de tres letras mayúsculas**, con ARS por defecto, como el esquema. Si no, 400 `invalid_currency` |
| 10 | **Corrige y borra el vendedor acreditado, y además `supervisor` y `admin`**; otro `operator` recibe un 403 `not_the_vendor`. Sólo se corrigen el monto y la moneda: el vendedor y el agente quedan como al marcar. Borrar es lógico (C09) |
| 11 | **La tasa mayor al 100 % queda para F35.** Hoy no se calcula ninguna tasa, y la regla de atribución es de H-25 (Paso 11). Elegido por el grupo el 2026-10-02 |
| 12 | **Las ventas no van en la fila de la bandeja ni se publican por el hub**: son para los reportes, no para atender |

## 3. Justificación

- **Al vendedor asignado (regla 2)** porque es quien atiende al cliente ([ADR-0037](0037-toda-conversacion-nace-con-vendedor.md)). Acreditar a quien marca dejaría que un `supervisor` sumara a su numerador las ventas de su equipo con sólo marcarlas.
- **El alcance del cierre (regla 3)**, porque marcar una venta es, como cerrar, un acto sobre el resultado de la atención, y el `supervisor` tiene que poder marcar la de un vendedor que no lo hizo.
- **Rechazar las del pool (regla 4)** porque `sales.operator_id` es obligatorio, y la otra salida —acreditar a quien marca— contradice la regla 2.
- **Copiar el agente IA y no el canal (reglas 6 y 7).** El agente de una conversación puede quedar dado de baja ([ADR-0038](0038-alta-baja-y-pausa-del-agente-ia.md)), y la venta tiene que conservar el que la gestionó. El canal de origen es del contacto y no cambia con la venta: copiarlo sería duplicar F15.
- **El monto como texto (regla 8)** porque un número JSON se decodifica a punto flotante antes de que nadie pueda validarlo, y C08 prohíbe el punto flotante en el dinero. El texto llega intacto a `numeric`.
- **Diferir la tasa (regla 11)** porque F14 sólo registra: la tasa se calcula en F35, y decidirla ahora sería decidir H-25 sin el Paso 11.

## 4. Consecuencias

### Positivas

- F14 existe, y F35 y F16 tienen su numerador: ventas y monto por vendedor.
- El monto se guarda y se lee tal como se escribió, sin redondeos.
- La venta conserva el agente IA y el vendedor de su momento, aunque después cambien.

### Negativas / costos asumidos

- **La tasa de conversión de F35 todavía puede pasar el 100 %.** El numerador acredita al vendedor asignado, y el denominador de [ADR-0025](0025-atribucion-por-operador.md) cuenta mensajes: en una conversación que cerró el bot, la venta suma y la conversación no. Se resuelve al construir F35.
- **Una conversación del pool no admite ventas** (regla 4). Si el cliente compró y nadie tomó la conversación, alguien tiene que tomarla antes de marcar.
- **El vendedor no cambia si la conversación se reasigna después de la venta.** La venta queda con quien la tenía al marcarla, aunque otro la cierre.
- **No queda registro de quién marcó la venta**, sólo de a quién se acreditó. Si la marcó un `supervisor`, el esquema no lo dice.
- **No hay un tipo de cambio.** Una venta en USD y otra en ARS no se pueden sumar; F35 tiene que agrupar por moneda o elegir una.
- **Cualquier `operator` puede ver las ventas de las conversaciones del pool**, que tienen el alcance del hilo, aunque nunca las tengan, porque el pool no admite ventas.
- **El agente IA copiado puede estar dado de baja** después. La venta lo conserva igual, que es lo que F14 pide.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Acreditar a quien marca la venta** | Un `supervisor` sumaría a su numerador las ventas de su equipo con sólo marcarlas |
| **Que sólo el vendedor marque** | Una venta que el vendedor olvidó marcar no tendría cómo entrar |
| **Acreditar a quien marca cuando la conversación es del pool** | Mezcla dos reglas de atribución en el mismo numerador, que es la trampa de ADR-0025 |
| **Leer *"el agente"* como la persona** | La persona ya queda en `operator_id`; el agente IA es lo único que la venta no tendría sin `ai_agent_id` |
| **Copiar el canal de origen a `sales`** | Duplica F15, y el esquema no tiene la columna |
| **El monto como número JSON** | Pasa por punto flotante antes de llegar a `numeric` (C08) |
| **Resolver ahora la tasa de F35** | Es la regla de atribución de H-25, que pertenece al Paso 11, y F14 no calcula ninguna tasa |
