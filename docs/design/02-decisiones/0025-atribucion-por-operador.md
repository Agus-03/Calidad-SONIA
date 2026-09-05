# ADR-0025 — Atribución de conversaciones por operador

| Campo | Valor |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 2026-09-04 |
| **Etapa** | Diseño — Paso 11 anticipado (definición de métrica) |
| **Resuelve** | **H-25** — cuatro reglas de atribución posibles, ninguna elegida |
| **Requisitos afectados** | F35, F16 · F13, F14 |

---

## 1. Contexto

F35 pide *"conversaciones resueltas, ventas generadas, monto atribuido y tasa de conversión **por operador**"*. Para calcular cualquiera de esas hay que decidir de quién es una conversación, y el modelo permite cuatro respuestas con números distintos:

| Regla | Qué mide | Defecto |
|---|---|---|
| `conversations.resolved_by_operator_id` | quién cerró | Como el cierre es manual (ADR-0002), **acredita al que sólo apretó "resolver"** en un chat que atendió la IA |
| `conversations.operator_id` | último asignado | Se sobrescribe; F26 lo vuelve arbitrario, y en modo autónomo es nulo |
| `conversation_assignments` | todos los que intervinieron | Los porcentajes suman más de 100 % |
| `messages.sender_operator_id` | **quién efectivamente respondió** | El más fiel al trabajo hecho, y el más caro de calcular |

**La trampa:** el numerador de la tasa de conversión ya está fijo (`sales.operator_id`). Si el denominador usa otra regla, el cociente mezcla dos atribuciones distintas y no significa nada.

F16 agrega una segunda indefinición: pide una *"comparativa de rendimiento por operador"* sin decir de qué.

## 2. Decisión

**Atribución por `messages.sender_operator_id`:** la conversación se atribuye al operador que **envió más mensajes salientes** en ella. Se aplica al denominador de **todas** las métricas de F35 y F16.

**F16 muestra las tres:** conversaciones resueltas, tasa de conversión y promedio de `conversations.quality_score` (F13). No se elige una sola.

## 3. Justificación

- **Es la única de las cuatro que mide trabajo efectivamente hecho.** Las otras tres acreditan a alguien por haber pasado por la conversación, no por haberla atendido — y separar el trabajo de la IA del trabajo humano es justamente lo que OE7 y F36 existen para medir.
- **Las tres alternativas fallan de forma conocida**, y las tres fallas están en la tabla de arriba: acreditar a quien cerró, un valor que se sobrescribe, o porcentajes que suman más de 100 %.
- **F16 pide una comparativa, no un ranking.** Un solo número esconde el caso del operador que resuelve mucho y vende poco —o al revés—, que es exactamente lo que una comparativa debería mostrar. Las tres ya son calculables sin datos nuevos.

## 4. Consecuencias

### Positivas

- Numerador y denominador de la tasa de conversión comparten atribución, así que el cociente significa algo.
- Las conversaciones resueltas de forma autónoma **no se le acreditan a ningún humano**, que es lo correcto y lo que sostiene OE7.
- Las tres métricas de F16 son calculables con el modelo actual.

### Negativas / costos asumidos

- **Es la más cara de calcular:** exige agregar `messages` —la tabla más grande del sistema— por conversación y operador. Si el Paso 7 muestra que no rinde, la salida es **materializar el resultado, no cambiar la regla**: cambiarla rompería la coherencia con `sales.operator_id`.
- **Las conversaciones sin ningún mensaje de operador no se atribuyen a nadie.** Es correcto —no hubo trabajo humano— pero significa que **la suma por operador no da el total de conversaciones**, y el panel tiene que decirlo o el número se lee como un error.
- **El empate necesita desempate** (dos operadores con la misma cantidad de salientes). Queda como regla de aplicación menor.
- Mostrar tres números en F16 obliga a que el panel explique qué es cada uno, o se comparan peras con manzanas.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **`resolved_by_operator_id`** | Acredita el trabajo de la IA al humano que apretó "resolver", que es lo contrario de lo que F36 y OE7 miden. |
| **`conversations.operator_id`** | Se sobrescribe con cada reasignación (F26) y es nulo en modo autónomo: atribuye por un estado, no por un hecho. |
| **`conversation_assignments`** | Atribuye a todos los que pasaron, así que los porcentajes por operador suman más de 100 % y la tasa de conversión deja de ser una proporción. |
| **Elegir una sola métrica de "rendimiento" para F16** | El requisito pide una comparativa. Un número único esconde justamente la variación que la comparativa tiene que mostrar. |
