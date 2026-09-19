# ADR-0036 — La bandeja y el hilo paginan por cursor, no por número de página

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · revisada el 2026-09-13, antes del merge, tras la revisión de diseño: el orden de la bandeja se declara decisión propia y los índices pasan a ser la regla 10 · regla 9 ampliada por [ADR-0038](0038-alta-baja-y-pausa-del-agente-ia.md), reglas 11 y 14: un catálogo que no crece —los agentes IA y los números de la cuenta— no pagina |
| **Fecha** | 2026-09-13 |
| **Etapa** | Construcción — Fase 5 (bandeja manual mínima) |
| **Resuelve** | Cómo se pagina un listado que se reordena mientras se lo lee, y por qué el núcleo pasa a tener dos formas de paginar |
| **Requisitos afectados** | F21, F22, F23 |
| **Relacionada con** | [ADR-0033](0033-secretos-de-bandejas-nullables-y-congelamiento-de-0001.md), [ADR-0035](0035-reglas-de-la-bandeja-manual.md) |

---

## 1. Contexto

Hasta la Fase 4, el núcleo paginaba de una sola manera: `contacts/` y `labels/` reciben `page` y `per_page`, y responden los ítems con el total. Detrás hay un `LIMIT … OFFSET …` sobre un orden alfabético (`lower(lastname), lower(name)` y `lower(name)`), que no cambia mientras alguien recorre las páginas.

La Fase 5 agrega dos listados que no tienen esa propiedad:

- **La bandeja** (`GET /conversations`) se ordena por la última actividad. **Es una decisión nuestra, no de F22**: el requisito pide ver *"último mensaje, tiempo de espera, agente asignado y estado del bot"* sin abrir la conversación, y no dice nada del orden. Se eligió la última actividad porque es lo que el operador tiene que atender primero. La consecuencia es que **cada mensaje entrante o saliente mueve una conversación al principio.**
- **El hilo** (`GET /conversations/:id/messages`) se ordena del más nuevo al más viejo, y **cada mensaje nuevo empuja a todos los demás una posición**.

Con un `OFFSET`, las dos cosas rompen la página siguiente. Si entre la página 1 y la 2 una conversación de la página 2 recibe un mensaje, sube a la página 1 y **todas las demás corren una posición: la primera de la página 2 queda en la 1, ya leída, y nadie la ve**. Si la que se mueve venía de la página 1, pasa lo contrario y una conversación aparece dos veces. En una bandeja con clientes escribiendo, eso no es un caso raro: es lo normal.

La pregunta llegó en la Fase 5 porque es la primera vez que un listado del núcleo se ordena por algo que el canal cambia solo.

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **La bandeja y el hilo paginan por cursor**: reciben `limit` y `cursor`, y responden `{ items, next_cursor }` |
| 2 | **El orden es una marca de tiempo más el `id`, las dos descendentes.** La bandeja, por `last_activity_at DESC, id DESC`. El hilo, por `created_at DESC, id DESC`, y no por `sent_at`. El `id` desempata dos filas con la misma marca de tiempo |
| 3 | **La página siguiente es la que sigue a la última fila entregada**: `(marca, id) < (marca del cursor, id del cursor)`. No hay `OFFSET` |
| 4 | **El cursor es opaco.** Es la marca de tiempo y el `id` de la última fila, en base64url. El frontend lo devuelve tal cual y no lo construye |
| 5 | **Un cursor mal formado es un 400 `invalid_cursor`**, no la primera página |
| 6 | **`next_cursor` es `null` en la última página**, nunca una cadena vacía |
| 7 | **Se lee una fila de más para saber si hay otra página.** Si llega, se descarta y el cursor apunta a la anterior. **No se calcula total** |
| 8 | **`limit` vale 20 por defecto y 100 como máximo.** Pedir más se recorta, no se rechaza, igual que `per_page` en `contacts/` |
| 9 | **`contacts/` y `labels/` siguen por número de página.** El criterio para un listado nuevo: cursor si el orden lo mueve la actividad del canal, número de página si es un catálogo que sólo cambia cuando alguien lo edita |
| 10 | **Los dos órdenes tienen su índice desde `0001`**, antes de que ninguna consulta sea lenta: `idx_conversations_last_activity_at_id` sobre `conversations (last_activity_at, id)`, de las no borradas, y `idx_messages_conversation_id_created_at_id` sobre `messages (conversation_id, created_at, id)`. Es una excepción declarada a la regla de [`migrations/README.md`](../../../src/core/migrations/README.md), decidida por el equipo el 2026-09-13 |

## 3. Justificación

