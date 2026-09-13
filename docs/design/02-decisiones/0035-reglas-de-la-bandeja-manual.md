# ADR-0035 — Reglas de la bandeja manual: quién ve, quién responde y quién apaga el bot

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · revisada el 2026-09-13, antes del merge, tras la revisión de diseño y de código: reglas 1, 6, 7, 8 y 9 |
| **Fecha** | 2026-09-13 |
| **Etapa** | Construcción — Fase 5 (bandeja manual mínima) |
| **Resuelve** | Qué conversaciones alcanza cada rol en la bandeja, cuáles puede responder, quién cambia el modo del agente IA y cuándo se decide la reasignación propia del `operator` |
| **Requisitos afectados** | F02, F22, F26, F27, F35, F36 · Anexo B |
| **Enmienda** | [ADR-0032](0032-acceso-a-contactos-y-datos-sensibles.md) §4, *Lo que queda abierto* — el plazo de la reasignación propia pasa de la Fase 5 a la Fase 11 |
| **Relacionada con** | [ADR-0002](0002-ciclo-de-vida-conversacion-y-recordatorios.md), [ADR-0004](0004-asignacion-de-conversaciones.md), [ADR-0008](0008-ventana-de-24-horas.md), [ADR-0020](0020-idempotencia-de-los-mensajes-salientes.md), [ADR-0025](0025-atribucion-por-operador.md), [ADR-0033](0033-secretos-de-bandejas-nullables-y-congelamiento-de-0001.md), [ADR-0036](0036-paginacion-por-cursor-en-la-bandeja.md) |

---

## 1. Contexto

La Fase 5 construye la bandeja manual: el listado de conversaciones, el hilo de mensajes, la respuesta de un operador y el interruptor del bot. Son cuatro rutas, y cada una necesita decir **qué rol entra y a qué conversaciones llega**. Hasta ahora esa pregunta estaba respondida para los contactos ([ADR-0032](0032-acceso-a-contactos-y-datos-sensibles.md)), no para las conversaciones.

Las fuentes dicen esto:

| Fuente | Qué dice |
|---|---|
| **Anexo B — User (operador básico)** | *"Responde conversaciones asignadas. Activa o desactiva el agente IA en un chat específico. No puede reasignar ni ver conversaciones de otros operadores."* |
| **Anexo B — Super Agent** | *"Todo lo de User. Además: puede reasignar conversaciones entre operadores y visualizar conversaciones de todo el equipo."* |
| **F27** | *"El operador cambia el modo del agente (autónomo/sugerencia/apagado) en un chat específico sin afectar los demás."* |
| **F02** | Tres modos, *"Configurable por conversación individual"*. No nombra un rol |
| **[ADR-0004](0004-asignacion-de-conversaciones.md)** | La conversación entra al pool y *"el primer operador que responde queda autoasignado"* |

Quedan tres huecos que ninguna fuente cierra:

- **El pool.** El Anexo B prohíbe ver conversaciones *"de otros operadores"*, y ADR-0004 exige que alguien vea el pool para poder responderlo. Una conversación del pool no es de nadie. **Responderla también excede la letra**: el Anexo B le da al operador básico *"Responde conversaciones asignadas"*, y una del pool todavía no lo está. Esa parte ya la resolvió ADR-0004, al hacer que responder sea lo que asigna.
- **Qué hace un `supervisor` con lo que ve.** El Anexo B le da *visualizar* todo el equipo. No dice si también puede *responder* lo que no es suyo.
- **La reasignación propia.** ADR-0032 dejó abierto si el `operator` puede reasignar sus propias conversaciones, y lo citó para *"al empezar la Fase 5, con su propio ADR"*. Este ADR la difiere a la Fase 11 (regla 16).

Hay además una decisión del equipo que **se aparta del texto**: quién cambia el modo del agente IA.

## 2. Decisión

### 2.1. Lectura

| # | Regla |
|---|---|
| 1 | **El `operator` ve sus conversaciones asignadas y las del pool** (`conversations.operator_id IS NULL`). `supervisor` y `admin` ven todas las de la cuenta. **Ningún rol ve las de un contacto dado de baja**: salen de la bandeja con él, y siguen en la base como historial (C09) |
| 2 | **El hilo de una conversación tiene el mismo alcance que la bandeja.** Lo que el `operator` no ve en el listado tampoco lo lee por el identificador |
| 3 | **Una conversación fuera de alcance se responde como inexistente** (404 `conversation_not_found`), no como prohibida |
| 4 | **La bandeja muestra del contacto el nombre, el apellido y el teléfono.** Los cuatro datos sensibles no viajan en ninguna ruta de la bandeja, para ningún rol |

