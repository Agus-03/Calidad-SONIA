# ADR-0053 — La estrella de seguimiento: quién la pone, sobre qué conversaciones y qué no cambia

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · registrada después de construirla, a pedido de la revisión de diseño de la Fase 10 (2026-09-26): las reglas estaban en el código y en ningún documento |
| **Fecha** | 2026-09-26 |
| **Etapa** | Construcción — Fase 10 (bandeja en tiempo real) |
| **Resuelve** | Qué alcance tiene la estrella de F25, si es por persona o compartida, y qué deja de hacer para no mover la bandeja |
| **Requisitos afectados** | F25 · F23 · F22 · Anexo B |
| **Relacionada con** | [ADR-0035](0035-reglas-de-la-bandeja-manual.md), [ADR-0036](0036-paginacion-por-cursor-en-la-bandeja.md), [ADR-0050](0050-filtros-de-la-bandeja.md), [ADR-0051](0051-bandeja-en-tiempo-real.md), [ADR-0052](0052-marca-de-leida.md) |

---

## 1. Contexto

F25 dice *"Marca conversaciones para priorizar seguimientos. Son filtrables en la bandeja"*. El Anexo C ya tiene `conversations.starred`, y el esquema la trae desde `0001`.

F25 no dice quién pone la estrella, si es de cada persona o de la conversación, ni si una conversación resuelta se puede destacar. La Fase 10 la construyó con reglas que quedaron en el código y en ningún ADR. La revisión de diseño lo señaló, y además encontró que [ADR-0052](0052-marca-de-leida.md) citaba para la estrella una regla de ADR-0035 que sólo habla del modo.

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **`PATCH /api/v1/conversations/:id/starred`**, con `{"starred": true}` o `{"starred": false}`, responde 200 con el estado final. Un cuerpo sin `starred`, o con algo que no es booleano, es un 400 `invalid_body`: `false` es un valor, no una ausencia |
| 2 | **La estrella es una sola por conversación**, compartida por todos los que la ven, como la columna del Anexo C |
| 3 | **Tiene el alcance del hilo** ([ADR-0035](0035-reglas-de-la-bandeja-manual.md), regla 2), para los tres roles: el `operator`, sobre las suyas y las del pool; `supervisor` y `admin`, sobre todas las de la cuenta. Lo que queda fuera de alcance es un 404 `conversation_not_found` ([ADR-0035](0035-reglas-de-la-bandeja-manual.md), regla 3) |
| 4 | **Una conversación resuelta se puede destacar** |
| 5 | **Pedir el estado que ya tiene no escribe** ni avisa a nadie, y responde 200 igual |
| 6 | **Destacar no asigna**, por la misma razón que cambiar el modo no asigna ([ADR-0035](0035-reglas-de-la-bandeja-manual.md), regla 13): asigna responder, y destacar no es responder |
| 7 | **No mueve `last_activity_at`**: la bandeja se ordena por esa columna, y una estrella que subiera la conversación correría los cursores en circulación ([ADR-0036](0036-paginacion-por-cursor-en-la-bandeja.md)) |
| 8 | **`starred` va en la fila del listado**, así que llega con el evento de [ADR-0051](0051-bandeja-en-tiempo-real.md), y es el filtro *destacadas* de [ADR-0050](0050-filtros-de-la-bandeja.md) |

## 3. Justificación

- **Compartida (regla 2)** porque así la define el Anexo C: una columna en `conversations`, no una tabla por persona. *"Priorizar seguimientos"* habla de la conversación, no de quién la mira.
- **El alcance del hilo (regla 3), y no el de responder que usa [ADR-0052](0052-marca-de-leida.md).** Las dos marcas se parecen y se deciden distinto a propósito:
  - La estrella **agrega** información: marca algo para atender después. F25 no nombra un rol, y que un `supervisor` destaque la conversación de un vendedor es justamente priorizarle un seguimiento.
  - La marca de leída **borra** un aviso que es del vendedor. Si el `supervisor` pudiera borrarlo con sólo mirar, el vendedor perdería el aviso de mensajes que nunca vio. Por eso sólo puede borrarla quien puede responder.
- **Una resuelta se puede destacar (regla 4)** porque un seguimiento suele venir después del trabajo: una conversación cerrada puede requerir un llamado la semana que viene.
- **Sin escribir dos veces (regla 5)**, por lo mismo que el modo: pedir el estado que ya tiene es el estado que el frontend quiere, y un evento sin cambio sería ruido en todas las bandejas.

## 4. Consecuencias

### Positivas

- F25 existe con un contrato cerrado, y el filtro *destacadas* de F23 tiene datos.
- Destacar no reordena la bandeja ni corre los cursores.
- Un `supervisor` puede marcarle seguimientos al equipo.

### Negativas / costos asumidos

- **Cualquiera que vea la conversación puede sacar la estrella que puso otro**: un `supervisor` la del vendedor, o un `operator` la de una conversación del pool. Sacarla borra información, y la regla 3 no lo distingue de ponerla.
- **No queda registro de quién la puso ni cuándo.** La columna es un booleano; sin tabla ni auditoría, la estrella no dice de quién es el seguimiento.
- **La estrella sobrevive al cambio de vendedor.** Quien toma una conversación del pool, o quien la reciba cuando exista F26, hereda una estrella que puso otro.
- **Destacar no sube la conversación en la bandeja.** Para ver primero las destacadas hay que usar el filtro.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Una estrella por Operator**, en una tabla nueva | Contradice el Anexo C, que la pone en `conversations`, y agrega una tabla y una migración que F25 no pide |
| **El alcance de responder**, como la marca de leída | Le impediría al `supervisor` marcarle un seguimiento al vendedor, que es para lo que sirve la estrella en un equipo |
| **Que destacar mueva `last_activity_at`** | Subiría la conversación, pero correría los cursores y mezclaría una marca del operador con la actividad del canal (ADR-0036) |
| **Rechazar las conversaciones resueltas** | El seguimiento suele venir después del cierre |
| **Dos rutas, `PUT` y `DELETE`** | Es el mismo cambio de estado, y el modo ya se cambia con un `PATCH` a su propio recurso |
