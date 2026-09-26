# ADR-0046 — El modo sugerencia: el turno se arma igual, y la sugerencia es un borrador que el Operator puede copiar

| Campo | Valor |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 2026-09-26 |
| **Etapa** | Construcción — Fase 9 (contexto dinámico y transferencia) |
| **Resuelve** | Si una conversación en modo `suggestion` arma turnos, qué recibe el Operator, cómo se registra una sugerencia que se usa y una que se ignora, y cómo se sostiene el alcance de OE6 con esos turnos en la base |
| **Requisitos afectados** | F02 · F27 · F34 · F36 · OE3 · OE6 · OE7 |
| **Naturaleza** | F02 dice *"sugerencia (el operador revisa antes de enviar)"* y *"configurable por conversación individual"*. **Que la sugerencia sea un borrador que se copia a la caja de texto, y no un mensaje que se aprueba, es decisión del equipo**: el anteproyecto no describe el gesto |
| **Enmienda** | [ADR-0041](0041-temporizador-del-turno-por-poller.md), **regla 8** — abre bloque también el modo `suggestion`, no sólo el autónomo · [ADR-0024](0024-unidad-y-alcance-de-oe6.md), **§2 (último punto), §3, §4 y §5** — el alcance de OE6 sigue siendo el modo autónomo, pero deja de sostenerse por la ausencia del bloque y pasa a sostenerse por un filtro (§2.4); la alternativa *"abrir el bloque igual en modo `suggestion`"*, descartada el 2026-09-20, queda adoptada con otro argumento · [ADR-0035](0035-reglas-de-la-bandeja-manual.md), **regla 11** — `suggestion` deja de rechazarse con 400 `mode_not_available` · [ADR-0042](0042-pipeline-de-decision-del-turno.md), **§2.4** — el texto fijo de un corte no se envía en modo `suggestion` (§2.3) · [07-secuencia-del-turno.md](../07-secuencia-del-turno.md), **§3** — no hay botón de rechazo, el mensaje lo manda el Operator con `sender_type = 'operator'`, y la sugerencia ignorada queda en `pending_review` · [05-consultas-de-metricas.sql](../05-consultas-de-metricas.sql), **supuesto S4 de OE7** — una sugerencia aceptada ya no produce un mensaje con `sender_type = 'ai_agent'` |
| **Relacionada con** | [ADR-0014](0014-medicion-de-oe3-y-oe4.md), [ADR-0035](0035-reglas-de-la-bandeja-manual.md), [ADR-0037](0037-toda-conversacion-nace-con-vendedor.md), [ADR-0038](0038-alta-baja-y-pausa-del-agente-ia.md), [ADR-0042](0042-pipeline-de-decision-del-turno.md), [ADR-0045](0045-senales-de-transferencia-y-desactivacion-del-agente-ia.md), [07-secuencia-del-turno.md](../07-secuencia-del-turno.md) |

---

## 1. Contexto

La Fase 9 ramifica el orquestador en los tres modos de F02, y el modo `suggestion` choca con una regla escrita: **la regla 8 de [ADR-0041](0041-temporizador-del-turno-por-poller.md) no abre bloque en ese modo.** Sin bloque no hay turno, así que el sistema no tiene de qué generar una sugerencia. Tampoco hay dónde escribir las señales: `signal_evaluations.message_batch_id` es `NOT NULL`, y [07-secuencia-del-turno.md](../07-secuencia-del-turno.md) §3 promete que en modo sugerencia *"la evaluación no se pierde: `signal_evaluations` la conserva igual"*.

La regla 8 no fue un descuido. El equipo decidió el 2026-09-20 que así quedaba, porque [ADR-0024](0024-unidad-y-alcance-de-oe6.md) mide OE6 por bloque y un bloque en modo sugerencia lo contesta un Operator. Se descartó abrir el bloque sabiendo que era la única salida que había que elegir antes de tener datos. **Esa decisión se tomó cuando el modo sugerencia todavía no existía**, y sin bloque el modo no puede funcionar.