### 2.2. Respuesta

| # | Regla |
|---|---|
| 5 | **Cualquier rol responde una conversación suya o del pool. Ninguno responde una asignada a otro**, tampoco `supervisor` ni `admin` |
| 6 | **Una conversación que ya estaba asignada a otro cuando se la leyó se rechaza según lo que el rol ve.** `supervisor` y `admin` reciben 409 `conversation_taken`, porque ven de quién es. El `operator` recibe 404, por la regla 3 |
| 7 | **Responder una conversación del pool la asigna a quien responde, en la misma transacción que el mensaje** (ADR-0004). La asignación queda en `conversation_assignments` con `reason = auto_assigned_on_first_reply` y quien asigna igual a quien recibe. **Asigna el primer envío, no la primera entrega**: si Meta rechaza el mensaje, la conversación igual quedó tomada. La *primera respuesta* (`first_response_at`) es otra cosa: la primera que Meta aceptó, porque mide lo que le llegó al cliente |
| 8 | **Si dos responden a la vez, gana el primero que escribe.** El segundo recibe 409 `conversation_taken`, sea cual sea su rol —la vio en el pool—, y no se escribe ni se envía nada suyo |
| 9 | **Antes de escribir se rechazan cuatro casos**: el texto vacío o ausente (400 `invalid_text`), la conversación resuelta (409 `conversation_resolved`), la ventana de 24 horas cerrada (409 `window_expired`, [ADR-0008](0008-ventana-de-24-horas.md)) y el número de WhatsApp que no existe o desde el que no se puede enviar (409 `channel_unavailable`) |

### 2.3. Modo del agente IA

| # | Regla |
|---|---|
| 10 | **El modo lo cambian `supervisor` y `admin`, sobre cualquier conversación de la cuenta. El `operator` no.** Se aparta de F27 y del Anexo B: ver §3.1 |
| 11 | **Hasta la Fase 9 sólo existen `autonomous` y `off`.** `suggestion` se rechaza (400 `mode_not_available`), porque el ciclo de revisión que lo hace un modo todavía no existe y el orquestador lo leería como silencio |
| 12 | **Prender el bot exige que la conversación tenga agente IA** (409 `no_ai_agent`). **Apagarlo no lo desvincula**: `ai_agent_id` se conserva, para poder volver a prenderlo |
| 13 | **Cambiar el modo no asigna.** Apagar el bot no saca la conversación del pool: asigna responder (regla 7), y apagar el bot no es responder |
| 14 | **Una conversación resuelta no cambia de modo** (409 `conversation_resolved`) |

### 2.4. Reasignación

| # | Regla |
|---|---|
| 15 | **En la Fase 5 nadie reasigna.** La única asignación que existe es la autoasignación de la regla 7 |
| 16 | **Si el `operator` puede reasignar sus propias conversaciones se decide en la Fase 11**, junto con la reasignación de F26, y no ahora. Hasta entonces rige el Anexo B literal: *"No puede reasignar"* |

## 3. Justificación

### 3.1. La desviación de F27 y del Anexo B

**El equipo decidió que el modo lo cambien los roles que están por encima del vendedor.** Es una decisión contra el texto de dos fuentes, no un hueco que se completa:

- **F27** dice *"El operador cambia el modo del agente"*.
- **El Anexo B** le da al operador básico *"Activa o desactiva el agente IA en un chat específico"*.
- **La tabla de C01** en [`00-convenciones.md`](../00-convenciones.md) reproduce esa capacidad al traducir los roles, y su punto abierto 1 afirma que *"las capacidades no se modifican"*. Con esta decisión, esa frase deja de ser cierta para el `operator`.

Los argumentos que la sostienen son de gobierno del sistema, no de requisitos:

- **El modo es una variable de medición, no sólo de atención.** F36 mide la tasa de resolución autónoma y [ADR-0024](0024-unidad-y-alcance-de-oe6.md) define qué turnos entran al denominador de OE6. Cada conversación que alguien apaga sale de esa población. Si el interruptor lo tiene cada vendedor, la población medida depende de criterios individuales que nadie registra.
- **Con [ADR-0025](0025-atribucion-por-operador.md), apagar el bot es también una vía para quedarse con la venta.** La atribución va al operador que más salientes envió. Un vendedor que apaga el bot en las conversaciones prometedoras y responde él se acredita conversiones que la IA quizás habría cerrado. Poner el interruptor un nivel más arriba lo convierte en una decisión de quien no compite por esa atribución.

