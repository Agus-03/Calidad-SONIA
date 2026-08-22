# ADR-0014 — Cómo se miden OE3 y OE4

| Campo | Valor |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 2026-08-10 |
| **Etapa** | Diseño — Paso 6 (validación cruzada de las métricas) |
| **Resuelve** | **H-18** — la salida del clasificador de frustración no se persiste |
| **Requisitos afectados** | OE3, OE4 · F12, F36 |

---

## 1. Contexto

El Paso 6 consiste en escribir la consulta SQL de cada métrica del anteproyecto contra el modelo de papel. Al intentarlo con OE3 y OE4 aparecieron tres problemas encadenados, y por eso las tres decisiones van en un solo ADR: **cada una determina si la siguiente hace falta.**

**Primero: el anteproyecto nunca define "precisión".** OE3 pide *"una precisión de clasificación de transferencia ≥ 85 %"*; OE4, *"detectar señales de frustración y cambios de intención con precisión ≥ 85 %"*; la sección 8.1 la repite entre las mediciones cuantitativas. En castellano técnico la palabra admite dos lecturas —*precision* estricta y *exactitud*— que producen números distintos sobre el mismo clasificador.

**Segundo: el anteproyecto tampoco define la unidad.** *"De las transferencias que hice"* puede contarse por conversación o por evento de transferencia, y una conversación puede tener más de uno.

**Tercero, y es el hallazgo:** `transfer_events` sólo tiene fila **cuando la transferencia se dispara**. Para el clasificador de transferencia eso alcanza, porque su salida positiva *es* la acción. Para el detector de frustración no: su salida es una percepción que sólo deja rastro si además cruzó el umbral y ganó frente a las otras señales. El conjunto que OE4 quiere medir —las detecciones— es un superconjunto del que la base guarda.

## 2. Decisión

**(a) "Precisión" significa *precision* estricta:** `VP / (VP + FP)`. El número declarado de OE3 y OE4 responde *"de las detecciones que hice, ¿cuántas estuvieron bien?"*.

**(b) La unidad de medida es la conversación**, no el evento. El denominador de OE3 son las conversaciones con al menos una transferencia; el gold se lee de `conversation_annotations`, que está a nivel de conversación.

**(c) Se incorpora `signal_evaluations`** (Bloque 7 del catálogo): una fila por cada señal de F12 evaluada en cada bloque procesado, **dispare o no**, con `score`, `threshold_used`, `classifier_version` y `fired`. `transfer_events` pierde `trigger_type`, `confidence`, `threshold_used` y `classifier_version` —que se mudan ahí— y gana `signal_evaluation_id`.

**(d) `conversation_annotations.gold_value` recibe dominio declarado por `annotation_type`**, reutilizando `TransferSignalType` para el caso de transferencia. Sin eso, comparar el gold contra la predicción es una convención tácita.

**(e) El recall se reporta al lado, como evidencia de respaldo y no como objetivo.** No modifica la meta que fija el anteproyecto.

## 3. Justificación

**Por qué precision y no exactitud.** La exactitud es la única de las tres lecturas que un clasificador nulo puede aprobar. Con una tasa base de frustración del orden del 12 % —y no se conocerá la real hasta terminar de etiquetar—, un detector que **no detecta nada nunca** obtiene:

```
VP = 0 · FP = 0 · FN = 60 · VN = 440   (sobre las 500 conversaciones de OE4)
Exactitud = 440 / 500 = 88 %   ← supera el ≥ 85 % del objetivo
Recall    = 0 %
```

Es la primera pregunta que haría un tribunal que conozca el tema. Y hay un problema anterior: **hoy no se puede saber si "85 %" es exigente o trivial**, porque eso depende del desbalance que se descubra al anotar. Precision y recall son robustos a ese desbalance; la exactitud no.

**Por qué la conversación como unidad.** OE3 declara textualmente *"un conjunto de prueba de 200 conversaciones"*. La unidad de la muestra la fija el anteproyecto, y medir sobre otra sería medir sobre algo que el objetivo no declara. Medir por evento exigiría además gold por turno, que `conversation_annotations` no tiene.

**Por qué persistir la evaluación de cada señal.** Sin ella, la única forma de medir la frustración sería sobre `transfer_events` — un conjunto **filtrado justamente por el umbral que se quiere evaluar**. Ese número *sube al subir el umbral*, sin que el clasificador haya mejorado: es regulable con una perilla, y por lo tanto indefendible.

El razonamiento es además consistente con una decisión ya tomada: `message_batches` persiste `timer_ms_used` y `closed_reason` porque *"sin persistir el bloque, F10 es una afirmación sin evidencia"*. Acá es lo mismo, aplicado al otro clasificador del Módulo 2. Formulado como principio general:

