# ADR-0004 — Asignación de conversaciones: pool con autoasignación

| Campo | Valor |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 2026-08-06 |
| **Etapa** | Diseño — Paso 3 |
| **Resuelve** | Hallazgo H-15 de la [matriz de trazabilidad](../01-trazabilidad.md) |
| **Requisitos afectados** | F02, F22, F23, F26, F34 · Anexo B (roles) |

---

## 1. Contexto

El Anexo C tiene `conversations.operator_id`, pero **el anteproyecto nunca define cómo se asigna una conversación a un operador**. F26 (transferencia entre operadores) y el rol Super Agent del Anexo B (*"puede reasignar"*) dependen de esa política, y no está escrita en ninguna parte.

Es un hueco de requisitos, no de modelo: ninguna tabla puede resolverlo sin que alguien decida primero cómo trabaja el equipo.

## 2. Decisión

- Una conversación entrante llega **sin operador asignado** (pool).
- **El primer operador que responde queda autoasignado.**
- Además existe **asignación manual explícita**: a pedido del cliente, o reasignación por un `supervisor` o `admin` (F26).
- **No hay round-robin ni balanceo por carga.**
- **`conversations.operator_id` es nullable.**
- Cada asignación se registra en **`conversation_assignments`**: de quién, a quién, quién la hizo, **por qué** y cuándo.

## 3. Justificación

- **Corresponde al modo real de trabajo del cooperante:** un equipo chico, donde la distribución automática resolvería un problema que no tiene.
- **`operator_id` nullable corrige de arrastre un problema de cardinalidad.** Una conversación en modo autónomo, o recién llegada al pool, legítimamente no tiene operador. Un `operator_id` obligatorio **contradiría F02**, que permite que el agente IA opere sin intervención humana.
- **`conversation_assignments` es lo que hace posible F26.** El requisito pide *"reasignación con historial completo"*: con sólo `operator_id`, cada reasignación pisa la anterior y el historial simplemente no existe.
- **El campo `reason` es donde entra "lo pidió el cliente"**, que de otro modo se pierde.

## 4. Consecuencias

### Positivas

- F26 pasa de no implementable a implementable.
- Se puede medir el tiempo en pool antes de la primera respuesta — insumo directo del "tiempo de espera" de F22 y de los tiempos de respuesta de F34.
- El modelo deja de contradecir a F02.

### Negativas / costos asumidos

- **Sin distribución automática, una conversación puede quedar en el pool si nadie la toma.** Se mitiga con la visibilidad que ya piden los requisitos: el filtro "en espera" de F23 y el tiempo de espera de F22. Se asume conscientemente.
- Una tabla más, que crece con cada reasignación.
- Round-robin y balanceo por carga quedan como trabajo futuro si el equipo crece.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Round-robin** | Agrega complejidad de implementación y de prueba sin resolver un problema que exista hoy con un equipo chico. Reconsiderable si el cooperante escala. |
| **Balanceo por carga** (asignar al operador con menos conversaciones activas) | Mismo motivo, y además requiere definir qué cuenta como "carga". |
| **Asignación obligatoria en el alta** | Contradice F02: una conversación en modo autónomo no necesita operador. |
