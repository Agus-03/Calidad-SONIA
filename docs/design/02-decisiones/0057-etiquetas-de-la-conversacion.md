# ADR-0057 — Las etiquetas de una conversación: quién etiqueta, sobre cuáles y cómo llegan a la bandeja

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · el alcance del hilo (regla 2), etiquetar también las resueltas (regla 3) y las etiquetas en la fila de la bandeja (regla 8) los eligió el grupo el 2026-10-01 |
| **Fecha** | 2026-10-01 |
| **Etapa** | Construcción — Fase 11 (resto del CRUD operativo) |
| **Resuelve** | Cómo se construye F24: quién asigna etiquetas a una conversación, sobre cuáles, qué pasa al repetir o quitar, y cómo se enteran las bandejas abiertas |
| **Requisitos afectados** | F24 · F19 · F23 · F21 · Anexo B |
| **Enmienda** | [ADR-0050](0050-filtros-de-la-bandeja.md) §1 y §4 — *por etiqueta* deja de devolver vacío · [ADR-0051](0051-bandeja-en-tiempo-real.md), regla 13 y §4 — `inbox/` publica al etiquetar, y la fila trae las etiquetas, así que el costo de los filtros queda sólo para *con recordatorio* |
| **Relacionada con** | [ADR-0003](0003-etiquetas-catalogo-y-relaciones.md), [ADR-0032](0032-acceso-a-contactos-y-datos-sensibles.md), [ADR-0035](0035-reglas-de-la-bandeja-manual.md), [ADR-0053](0053-estrella-de-seguimiento.md) |

---

## 1. Contexto

F24 dice *"El operador asigna etiquetas del catálogo directamente desde el chat"*, y F19 le da el catálogo al `admin`. [ADR-0003](0003-etiquetas-catalogo-y-relaciones.md) creó `conversation_labels` para que F24 fuera representable, y [ADR-0050](0050-filtros-de-la-bandeja.md) construyó el filtro *por etiqueta*, que devolvía vacío porque nadie escribía la tabla.

Hay un precedente casi igual: las etiquetas de contactos, que `contacts/` asigna y quita desde la Fase 6 ([ADR-0032](0032-acceso-a-contactos-y-datos-sensibles.md), regla 13). F24 no dice qué rol etiqueta, si una resuelta se etiqueta ni cómo se ven las etiquetas en la bandeja.

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **`POST /api/v1/conversations/:id/labels`**, con `{"label_id": …}`, y **`DELETE /api/v1/conversations/:id/labels/:label_id`**. Las dos responden 200 con las etiquetas que quedaron |
| 2 | **Etiqueta quien puede leer la conversación** (el alcance del hilo, [ADR-0035](0035-reglas-de-la-bandeja-manual.md), regla 2), en los tres roles, como la estrella ([ADR-0053](0053-estrella-de-seguimiento.md)). Elegido por el grupo el 2026-10-01 |
| 3 | **Una conversación resuelta se etiqueta también.** Elegido por el grupo el 2026-10-01 |
| 4 | **Sólo etiquetas del catálogo de la cuenta y no retiradas.** Lo pregunta `inbox/` a `labels/`, el dueño del catálogo. Otra es un 404 `label_not_found` |
| 5 | **Pedir el estado que ya existe no es un error**: etiquetar dos veces deja un solo vínculo (la restricción única de la tabla), y quitar una etiqueta que no está responde 200, como en las etiquetas de contactos |
| 6 | **Quitar es un borrado físico**, la excepción a C09 que el esquema declara para esta tabla. Se graba quién etiquetó en `assigned_by_operator_id` |
| 7 | **Etiquetar no asigna** ([ADR-0035](0035-reglas-de-la-bandeja-manual.md), regla 13) **ni mueve `last_activity_at`** ([ADR-0053](0053-estrella-de-seguimiento.md), regla 7) |
| 8 | **Las etiquetas van en la fila de la bandeja**, con su nombre y color, ordenadas por nombre; una lista vacía y nunca `null`. Las retiradas del catálogo no aparecen, como en el filtro (ADR-0050, regla 5). Elegido por el grupo el 2026-10-01 |
| 9 | **`inbox/` publica `conversation.updated` después del commit, y sólo si un vínculo cambió** ([ADR-0051](0051-bandeja-en-tiempo-real.md), regla 13) |