**Ninguno de los dos argumentos está en el anteproyecto.** Es la misma clase de restricción que [ADR-0011](0011-acceso-a-datos-sensibles.md) le puso a los contactos y que [ADR-0032](0032-acceso-a-contactos-y-datos-sensibles.md) revirtió por falta de fuente. La diferencia que hay que poder defender es que acá el equipo **la toma a sabiendas y la declara**, en vez de presentarla como lectura del texto. Por eso se escribe como desviación y va al informe final (§4).

### 3.2. El resto de las reglas

- **La regla 1 es la única lectura del Anexo B compatible con ADR-0004.** Si el pool no fuera visible para el `operator`, sólo `supervisor` y `admin` podrían tomar una conversación nueva, y ADR-0004 —*"el primer operador que responde"*— no tendría operador. Una conversación del pool no es *"de otros operadores"*: no es de nadie.
- **Lo último de la regla 1 cierra un hueco que la baja de contactos abría.** Un `admin` puede dar de baja un contacto (ADR-0032, regla 12), y su conversación seguía en la bandeja y se podía responder: se le escribía a alguien que el equipo había sacado de la agenda. El historial no se toca, porque es el registro sobre el que se miden F34 a F36.
- **La regla 3 se decide acá por primera vez.** ADR-0032 no dice nada sobre 404 o 403; el criterio sólo estaba en el código de `contacts/`, que ya responde así. Un 403 le confirmaría al vendedor que la conversación existe y que es de otro, que es justo lo que el Anexo B le oculta.
- **La regla 4 separa la bandeja de la agenda, y con eso quedan dos alcances distintos.** ADR-0032 limita la agenda del `operator` a los contactos de sus conversaciones asignadas; la bandeja le suma el pool. Hace falta, porque para tomar una conversación hay que saber quién escribió, pero es un segundo alcance, no el mismo derivado dos veces (§4). Lo que ADR-0032 protege de verdad —los cuatro datos cifrados (regla 4 de ese ADR)— no viaja en la bandeja.
- **La regla 5 lee *"visualizar"* como visualizar.** El Anexo B le suma al `supervisor` dos capacidades —reasignar y ver todo—, y responder lo ajeno no es ninguna de las dos. Si respondiera sin reasignar, habría dos personas escribiéndole al mismo cliente, y `messages.sender_operator_id` (ADR-0025) le acreditaría trabajo en una conversación que `operator_id` dice que es de otro. El camino es reasignar primero (F26) y responder después.
- **La regla 6 le da a cada uno el error que puede entender.** Al `supervisor`, un 404 sobre una conversación que tiene en pantalla sería falso. Al `operator`, un 409 le revelaría que la conversación existe.
- **La regla 7 hace atómicas la asignación y la respuesta.** Si la asignación y el mensaje se escribieran por separado, un corte entre los dos dejaría un mensaje de un operador en una conversación del pool, o una conversación tomada sin respuesta. **Que asigne el envío y no la entrega es forzado por [ADR-0020](0020-idempotencia-de-los-mensajes-salientes.md)**: la fila se escribe antes de salir, y la entrega pasa fuera de la transacción porque ninguna transacción cruza la frontera con otro servicio.
- **La regla 7 separa dos "primeras" porque sirven para cosas distintas.** Para decidir quién atiende alcanza con quién escribió primero, llegue o no: es el que tomó la decisión de atender. Para medir el tiempo de respuesta de F34 cuenta lo que le llegó al cliente, y un envío fallido no le llegó.
- **La regla 8 cae de la regla 7.** La asignación es un `UPDATE … WHERE operator_id IS NULL`, y sólo uno de los dos lo gana. El que pierde no escribió nada, así que no hay mensaje que reconciliar. Recibe 409 aunque sea `operator`, porque la conversación estaba en su bandeja cuando la leyó: decirle que no existe sería falso.
- **La regla 9 rechaza antes de escribir porque ADR-0020 no deja arreglar después.** Un saliente fuera de la ventana es una fila que Meta iba a rechazar siempre: quedaría `failed` para siempre por una falla que el operador no causó. Lo mismo vale para un número que no existe o que nadie conectó ([ADR-0033](0033-secretos-de-bandejas-nullables-y-congelamiento-de-0001.md)), que además le dejaría la conversación tomada por un intento que nunca tuvo chance. La conversación resuelta se rechaza porque [ADR-0002](0002-ciclo-de-vida-conversacion-y-recordatorios.md) la cierra como unidad de trabajo, y lo que abre la siguiente es un mensaje del cliente, no uno del operador.
- **Si se puede enviar desde un número lo dice quien envía, no la bandeja.** El cliente real exige token; el doble de desarrollo no, y por eso la validación es una pregunta al puerto de envío (`Ready`) y no una regla sobre la fila.
- **Las reglas 11 y 12 evitan dos estados que el orquestador no sabría leer**: `suggestion` sin ciclo de revisión, y `autonomous` sin agente IA.
- **La regla 13 mantiene una sola causa para la asignación.** Si apagar el bot asignara, `conversation_assignments` tendría dos motivos automáticos y F26 heredaría el trabajo de distinguirlos.
- **La regla 16 no decide en el vacío.** En la Fase 5 no existe ninguna reasignación, para ningún rol. Decidir ahora si el `operator` puede usar un mecanismo que no existe sería fijar la forma de F26 antes de construirlo. ADR-0032 la había citado para esta fase porque *"cambia quién ve qué por vía de la regla 2"*, y eso sigue siendo cierto, pero sólo empieza a ocurrir cuando haya una forma de mover una conversación. Esa forma llega en la Fase 11.