> **Todo clasificador cuya precisión es un objetivo específico tiene que persistir su salida aunque esa salida no dispare ninguna acción.** El de transferencia lo cumple por accidente, porque su salida *es* una acción; el de frustración no lo cumplía.

**Por qué los atributos se mudan y no se copian.** `transfer_events` con su propio `confidence` y `threshold_used` sería el mismo dato en dos lugares. Es la separación que el modelo ya usa entre `generations` (la acción) y `generation_components` (con qué se armó): el evento apunta a la evidencia.

## 4. Consecuencias

### Positivas

- **OE4 pasa a ser medible.** Era el único objetivo que ninguna consulta podía responder ni siquiera de forma aproximada.
- **Barrido de umbral.** Con `score` y `threshold_used` de cada evaluación, mover el umbral y recalcular es una consulta. Sin la tabla, cada ajuste obliga a **volver a correr las 200 y las 500 conversaciones enteras** — y entre las fechas de OE3 (28/08/2026) y OE4 (07/09/2026) hay diez días, así que el umbral se va a mover.
- **F36 gana sustancia.** La *"tasa de transferencia por tipo de señal"* deja de contar disparos y pasa a saber cuáles estuvieron bien atribuidos.
- **OE6 queda encaminado.** `out_of_domain` es una de las cuatro señales: su evidencia ya queda registrada.
- **Trazabilidad operativa.** *"¿Por qué el bot no me derivó con una persona?"* tiene una fila que lo responde.

### Negativas / costos asumidos

- **El falso negativo queda sin métrica que lo vigile.** Medir sólo precision deja fuera al cliente frustrado que el bot no suelta — que es, textualmente, el tercero de los seis problemas de la sección 5.2. La mitigación es el punto (e): reportar el recall al lado.
- **Restringe la implementación.** El clasificador debe **evaluar las cuatro señales en cada bloque, aunque una ya haya disparado**. Cortar al primer disparo es lo eficiente y reabre el hueco. Si en algún momento se decide cortocircuitar, hay que registrar explícitamente cuáles se saltearon. Es una restricción que nace del diseño y que dentro de seis meses nadie va a recordar si no está escrita.
- **Una entidad más** (36 en el modelo) y **cuatro filas por turno**. Con ~50 conversaciones diarias y ~6 turnos son unas 440 mil filas al año: irrelevante para PostgreSQL, pero la política de retención va al Paso 7.
- **No se mide *cuándo* se transfirió.** Con la unidad en la conversación, transferir en el turno 3 algo que correspondía en el turno 8 cuenta como acierto. Es aceptable porque el momento óptimo de transferencia no es un objetivo declarado en ningún lado del anteproyecto.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Interpretar "precisión" como exactitud** | Un clasificador que no hace nada cumple OE4 por desbalance de clases. Además obligaría igual a persistir los negativos, porque la exactitud necesita verdaderos negativos explícitos y la ausencia de fila confunde *"decidió que no"* con *"nunca corrió"*. |
| **Redefinir OE4 como "precisión de las transferencias atribuidas a frustración"** | Medible hoy, sin cambios. Pero mide la atribución de la transferencia, no el detector, y el número se regula subiendo el umbral. Redefinir un objetivo para hacerlo medible es legítimo; redefinirlo hacia un número autocomplaciente, no. |
| **Dos columnas en `message_batches` (`frustration_score`, `classifier_version`)** | Lo más barato. Se descartó porque F12 declara **cuatro** señales y habría que repetir columnas por cada una; porque mezcla la responsabilidad del agregador (F10/F11) con la del clasificador (F12); y porque pierde la simetría con `transfer_policies`, que ya tiene una fila por señal por agente. |
| **Medir OE3 y OE4 fuera de la base, exportando a una planilla** | Es exactamente el hallazgo **H-10** de la matriz de trazabilidad: una métrica etiquetada en una planilla suelta no es reproducible ante el tribunal. |
| **Unidad = evento de transferencia** | Requiere gold por turno, que `conversation_annotations` no tiene, y mide sobre una unidad que OE3 no declara — el anteproyecto habla de 200 *conversaciones*. |

## 6. Pendientes que abre

- [ ] **Regla de atribución entre señales simultáneas.** Si dos superan el umbral en el mismo turno hay una sola transferencia y `signal_evaluation_id` apunta a una sola fila. F12 no dice cuál gana. Candidatos: mayor `score / threshold_used`, u orden de prioridad fijo entre las cuatro.
- [ ] **Definición de "cambio de intención"** (la otra mitad de OE4). Los datos existen —`detected_intents` permite derivar el cambio comparando bloques consecutivos—, pero bajo la lectura ingenua *toda* conversación tiene cambios de intención y la métrica se degrada a la tasa base. Hay que definir qué cuenta como cambio **disruptivo**. Se resuelve en el Paso 11 (protocolo de evaluación), no en el modelo.