Además, `07-secuencia` §3 describe un ciclo de revisión con tres botones —aceptar, editar, rechazar— y el equipo describe otro gesto: **el Operator ve la sugerencia, la copia a su caja de texto con un botón, y la manda él**, tal cual o cambiada. O la ignora y escribe otra cosa.

## 2. Decisión

### 2.1 El modo sugerencia arma el turno igual que el autónomo

**Abre bloque, lo cierra el poller y llama al cerebro con `mode = suggestion`**, igual que el autónomo. La regla 8 de ADR-0041 pasa a decir *"modo autónomo o sugerencia"*; el resto de la regla —las tres condiciones que se vuelven a leer al cerrar— se sostiene.

### 2.2 El pipeline corre entero y la generación queda en `pending_review`

El cerebro corre los mismos pasos, escribe las mismas señales y guarda la generación con `status = 'pending_review'` y `message_id` NULL. **Nada sale solo hacia la clienta.**

### 2.3 Un corte en modo sugerencia no le manda nada a la clienta

El texto fijo de O2 ([ADR-0042](0042-pipeline-de-decision-del-turno.md)) **no se envía**: F02 dice que el Operator revisa antes de enviar, y un texto automático no pasa por nadie. El resto del corte se escribe igual —pasos, señales y generación `blocked` si fue a la salida—, y la conversación queda como la deja cualquier corte.

### 2.4 OE6 los deja afuera por un filtro

**El `detail_json` del `triage` guarda el modo del turno** (`"mode": "suggestion"`), y la consulta de OE6, que ya lee esa fila para el denominador, excluye esos bloques. **El alcance de OE6 no cambia**: son las conversaciones autónomas. Lo que cambia es cómo se sostiene: antes, por ausencia del bloque; ahora, por un filtro sobre la misma fila que decide el denominador.

### 2.5 Una sugerencia se usa copiándola, y la manda el Operator

- **El Operator ve la sugerencia pendiente de su conversación** en una lectura del núcleo. En tiempo real llega con la bandeja de la Fase 10 (F21).
- **El botón copia el texto a la caja**, y lo que el Operator manda sale por el mismo envío que cualquier mensaje suyo ([ADR-0035](0035-reglas-de-la-bandeja-manual.md)), con **`sender_type = 'operator'`** y un `generation_id` opcional que dice de qué sugerencia salió.
- **Con `generation_id`, en la misma transacción que el mensaje**, el núcleo escribe en esa generación `message_id`, `reviewed_by_operator_id`, `reviewed_at` y el `status`: **`accepted` si el texto enviado es idéntico a `completion_text`**, `edited` si no. La generación tiene que ser de esa conversación y estar en `pending_review`.
- **No hay botón de rechazo.**

### 2.6 Una sugerencia ignorada queda en `pending_review`

No se marca `rejected`, porque nadie la rechazó. Cada bloque nuevo genera su propia sugerencia y la anterior queda como estaba.

### 2.7 En modo sugerencia no se transfiere

Las señales se evalúan y se escriben, pero **el núcleo no escribe `transfer_events` ni apaga nada**: la conversación ya la atiende una persona (H-30, `07-secuencia` §3). Con §2.1 esto pasa a ser una regla del orquestador y no algo que se cumple porque el turno no existe, y **tiene un test que lo prueba**.

## 3. Justificación

**Por qué abrir el bloque.** Sin bloque el modo no existe: no hay turno del cual sugerir, y las señales que `07-secuencia` §3 prometía conservar no tienen fila a la cual colgarse. El argumento del 2026-09-20 era que el bloque metería en OE6 turnos que contesta un Operator, y eso sigue siendo cierto: por eso OE6 los filtra (§2.4). **Lo que se descartó entonces era abrir el bloque sólo para medir**; lo que se decide acá es abrirlo porque el modo lo necesita para funcionar.

**Por qué el filtro va en el `detail_json` del `triage`.** Es la fila que ya define el denominador de OE6, así que el filtro no agrega un join. Es el mismo mecanismo con que [ADR-0042](0042-pipeline-de-decision-del-turno.md) O1b saca los saludos (`detail_json.greeting`), y tampoco toca el esquema.

