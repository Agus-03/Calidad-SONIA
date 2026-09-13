# ADR-0036 — La bandeja y el hilo paginan por cursor, no por número de página

| Campo | Valor |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 2026-09-13 |
| **Etapa** | Construcción — Fase 5 (bandeja manual mínima) |
| **Resuelve** | Cómo se pagina un listado que se reordena mientras se lo lee, y por qué el núcleo pasa a tener dos formas de paginar |
| **Requisitos afectados** | F21, F22, F23 |
| **Relacionada con** | [ADR-0035](0035-reglas-de-la-bandeja-manual.md) |

---

## 1. Contexto

Hasta la Fase 4, el núcleo paginaba de una sola manera: `contacts/` y `labels/` reciben `page` y `per_page`, y responden los ítems con el total. Detrás hay un `LIMIT … OFFSET …` sobre un orden alfabético (`lower(lastname), lower(name)` y `lower(name)`), que no cambia mientras alguien recorre las páginas.

La Fase 5 agrega dos listados que no tienen esa propiedad:

- **La bandeja** (`GET /conversations`) se ordena por la última actividad, porque F22 pide ver primero lo que acaba de pasar. **Cada mensaje entrante o saliente mueve una conversación al principio.**
- **El hilo** (`GET /conversations/:id/messages`) se ordena del más nuevo al más viejo, y **cada mensaje nuevo empuja a todos los demás una posición**.

Con un `OFFSET`, las dos cosas rompen la página siguiente. Si entre la página 1 y la 2 una conversación de la página 2 recibe un mensaje, sube a la página 1 y **todas las demás corren una posición: la primera de la página 2 queda en la 1, ya leída, y nadie la ve**. Si la que se mueve venía de la página 1, pasa lo contrario y una conversación aparece dos veces. En una bandeja con clientes escribiendo, eso no es un caso raro: es lo normal.

La pregunta llegó en la Fase 5 porque es la primera vez que un listado del núcleo se ordena por algo que el canal cambia solo.

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **La bandeja y el hilo paginan por cursor**: reciben `limit` y `cursor`, y responden `{ items, next_cursor }` |
| 2 | **El orden es una marca de tiempo más el `id`, las dos descendentes.** La bandeja, por `last_activity_at DESC, id DESC`. El hilo, por `created_at DESC, id DESC`. El `id` desempata dos filas con la misma marca de tiempo |
| 3 | **La página siguiente es la que sigue a la última fila entregada**: `(marca, id) < (marca del cursor, id del cursor)`. No hay `OFFSET` |
| 4 | **El cursor es opaco.** Es la marca de tiempo y el `id` de la última fila, en base64url. El frontend lo devuelve tal cual y no lo construye |
| 5 | **Un cursor mal formado es un 400 `invalid_cursor`**, no la primera página |
| 6 | **`next_cursor` es `null` en la última página**, nunca una cadena vacía |
| 7 | **Se lee una fila de más para saber si hay otra página.** Si llega, se descarta y el cursor apunta a la anterior. **No se calcula total** |
| 8 | **`limit` vale 20 por defecto y 100 como máximo.** Pedir más se recorta, no se rechaza, igual que `per_page` en `contacts/` |
| 9 | **`contacts/` y `labels/` siguen por número de página.** El criterio para un listado nuevo: cursor si el orden lo mueve la actividad del canal, número de página si es un catálogo que sólo cambia cuando alguien lo edita |

## 3. Justificación

- **El cursor no se corre cuando la lista se mueve.** La página siguiente no se define como *"las filas de la 21 a la 40"*, sino como *"las que vienen después de esta"*. Una conversación que recibe un mensaje sube al principio y deja de estar en las páginas que faltan, pero **ninguna otra cambia de lugar**: no hay repetidas ni salteadas entre las que no se movieron.
- **Lo que se mueve va a donde el operador mira.** La conversación que acaba de recibir un mensaje queda en la primera página, que es la que se vuelve a pedir. Con WebSockets (F21, Fase 10), ese mismo evento es el que la va a empujar a la pantalla.
- **El `id` como desempate es lo que hace estricta la comparación.** Dos conversaciones pueden tener la misma `last_activity_at` al microsegundo. Sin segundo criterio, el cursor no sabría cuál de las dos ya se entregó.
- **Opaco para no atar el frontend a la consulta.** Lo que lleva el cursor es cómo sigue el `WHERE`. Un cliente que aprendiera a armarlo se rompería el día que cambie el orden, por ejemplo cuando F23 agregue *"en espera"*. Por la misma razón, uno mal formado se rechaza: adivinar devolvería la primera página y el frontend la dibujaría como la siguiente.
- **Una fila de más es la única forma honesta de saber si hay otra página.** Una página llena puede ser la última. Sin la fila extra, el frontend recibiría un cursor que lleva a una página vacía.
- **Los mismos números que `contacts/`**, para que el frontend no tenga que aprender dos topes.
- **No se migra `contacts/` ni `labels/`.** Su orden no se mueve solo, así que el número de página funciona, y además da el total, que en un catálogo sirve (*"143 contactos"*). Cambiarlos sería trabajo sin un problema que lo pida.

