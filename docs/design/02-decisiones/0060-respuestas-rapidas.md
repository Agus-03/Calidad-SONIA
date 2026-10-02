# ADR-0060 — Las respuestas rápidas: quién las carga, quién las ve y cómo llegan al chat

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · que seleccionar una respuesta la copie en la caja de respuesta y no la envíe (regla 9) y que el título sea único por cuenta (regla 5) los eligió el grupo el 2026-10-02 |
| **Fecha** | 2026-10-02 |
| **Etapa** | Construcción — Fase 11 (resto del CRUD operativo) |
| **Resuelve** | Cómo se construye F09: quién carga, edita y borra una respuesta rápida, quién la ve, cómo se busca y qué pasa cuando el operador la elige en el chat |
| **Requisitos afectados** | F09 · F07 · Anexo B |
| **Enmienda** | — |
| **Relacionada con** | [ADR-0012](0012-cuenta-y-bandejas-de-entrada.md), [ADR-0032](0032-acceso-a-contactos-y-datos-sensibles.md), [ADR-0035](0035-reglas-de-la-bandeja-manual.md), [ADR-0051](0051-bandeja-en-tiempo-real.md) |

---

## 1. Contexto

F09 dice *"El administrador carga textos frecuentes. Los operadores los seleccionan desde el gestor de chats"*. El esquema trae `quick_replies` desde `0001`, con título, contenido, autor, `active` y borrado lógico, y [ADR-0012](0012-cuenta-y-bandejas-de-entrada.md) le dio `account_id`. Faltaba el paquete `quick_replies/` que [`struct.md`](../../struct.md) preveía.

F09 no dice qué pasa cuando el operador elige una respuesta: si se envía o si se puede retocar antes. Tampoco dice si dos respuestas pueden tener el mismo título. F07 lee este mismo catálogo desde el cerebro, pero se construye aparte.

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **Un paquete nuevo, `quick_replies/`, dueño de la tabla.** Rutas: `GET` y `POST /api/v1/quick-replies`, `PATCH` y `DELETE /api/v1/quick-replies/:id`. El paquete Go se llama `quickreplies`, porque Go no usa guiones bajos en los nombres de paquete; el directorio sigue el árbol de [`08-structure-rules.md`](../08-structure-rules.md) |
| 2 | **Carga, edita y borra sólo el `admin`**, que es el rol que F09 nombra. Para los otros dos roles, el router responde 403 |
| 3 | **Los tres roles leen**, porque F09 hace que los operadores elijan desde el chat. El `operator` y el `supervisor` ven sólo las activas; el `admin` ve todas, porque es quien las pausa y tiene que poder encontrarlas para reactivarlas |
| 4 | **La lista se busca con `q`, por título y por contenido**, sin distinguir mayúsculas. El texto se busca tal cual: un `%` o un `_` no son comodines. Se pagina por número de página, como el catálogo de etiquetas |
| 5 | **El título es único por cuenta, sin distinguir mayúsculas**, entre las respuestas no borradas, incluidas las pausadas. Elegido por el grupo el 2026-10-02. Un título repetido es un 409 `quick_reply_title_taken`. Es un índice nuevo en `0001`: `uq_quick_replies_account_id_title` |
| 6 | **Al título se le quitan los espacios de los extremos; el contenido se guarda como se escribió**, con sus saltos de línea. Un título o un contenido vacío es un 400: `invalid_title` o `invalid_content` |
| 7 | **`active` pausa una respuesta sin borrarla.** El `PATCH` es parcial: pausar manda sólo `active`. Borrar es lógico (C09) y libera el título |
| 8 | **El autor sale del token**, en `created_by_operator_id`, nunca del cuerpo |
| 9 | **Seleccionar una respuesta la copia en la caja de respuesta.** El operador la retoca si quiere y la manda con el envío manual que ya existe (`POST /api/v1/conversations/:id/messages`). Elegido por el grupo el 2026-10-02. El núcleo no suma ninguna ruta para enviarla |
| 10 | **Sin variables** del tipo `{{nombre}}`: el anteproyecto no las pide |
| 11 | **Los cambios del catálogo no se publican por el hub** ([ADR-0051](0051-bandeja-en-tiempo-real.md)). No son cambios de una conversación: el operador ve la versión nueva la próxima vez que pide la lista |