## 4. Consecuencias

### Positivas

- Cada una de las cuatro rutas tiene escrito quién entra y a qué llega, y el código lo cita en vez de decidirlo.
- La bandeja y el hilo no pueden discrepar: comparten la regla de alcance (reglas 1 y 2).
- La carrera por el pool no deja rastros: el que pierde no escribe nada (regla 8).
- Ninguna conversación tiene dos voces humanas: sólo su dueño le responde (regla 5).
- `conversation_assignments` empieza a llenarse en esta fase, así que F26 va a tener historial desde el primer día y no desde la Fase 11.

### Negativas / costos asumidos

- **Un vendedor que toma una conversación autónoma no puede callar al bot.** Mientras un `supervisor` o `admin` no lo apague, **el cliente recibe dos voces**: la del vendedor y la de la IA, que sigue contestando cada mensaje. Es el problema que el interruptor venía a resolver, y para el rol que más responde queda resuelto a medias. El equipo necesita un procedimiento —el vendedor avisa y el supervisor apaga— que el sistema no sostiene ni registra.
- **La decisión contradice un requisito, no sólo un anexo.** F27 está en el Anexo A y dice *"el operador"*. **Hay que declararlo en el informe final**, igual que ADR-0032 declara que revirtió a ADR-0011. La tabla de C01 en `00-convenciones.md`, que le atribuía al `operator` una capacidad que el sistema no le da, quedó marcada como enmendada el 2026-09-13.
- **Hasta la Fase 11, una conversación tomada por alguien ausente queda trabada.** El `supervisor` la ve y no puede responderla (regla 5) ni reasignarla (regla 15). La única salida es SQL a mano. Con un equipo chico y la bandeja en construcción el riesgo es acotado, pero no es cero.
- **Un envío fallido igual toma la conversación** (regla 7). Los rechazos que se saben de antemano ya no llegan a escribirse (regla 9), pero el vendedor al que Meta le rechaza el mensaje se queda con una conversación que el cliente nunca recibió como atendida. El fallido queda visible en la bandeja (ADR-0020), y la conversación, a su nombre.
- **Un token revocado entre la validación y el envío sigue fallando como antes** (regla 9). Ninguna comprobación previa puede ver lo que cambia después de hacerla: ese caso deja la fila `failed` y la conversación tomada.
- **El `operator` lee todo el pool, no sólo el nombre y el teléfono** (reglas 1, 2 y 4). Ve el hilo completo de cada conversación sin asignar, aunque nunca la atienda, incluidos los datos sensibles que el cliente haya escrito en el chat, que no están cifrados ([ADR-0032](0032-acceso-a-contactos-y-datos-sensibles.md) §2.3). Es bastante más de lo que su agenda le muestra, y es el precio de que el pool se pueda tomar.
- **La agenda y la bandeja tienen dos alcances.** La agenda del `operator` son sus asignadas (`conversations/scope.go`); la bandeja, sus asignadas más el pool (`inbox/repository.go`). ADR-0032 contaba como positiva que el control fuera *"una sola consulta… derivada de la regla de la bandeja"*, y eso deja de ser cierto: son dos consultas, y hay que mantenerlas por separado.
- **Una conversación de un contacto dado de baja sigue abierta en la base, oculta** (regla 1). Nadie la ve ni la cierra desde la bandeja. Si el número vuelve a escribir, el webhook crea un contacto nuevo con su propia conversación, y la vieja queda `active` para siempre. **Una métrica de conversaciones activas (F34) la va a contar** si no excluye los contactos dados de baja.
- **El modo se lee al empezar el turno.** Si alguien apaga el bot mientras el cerebro está generando, esa respuesta igual sale. Se cierra en la Fase 9, cuando los tres modos bifurquen el orquestador. Mientras tanto, *"apagado"* significa *"no empieza turnos nuevos"*.
- **ADR-0032 citó la reasignación propia para esta fase y no se resuelve acá.** Es la segunda vez que se mueve de fecha. El plazo nuevo queda en el estado de ADR-0032 y en la regla 16, pero una decisión que se posterga dos veces merece que alguien la mire en la Fase 11 antes de construir F26, no después.