- **El cursor no se corre cuando la lista se mueve.** La página siguiente no se define como *"las filas de la 21 a la 40"*, sino como *"las que vienen después de esta"*. Una conversación que recibe un mensaje sube al principio y deja de estar en las páginas que faltan, pero **ninguna otra cambia de lugar**: no hay repetidas ni salteadas entre las que no se movieron.
- **Lo que se mueve va a donde el operador mira.** La conversación que acaba de recibir un mensaje queda en la primera página, que es la que se vuelve a pedir. Con WebSockets (F21, Fase 10), ese mismo evento es el que la va a empujar a la pantalla.
- **El `id` como desempate es lo que hace estricta la comparación.** Dos conversaciones pueden tener la misma `last_activity_at` al microsegundo. Sin segundo criterio, el cursor no sabría cuál de las dos ya se entregó.
- **El hilo usa `created_at` porque es un solo reloj.** `sent_at` mezcla dos (C06): el de Meta en un entrante y el nuestro en un saliente. Ordenar por él pondría en una misma línea instantes medidos por relojes distintos. `created_at` lo escribe siempre la base.
- **Opaco para no atar el frontend a la consulta.** Lo que lleva el cursor es cómo sigue el `WHERE`. Un cliente que aprendiera a armarlo se rompería el día que cambie el orden, por ejemplo cuando F23 agregue *"en espera"*. Por la misma razón, uno mal formado se rechaza: adivinar devolvería la primera página y el frontend la dibujaría como la siguiente.
- **Una fila de más es la única forma honesta de saber si hay otra página.** Una página llena puede ser la última. Sin la fila extra, el frontend recibiría un cursor que lleva a una página vacía.
- **Los mismos números que `contacts/`**, para que el frontend no tenga que aprender dos topes.
- **No se migra `contacts/` ni `labels/`.** Su orden no se mueve solo, así que el número de página funciona, y además da el total, que en un catálogo sirve (*"143 contactos"*). Cambiarlos sería trabajo sin un problema que lo pida.
- **La regla 10 no espera a una consulta lenta**, que es lo que pide `migrations/README.md`, por dos motivos:
  - **No es adivinar.** La consulta existe y corre en cada carga de la bandeja.
  - **Congelado `0001`, agregarlos es más caro.** El corredor aplica cada archivo dentro de una transacción, así que el índice no se puede construir con `CONCURRENTLY`, y `messages` quedaría bloqueada para escritura mientras se construye.

  Van ascendentes porque PostgreSQL los recorre hacia atrás para el orden descendente, y la comparación de filas del cursor entra como condición del índice, sin paso de ordenamiento. Se verificó con `EXPLAIN` sobre la base sembrada.

## 4. Consecuencias

### Positivas

- La bandeja se puede recorrer mientras entran mensajes sin repetir ni perder conversaciones que no se movieron.
- El hilo de una conversación larga se recorre hacia atrás sin que un mensaje nuevo corra las páginas.
- No hay un `OFFSET` que recorra y descarte filas: con los índices de la regla 10, la página N cuesta lo mismo que la 1.
- La regla de paginación es una sola función para los dos listados, así que no pueden discrepar sobre cuándo una página es la última.

### Negativas / costos asumidos

- **No hay total ni salto a la página N.** La bandeja no puede decir *"37 conversaciones"* ni ir a la página 5. Los contadores que va a pedir F23 (*"en espera"*, *"no leídas"*) necesitan su propia consulta, porque este listado no los da.
- **Una conversación que se mueve durante el recorrido no aparece en las páginas que faltan.** Queda arriba, y el operador la ve sólo si vuelve a pedir la primera página. Hasta la Fase 10, sin WebSockets, **la bandeja no avisa que algo se movió**: depende de que el frontend refresque.
- **El hilo ordena por llegada, no por envío.** Un webhook demorado de un mensaje viejo queda en el hilo después de mensajes que el cliente mandó más tarde, porque `created_at` es cuándo lo registramos. Para leer el orden real del cliente está `sent_at`, que se muestra en cada mensaje.
- **El núcleo tiene dos formas de paginar.** El frontend tiene que manejar las dos, y quien escriba un listado nuevo tiene que elegir. La regla 9 da el criterio, pero es una decisión más para cada listado.
- **Cambiar el orden invalida los cursores en circulación.** Si la bandeja pasa a ordenarse por otra columna, un cursor viejo se va a leer con el orden nuevo y va a continuar desde un lugar sin sentido. No se versiona el cursor: con una sola pantalla que lo consume, se asume que el frontend vuelve a la primera página después de un despliegue.
- **Cada mensaje que entra o sale paga una escritura de índice más** (regla 10), y la regla general de `migrations/README.md` tiene ahora una excepción que hay que conocer.

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
| **Sin paginación: devolver todo** | Con el alcance de 7.3 —una sola cooperante— la bandeja quizás entre en una respuesta, pero el hilo de una conversación larga no tiene techo. Y cambiar a paginado después es romper el contrato con el frontend |
| **Ordenar el hilo por `sent_at`** | Mostraría el orden en que el cliente escribió, pero mezclando dos relojes en una sola línea (C06), y el cursor dependería de un reloj que no controlamos |
| **Esperar a la consulta lenta para indexar, o indexar después en `0002`** | Es lo que pide la regla general. Se descartó porque la consulta ya existe, y porque una vez congelado `0001` el índice de `messages` se construiría bloqueando las escrituras de la tabla que más crece |