**Por qué `sender_type = 'operator'`.** Lo manda un Operator, que pudo haberlo cambiado. Atribuirlo al AIAgent pondría en la boca del modelo un texto que quizás no escribió, que es el mismo argumento con que ADR-0042 no le atribuyó el texto fijo. El origen no se pierde: `generations.message_id` apunta al mensaje, y `completion_text` guarda lo que el AIAgent propuso.

**Por qué comparar el texto exacto.** Es la única regla que no necesita un umbral de parecido, y la tasa de edición de F02 sale de comparar `messages.content_text` con `completion_text` cuando el `status` es `edited`, como ya decía `07-secuencia` §3.

**Por qué no se transfiere.** `transfer_events` registra la acción de pasar la conversación a una persona, y en modo sugerencia ya está en manos de una. Es la decisión 5 del Paso 8, que se sostiene.

## 4. Consecuencias

### Positivas

- **El modo sugerencia existe**, con el mismo pipeline que el autónomo: la sugerencia pasó por el mismo filtro que una respuesta enviada.
- **Las señales de F12 se evalúan también en estas conversaciones**, así que OE4 tiene más datos y `07-secuencia` §3 dice algo cierto.
- **Los dos modos pasan a tener la misma unidad** desde hoy. Comparar cuánto se usa una sugerencia contra cuánto resuelve el AIAgent solo deja de ser imposible.
- **OE7 no se infla**: toda sugerencia usada deja un mensaje de Operator, que el supuesto S1 ya descalifica.

### Negativas / costos asumidos

- **Se paga una generación por bloque aunque nadie la use.** Una sugerencia ignorada cuesta lo mismo que una respuesta enviada. Es un criterio de costo del equipo, no una métrica del anteproyecto.
- **`rejected` queda inalcanzable.** El valor sigue en `GenerationStatus` y en el `CHECK` de `0001`, y nada lo escribe. Quien lea la enumeración va a suponer que existe un rechazo.
- **`pending_review` deja de querer decir "alguien la va a mirar".** Una consulta que cuente sugerencias ignoradas tiene que buscar las `pending_review` con un mensaje posterior en la conversación, y ninguna consulta de `05-consultas-de-metricas.sql` lo hace hoy.
- **El vínculo depende del frontend.** Si la caja de texto no manda el `generation_id`, el mensaje sale como uno cualquiera del Operator y la sugerencia queda `pending_review`. La tasa de edición se pierde sin error.
- **Un espacio de más cuenta como edición.** La comparación exacta no distingue un retoque de una reescritura.
- **La exclusión de OE6 vive en la consulta y en un `jsonb`.** Una medición que olvide el filtro mete turnos de Operator en OE6, y nada la frena. Es el mismo costo que ADR-0042 asumió con los saludos.
- **Los bloques cortados en modo sugerencia antes del `triage` no tienen `detail_json.mode`.** No afectan a OE6, que no los cuenta, pero una consulta por modo sobre `message_batches` no los distingue.
- **La tasa de transferencia de F36 no cuenta estas conversaciones**, aunque una señal haya disparado. El conteo sin atribuir sigue en `signal_evaluations`.
- **Los dos modos siguen sin compararse hacia atrás**: los bloques de antes de esta fase no existen.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Sostener la regla 8 y generar la sugerencia fuera del bloque** | La generación no tendría turno, `signal_evaluations` no tendría dónde escribirse y el contexto dinámico de F04, que se reescribe por bloque, no avanzaría |
| **El ciclo de tres botones de `07-secuencia` §3** | No es el gesto que describe el equipo: el Operator no aprueba un mensaje, usa un borrador |
| **`sender_type = 'ai_agent'` para la sugerencia aceptada** | Le atribuye al AIAgent un texto que un Operator pudo cambiar, y obliga a OE7 a un supuesto aparte para no inflarse |
| **Marcar `rejected` la sugerencia ignorada** | Afirma una acción que nadie hizo, en la tabla de la que sale la tasa de edición |
| **Una columna `mode` en `message_batches`** | Sería el dato en su lugar natural, pero es DDL para un filtro que la fila del `triage` ya puede llevar |
| **Enviar el texto fijo de un corte también en modo sugerencia** | Contradice F02: sale algo hacia la clienta sin que un Operator lo revise |
