# ADR-0020 — Idempotencia de los mensajes salientes: *at-most-once*

| Campo | Valor |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 2026-09-04 |
| **Etapa** | Diseño — Paso 7 (nivel físico) |
| **Resuelve** | **H-29** — los salientes no tienen con qué reconocer un reintento |
| **Requisitos afectados** | F20, F21, F29 · C09 |

---

## 1. Contexto

Para los mensajes **entrantes** el problema está resuelto: Meta asigna un `wamid`, la columna es única, y si Meta reintenta el webhook el segundo `INSERT` choca contra la restricción. La deduplicación es una garantía del modelo (H-08).

Para los **salientes** no hay equivalente. El `wamid` lo devuelve Meta *después* de aceptar el envío, así que una llamada cortada por timeout deja al sistema sin saber si el mensaje salió o no. Y **`messages` no admite borrado ni corrección** (F29, C09): un duplicado queda en el historial para siempre, y es el historial sobre el que se calculan F34, F35, F36 y OE7.

La pregunta es una sola: ante la duda, ¿se reintenta y se arriesga un duplicado, o no se reintenta y se arriesga una pérdida?

## 2. Decisión

**At-most-once.**

- Antes de llamar a Meta se persiste la fila con el estado del intento en curso.
- Si la llamada no confirma, **no se reintenta automáticamente**: la fila queda `failed` con su `error_code`, visible en la bandeja.
- **El reenvío es una acción del operador**, y produce una **fila nueva** — nunca una corrección de la anterior, que F29 prohíbe.

## 3. Justificación

- **El duplicado es irreversible y el fallido no.** Un mensaje repetido le llega al cliente, parece un sistema roto y no se puede limpiar (F29). Un fallido es visible, tiene dueño humano y su costo es un reenvío.
- **La Cloud API de Meta no acepta una clave de idempotencia del cliente**, así que "reintentar sin duplicar" no es implementable de nuestro lado. La elección no es entre tres opciones: es entre duplicar y perder.
- **Es coherente con la regla que el Paso 8 ya fijó para el cerebro caído:** no se reprocesa automáticamente, porque una respuesta tardía sobre un contexto que ya avanzó es peor que ninguna respuesta. Misma lógica, otro punto del turno.

## 4. Consecuencias

### Positivas

- Ningún duplicado permanente en la tabla sobre la que se calculan todas las métricas.
- La falla es visible y tiene responsable, en vez de resolverse sola y en silencio.
- Consistente con la política de reintentos ya decidida para el salto al cerebro.

### Negativas / costos asumidos

- **Se pierden mensajes en el caso ambiguo.** Si Meta recibió el envío y la respuesta se cortó, la fila queda `failed` **y el cliente recibió el mensaje igual**. Si el operador lo reenvía, duplica. El problema no desaparece: se mueve a una decisión humana informada, que es donde tiene arreglo.
- **Depende de que la bandeja muestre los fallidos de forma prominente**, o la política se vuelve "se perdió y nadie se enteró". Es trabajo de frontend que hay que planificar explícitamente, no un detalle de presentación.
- Agrega un estado intermedio al ciclo de vida del saliente, que hay que reflejar en `MessageStatus` o en la regla de aplicación que lo interprete.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **At-least-once: reintentar hasta confirmar** | Produce duplicados permanentes (F29, C09) en la tabla de hechos sobre la que se miden OE7 y F34–F36. |
| **Consultar a Meta si el mensaje salió** | La Cloud API no ofrece una consulta confiable de "¿recibiste este envío?" sin el `wamid`, que es justamente lo que falta. |
| **Clave de idempotencia propia enviada a Meta** | Meta no la acepta; la garantía tendría que vivir de su lado y no existe. |