## 3. Justificación

- **El alcance del hilo (regla 2)** porque etiquetar agrega información, igual que la estrella: que un `supervisor` segmente las conversaciones de su equipo es justamente para lo que sirve el catálogo de F19. F24 dice *"el operador"* sin restringir.
- **Las resueltas también (regla 3)** porque etiquetar sirve para segmentar, y lo que más se segmenta es lo que ya terminó.
- **Desde el catálogo, preguntándole a `labels/` (regla 4)**, por la regla 1 de la estructura: el catálogo no es tabla de `inbox/`. Es lo que ya hace `contacts/`.
- **El estado final (regla 5)**, por lo mismo que la estrella y las etiquetas de contactos: lo que pide el frontend es cómo tiene que quedar la conversación, y después del segundo pedido queda así.
- **En la fila (regla 8)**, como hace la mayoría de los CRM, porque el frontend las dibuja sin otro pedido y, sobre todo, porque con la fila puede decidir con el evento si la conversación sigue entrando en el filtro *por etiqueta*. Ese era un costo declarado de [ADR-0051](0051-bandeja-en-tiempo-real.md) §4.
- **Publicar sólo si cambió (regla 9)**, porque un evento sin cambio es ruido en todas las bandejas.

## 4. Consecuencias

### Positivas

- F24 existe, y el filtro *por etiqueta* de F23 tiene datos.
- El frontend ve las etiquetas sin abrir la conversación, y el evento le alcanza para el filtro *por etiqueta*.
- Las etiquetas de conversaciones y de contactos se comportan igual.

### Negativas / costos asumidos

- **Cada fila de la bandeja cuesta una lectura más**: las etiquetas se arman por conversación en la misma consulta del listado y de cada evento.
- **Cualquiera que vea la conversación puede sacar una etiqueta que puso otro**, también un `operator` en una del pool. Quitar borra información, y la regla 2 no lo distingue de poner.
- **No queda historial.** El borrado es físico: no se sabe quién sacó una etiqueta ni cuándo, y quien la puso se pierde con ella.
- **Retirar una etiqueta del catálogo no avisa a las bandejas.** La etiqueta deja de aparecer en cada fila recién con el próximo evento de esa conversación, porque `labels/` no conoce el hub.
- **Hay una ventana entre preguntar al catálogo y escribir el vínculo.** Una etiqueta retirada en ese instante queda vinculada, pero nunca se ve, porque la fila y el filtro sólo leen las no retiradas. Es el mismo costo que ya asumían las etiquetas de contactos.
- **Etiquetar una conversación no etiqueta al contacto**, ni al revés. Son dos relaciones distintas ([ADR-0003](0003-etiquetas-catalogo-y-relaciones.md)), y el frontend tiene que dejarlo claro.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **El alcance de responder** | Le impediría al `supervisor` segmentar las conversaciones de su equipo |
| **No etiquetar las resueltas** | Segmentar lo que ya terminó es el uso principal de una etiqueta |
| **Una ruta aparte para leer las etiquetas**, sin ponerlas en la fila | Obliga a un pedido por conversación para dibujar la bandeja, y deja el filtro *por etiqueta* sin poder decidirse con el evento |
| **Rechazar con 409 etiquetar dos veces, o 404 quitar una que no está** | Distinto de las etiquetas de contactos y de la estrella, sin una razón de dominio |
| **Validar la etiqueta con SQL propio** en `inbox/` | Duplica la regla de qué etiqueta es válida, que es de `labels/`. Leerla para dibujar la fila sí se hace, como ya la lee el filtro |
