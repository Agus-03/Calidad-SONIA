# ADR-0002 — Ciclo de vida de la conversación y vínculo de los recordatorios

| Campo | Valor |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 2026-08-06 |
| **Etapa** | Diseño — Paso 3 |
| **Resuelve** | Hallazgo H-14 de la [matriz de trazabilidad](../01-trazabilidad.md) |
| **Requisitos afectados** | F13, F22, F23, F28, F32, F34, OE7 |

---

## 1. Contexto

Dos cuestiones acopladas, que conviene decidir juntas.

**La visible:** F28 vincula los recordatorios a *contactos* (*"alertas vinculadas a **contactos** con fecha"*); el Anexo C los vincula a conversaciones (`reminders.conversation_id`).

**La de fondo, que el anteproyecto nunca responde:** ¿las conversaciones se cierran? En WhatsApp el cliente ve **un solo chat continuo, para siempre** — no existe "cerrar el ticket" desde su lado. Entonces, ¿qué significa "resolver"?

## 2. Decisión

**a) Las conversaciones se resuelven.** Se conserva `conversations.status` con los tres estados del Anexo C (`active` / `waiting` / `resolved`).

**b) Un mensaje entrante sobre una conversación resuelta crea una conversación nueva**, no reabre la anterior.

**c) `reminders.contact_id` es obligatorio; `reminders.conversation_id` es opcional.**

**d) El cierre es manual, por el momento.** Lo hace el operador desde la bandeja. **El auto-cierre por inactividad queda declarado como mejora futura**, fuera del alcance actual: no está en el anteproyecto y sería un requisito nuevo.

## 3. Justificación

### El chat y la conversación no son lo mismo

- El **chat** es el canal: continuo, uno por contacto, infinito.
- La **conversación** es la unidad de trabajo: acotada, medible, con principio y fin.

Se necesita la segunda porque cuatro requisitos dependen de que exista un cierre:

| Requisito | Por qué necesita el cierre |
|---|---|
| **F13** | Dice textual: *"el operador califica cada conversación **cerrada**"*. Sin cierre no hay qué calificar |
| **F23** | Filtra por "resueltas" |
| **F34** | Cuenta activas / resueltas / en espera |
| **OE7** | Tasa de resolución autónoma ≥ 75 % — necesita un **denominador**: resueltas sin escalada sobre el total de resueltas |

Sin cierre, OE7 se queda sin denominador y la bandeja se vuelve inusable: cada contacto que alguna vez escribió permanece abierto para siempre.

### Por qué una conversación nueva y no reabrir la resuelta

Por **integridad de medición**. Una fila reabierta pasa a tener dos momentos de resolución, un solo campo `score` para dos ciclos distintos y un `first_response_at` ambiguo. Las métricas de F34, F36 y OE7 necesitarían casos especiales para todas ellas.

Con una conversación nueva, cada una es una unidad limpia y medible — y **F32 (historial unificado por contacto) es exactamente el mecanismo, ya presente en los requisitos, que le devuelve al humano la vista continua**.

### Y esto obliga la parte (c)

Si las conversaciones se cierran y nacen nuevas, **un recordatorio colgado de una conversación queda huérfano apenas esa conversación se resuelve** — precisamente el escenario que vuelve inútil el modelo del Anexo C. El requisito tenía razón y el modelo estaba mal. F32 además exige poder llegar a los recordatorios desde el contacto.

## 4. Consecuencias

### Positivas

- Cada conversación es una unidad limpia y medible; OE7 tiene denominador.
- La bandeja se mantiene usable: lo resuelto sale de la vista activa.
- F32 gana peso: pasa a ser la vista que reconstituye la continuidad que el cliente percibe.
- El recordatorio sobrevive al cierre de la conversación que lo originó.

### Negativas / costos asumidos

- **Un contacto recurrente genera muchas filas de conversación.** Se asume: es exactamente lo que vuelve medibles las métricas.
- **Las métricas "por conversación" y "por contacto" dejan de ser intercambiables.** Cada gráfico de F34 y F38 tiene que declarar cuál usa.
- **El cierre manual depende de la disciplina del operador.** Una conversación que el cliente abandona sin avisar queda activa hasta que alguien la cierre. Eso infla el conteo de "activas" de F34 y ensucia el denominador de OE7, porque las conversaciones nunca cerradas no entran en la tasa de resolución. Se asume; el auto-cierre por inactividad queda como mejora futura, y no está en el anteproyecto (sería un requisito nuevo).

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Conversación única perpetua por contacto** | Coherente con lo que ve el cliente, pero deja a OE7 sin denominador y a F13 sin objeto. F23 y F34 pierden dos de sus tres estados. |
| **Reabrir la conversación resuelta** | Es la opción más común en herramientas de ticketing y es defendible, pero rompe la integridad de medición descrita en §3. Sostenerla obligaría a modelar los ciclos por separado — más complejo que crear una fila nueva. |
| **Recordatorios vinculados sólo a conversaciones** (statu quo) | Quedan huérfanos al cierre, y F32 no puede alcanzarlos desde el contacto. |