## 3. Justificación

- **Sólo el `admin` escribe (regla 2)** porque F09 lo dice. Es el mismo reparto que el catálogo de etiquetas ([ADR-0032](0032-acceso-a-contactos-y-datos-sensibles.md), regla 13): el anteproyecto nombra a quien define el catálogo, y todos lo usan.
- **Copiar y no enviar (regla 9)** porque una respuesta precargada casi siempre necesita un retoque, como el nombre del cliente o una fecha, y así no hace falta inventar variables (regla 10). Además, el texto pasa por las mismas reglas que cualquier respuesta manual: el vendedor de la conversación, la conversación abierta y la ventana de 24 horas ([ADR-0035](0035-reglas-de-la-bandeja-manual.md)). Una ruta propia tendría que repetirlas.
- **Título único (regla 5)** porque el operador elige por el título, y dos "Horarios" en la misma lista lo obligan a abrir las dos para saber cuál es cuál.
- **Que las pausadas sólo las vea el `admin` (regla 3)** porque pausar sirve para sacar una respuesta del chat sin perderla, como una promoción que vuelve cada temporada.
- **Buscar sin comodines (regla 4)** porque el operador escribe texto, no patrones: un "50%" tiene que encontrar "50%".

## 4. Consecuencias

### Positivas

- F09 existe, y F07 tiene el catálogo que va a leer.
- Lo que el operador manda desde una respuesta rápida es un envío manual más: no hay un segundo camino de envío que mantener.
- Una respuesta pausada se recupera con un `PATCH`, sin volver a cargarla.

### Negativas / costos asumidos

- **No queda registro de qué respuesta rápida se usó.** El mensaje sale como texto libre, así que no se puede medir qué respuestas se usan ni cuáles conviene borrar.
- **Una respuesta pausada retiene su título.** Para cargar otra con el mismo nombre hay que borrar la vieja o renombrarla.
- **Un operador con la lista abierta puede elegir una respuesta que el `admin` acaba de pausar o borrar** (regla 11). Como el texto ya está en su caja, se envía igual.
- **El `supervisor` no puede cargar respuestas**, aunque coordine al equipo, porque F09 nombra sólo al administrador.
- **No hay un límite de largo propio.** Un contenido más largo que lo que acepta WhatsApp se carga sin problema y falla recién al enviarse.
- **La búsqueda recorre todo el catálogo**, sin índice. Alcanza para un catálogo de una cuenta; si creciera mucho, haría falta un índice de texto.
- **El índice nuevo cambia `0001`.** Por la regla de [`migrations/README.md`](../../../src/core/migrations/README.md) todavía se puede reescribir, pero cada base existente tiene que recrearse con `docker compose down -v`, como pasó con [ADR-0055](0055-puntaje-de-la-conversacion.md).
- **El paquete se llama distinto que su directorio** (regla 1): quien lo importa tiene que nombrarlo con un alias, como en `deps.go`.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Enviar la respuesta directo, con una ruta propia** | Repite las reglas del envío manual y no deja retocar el texto |
| **Variables que se completan solas, como `{{nombre}}`** | El anteproyecto no las pide; copiar el texto en la caja ya permite completarlo a mano |
| **Que el `supervisor` también cargue** | F09 nombra sólo al administrador |
| **Títulos repetidos** | El operador elige por el título, y dos iguales no se distinguen en la lista |
| **Que los tres roles vean las pausadas** | Una respuesta pausada es justamente una que no tiene que aparecer en el chat |
| **Buscar con `LIKE`** | Convierte el `%` y el `_` que escribe el operador en comodines |
| **Publicar los cambios del catálogo por el hub** | El hub es para los cambios de conversación ([ADR-0051](0051-bandeja-en-tiempo-real.md)); un catálogo que cambia poco no lo justifica |