## 4. Consecuencias

### Positivas

- La bandeja se puede recorrer mientras entran mensajes sin repetir ni perder conversaciones que no se movieron.
- El hilo de una conversación larga se recorre hacia atrás sin que un mensaje nuevo corra las páginas.
- No hay un `OFFSET` que recorra y descarte filas: con los dos índices del orden (§4.1), la página N cuesta lo mismo que la 1.
- La regla de paginación es una sola función para los dos listados, así que no pueden discrepar sobre cuándo una página es la última.

### Negativas / costos asumidos

- **No hay total ni salto a la página N.** La bandeja no puede decir *"37 conversaciones"* ni ir a la página 5. Los contadores que va a pedir F23 (*"en espera"*, *"no leídas"*) necesitan su propia consulta, porque este listado no los da.
- **Una conversación que se mueve durante el recorrido no aparece en las páginas que faltan.** Queda arriba, y el operador la ve sólo si vuelve a pedir la primera página. Hasta la Fase 10, sin WebSockets, **la bandeja no avisa que algo se movió**: depende de que el frontend refresque.
- **El núcleo tiene dos formas de paginar.** El frontend tiene que manejar las dos, y quien escriba un listado nuevo tiene que elegir. La regla 9 da el criterio, pero es una decisión más para cada listado.
- **Cambiar el orden invalida los cursores en circulación.** Si la bandeja pasa a ordenarse por otra columna, un cursor viejo se va a leer con el orden nuevo y va a continuar desde un lugar sin sentido. No se versiona el cursor: con una sola pantalla que lo consume, se asume que el frontend vuelve a la primera página después de un despliegue.
- **Los dos índices del orden se agregan antes de tener una consulta lenta que los pida** (§4.1). Es una excepción a la regla de [`migrations/README.md`](../../../src/core/migrations/README.md), y cada mensaje que entra o sale paga una escritura de índice más.

### 4.1. Los índices del orden

El 2026-09-13 el equipo decidió agregarlos ya, editando `0001` ([ADR-0033](0033-secretos-de-bandejas-nullables-y-congelamiento-de-0001.md), regla 4):

| Índice | Sirve a |
|---|---|
| `conversations_inbox_order` — `conversations (last_activity_at, id)`, de las no borradas | El orden y el cursor de la bandeja |
| `messages_thread_order` — `messages (conversation_id, created_at, id)` | El hilo y el último mensaje de cada fila de la bandeja |

Van ascendentes porque PostgreSQL los recorre hacia atrás para el orden descendente, y la comparación de filas del cursor entra como condición del índice, sin paso de ordenamiento. Se verificó con `EXPLAIN` sobre la base sembrada.

**Por qué no se esperó a una consulta lenta**, que es lo que pide `migrations/README.md`:
- **No es adivinar.** La consulta existe y corre en cada carga de la bandeja.
- **Congelado `0001`, agregarlos es más caro.** El corredor aplica cada archivo dentro de una transacción, así que el índice no se puede construir con `CONCURRENTLY`, y `messages` quedaría bloqueada para escritura mientras se construye.

### Lo que queda abierto

| Qué | Por qué no se resuelve acá |
|---|---|
| **Cómo convive el cursor con los filtros de F23** | Un filtro que no cambia el orden no afecta al cursor. Uno que lo cambie —ordenar *"en espera"* por tiempo de espera— necesita su propio cursor. Es de la **Fase 10** |

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Número de página, como `contacts/`** | Repite y saltea conversaciones cada vez que una recibe un mensaje mientras se recorre, que en una bandeja es lo normal (§1) |
| **Cursor con los parámetros a la vista** (`?before_at=…&before_id=…`) | Es la misma consulta con el orden expuesto. El frontend aprendería a armarlo, y cambiar el orden rompería el contrato de la API en vez de un detalle interno |
| **Número de página con una foto del instante** (`?as_of=…`) | Cada página se leería como estaba en ese instante. Para eso haría falta la `last_activity_at` que cada conversación tenía entonces, y la tabla guarda sólo la actual. Y aunque se pudiera, mostraría a propósito una bandeja vieja |
| **Sin paginación: devolver todo** | Con el volumen de 7.3 la bandeja quizás entre en una respuesta, pero el hilo de una conversación larga no tiene techo. Y cambiar a paginado después es romper el contrato con el frontend |