### Lo que queda abierto

| Qué | Por qué no se resuelve acá |
|---|---|
| **Si el `operator` reasigna sus propias conversaciones** | Regla 16. Se decide en la **Fase 11**, con F26 |
| **Qué hace el sistema con el bot cuando un vendedor toma la conversación** | El equipo descartó que tomar la conversación apague el bot (§5). Si el procedimiento manual no alcanza en la operación real, la salida está en esta decisión —devolverle el interruptor al `operator` para las conversaciones suyas— y no en un automatismo |
| **Declarar la desviación de F27 en el informe final** | El ADR la registra y C01 ya apunta acá; el informe es un documento aparte |
| **Qué filtros ofrece la bandeja** | F23 —no leídas, asignadas, en espera, resueltas, por etiqueta, con recordatorio, destacadas— es de la **Fase 10** |

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **El modo lo cambia también el `operator`, sobre sus conversaciones** — lo que dicen F27 y el Anexo B | Es la lectura literal y resuelve las dos voces. El equipo reservó el modo a los roles de arriba; los argumentos de §3.1 son los que sostienen esa elección. Es la salida natural si el procedimiento manual no alcanza |
| **Tomar la conversación apaga el bot** | No hay dos voces y no hace falta interruptor. **No está en ninguna fuente**, deja sin forma de volver a prenderlo y mezcla en una sola acción dos decisiones distintas: quién atiende y si la IA sigue |
| **El `operator` no ve el pool; lo reparte el `supervisor`** | Deja a ADR-0004 sin *"primer operador que responde"*, y convierte al `supervisor` en un despachador que el cooperante no tiene |
| **El `supervisor` y el `admin` responden cualquier conversación** | Dos personas escribiéndole al mismo cliente, y una atribución por `sender_operator_id` que no coincide con `operator_id`. Lo que el `supervisor` necesita para intervenir es reasignar, y eso es F26 |
| **403 en vez de 404 para el `operator`** | Le confirma que la conversación existe y que es de otro. Es lo mismo que `contacts/` ya evita en su código |
| **Mostrar las conversaciones de un contacto dado de baja, como historial** | C09 conserva el historial, pero la bandeja no es el historial: es lo que se atiende. Responderle a alguien que el equipo sacó de la agenda es el error que la baja quería evitar. El historial se va a leer en F32 |
| **Validar el número después de escribir, dejando el fallo visible** | Es lo que hacía la primera versión. Deja una fila `failed` y una conversación tomada por un fallo que se sabía de antemano, justo lo que la regla 9 rechaza para la ventana cerrada |
| **Asignar al confirmar la entrega, no al escribir** | La entrega pasa fuera de la transacción (ADR-0020), así que la asignación quedaría fuera también, y dos vendedores podrían escribirle al mismo cliente del pool antes de que Meta conteste a cualquiera de los dos |
| **Decidir ya la reasignación propia del `operator`** | En la Fase 5 no existe ninguna reasignación. Decidir sobre un mecanismo que no está construido es fijar la forma de F26 desde afuera |
