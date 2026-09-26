# ADR-0044 — La señal de frustración: qué detecta, con qué, y por qué se construye en la Fase 16

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · **el segundo párrafo de §2.1 (la frontera con `no_resolution`) es una propuesta, pendiente del grupo** |
| **Fecha** | 2026-09-26 |
| **Etapa** | Construcción — Fase 9 (contexto dinámico y transferencia). **Se decide acá y se construye en la Fase 16** |
| **Resuelve** | Qué cuenta como `frustration` entre las cuatro señales de F12, con qué instrumento se detecta, y qué escribe el turno mientras ese instrumento no existe |
| **Requisitos afectados** | F12 · F36 · OE3 · OE4 · OE2 y OE7 (sólo para decir que su instrumento no cambia) |
| **Naturaleza** | **Lectura del equipo.** F12 dice *"frustración"* sin definirla, y OE4 pide *"detectar señales de frustración"* sin decir con qué. Ni la definición de §2.1, ni el instrumento de §2.2, ni la elección de *LLM-as-a-judge* para OE7 están en el anteproyecto |
| **Enmienda** | Hasta la Fase 16, dejan de ser ciertas las frases que dicen que se evalúan **cuatro** señales en cada bloque: son tres, y la cuarta queda registrada como salteada (§2.3). Son [07-secuencia-del-turno.md](../07-secuencia-del-turno.md) §1 y el diagrama de §2 · [ADR-0021](0021-atribucion-entre-senales-simultaneas.md) §1 y §2 · [DER/relaciones.md](../DER/relaciones.md), Bloque 7 y la regla de las cuatro señales · [DER/guia-de-dibujo.md](../DER/guia-de-dibujo.md), la nota de las cuatro señales. Además, [08-structure-rules.md](../08-structure-rules.md) **regla 9** gana un caso: un clasificador que no es un prompt (§2.2). **No enmienda [ADR-0014](0014-medicion-de-oe3-y-oe4.md)**: usa la salida que ese ADR dejó escrita para no evaluar una señal, y dice dónde queda el registro |
| **Relacionada con** | [ADR-0014](0014-medicion-de-oe3-y-oe4.md), [ADR-0017](0017-decomposicion-en-despliegues-y-lenguajes.md), [ADR-0021](0021-atribucion-entre-senales-simultaneas.md), [ADR-0023](0023-definicion-de-cambio-de-intencion.md), [ADR-0026](0026-conjuntos-de-evaluacion-simulados.md), [ADR-0028](0028-cobertura-de-pruebas-por-servicio.md), [ADR-0042](0042-pipeline-de-decision-del-turno.md), [07-secuencia-del-turno.md](../07-secuencia-del-turno.md), [08-structure-rules.md](../08-structure-rules.md) |

---

## 1. Contexto

La Fase 9 construye las señales de F12 —`explicit_request`, `frustration`, `out_of_domain`, `no_resolution`— y [ADR-0014](0014-medicion-de-oe3-y-oe4.md) exige evaluarlas **siempre**, aunque una ya haya disparado, porque OE4 mide la precisión del detector de frustración y un detector sólo es medible si su salida se persiste.

Tres cosas estaban sin decidir:

1. **Qué es "frustración".** F12 la nombra y OE4 la mide, pero ninguno dice frustración *con qué*. Un cliente enojado porque el producto llegó roto y un cliente enojado porque el bot no le entiende la pregunta son dos fenómenos distintos, y sólo uno de ellos es un motivo para sacarle la conversación al agente IA.
2. **Con qué se detecta.** La opción inmediata es una llamada más al proveedor de LLM en cada turno. La señal corre en **todos** los turnos, así que su costo y su latencia se pagan sobre el camino feliz.
3. **Qué pasa si el instrumento no llega a la Fase 9.** El equipo quiere entrenar un modelo propio, y ese entrenamiento no cabe en esta fase.

## 2. Decisión

### 2.1 Qué detecta la señal

**`frustration` detecta la frustración del cliente con el agente IA porque el agente no le entiende.** No detecta el enojo del cliente con el negocio, con el producto o con su problema.

**Propuesta, pendiente del grupo: la frontera con `no_resolution`.** F12 describe `no_resolution` como *"turnos sin resolución"* y ningún ADR la define todavía. La lectura que se propone es que `no_resolution` mire **lo que hizo el agente** y `frustration` mire **lo que dice el cliente** sobre eso. El problema es que el síntoma observable de las dos se parece —un cliente que se repite o reformula es también un turno que no avanza—, así que la frontera no se puede fijar hasta que exista la regla con que se detecta `no_resolution`. Se decide al construir esa señal, en la Fase 9, junto con qué `gold_value` de `annotation_type = 'transfer'` se anota cuando aplican las dos.

### 2.2 Con qué se detecta

**Con un modelo de Machine Learning propio, entrenado en un repositorio aparte**, y no con una llamada al LLM.

- **La entrada del modelo es el bloque del cliente y la respuesta anterior del agente IA**, no sólo el bloque. Saber si el agente "no entendió" exige ver qué contestó. Qué entra cuando no hay respuesta anterior del agente —el primer turno, o una conversación donde lo último fue un operador o un texto `system`— se define al construir el modelo.
- **`classifier_version` identifica un artefacto exacto**: el modelo y la versión de los datos con que se entrenó. Sin eso, el número de OE4 no se puede reproducir.
- **`rules/frustration.yaml` guarda la referencia al artefacto, su versión y el umbral por defecto.** No guarda un prompt: es el primer clasificador que no lo tiene. Sigue en `rules/` por el argumento de la regla 9 de [08-structure-rules.md](../08-structure-rules.md): lo que decide se versiona en un dato que entra en un `WHERE`.
- **Las 500 conversaciones de OE4 ([ADR-0026](0026-conjuntos-de-evaluacion-simulados.md)) no se usan para entrenar.** Viven en `test/datasets/` de este repositorio, y el repositorio de entrenamiento no las lee.
- **La consistencia de tono no cambia de instrumento: es *LLM-as-a-judge*.** Para OE2 lo fija el anteproyecto (también F36). **Para OE7 es decisión del equipo**: OE7 dice *"consistencia de tono ≥ 85 %"* sin decir quién juzga, y §5.3 promete *"evaluación humana"*. El ML es sólo para esta señal.

### 2.3 Qué escribe el turno hasta la Fase 16

**La señal no se evalúa, no se escribe ninguna fila de `signal_evaluations` para `frustration`, y el turno la registra como salteada en el `detail_json` de la fila del `triage`**, que es el paso que corre en todos los turnos. Si el turno se corta antes del `triage`, la anota la fila del paso que cortó, como ya hace la regla 4 de [ADR-0042](0042-pipeline-de-decision-del-turno.md) §2.4 con las señales de un turno cortado.

No se escribe una fila con valores inventados por el mismo motivo que da esa regla: `score`, `threshold_used` y `classifier_version` son `NOT NULL`, y una señal que nadie evaluó no tiene ninguno de los tres. Una fila con `score = 0` y `fired = false` diría *"evalué y decidí que no"*, cuando lo cierto es *"nunca evalué"*.

Las otras tres señales se evalúan en todos los turnos, como exige ADR-0014.

## 3. Justificación

**Por qué acotar la definición.** Una transferencia a un humano tiene sentido cuando el humano puede hacer algo que el agente no. Un cliente frustrado porque el agente no le entiende se resuelve con una persona; un cliente enojado con su problema no necesariamente, y transferirlo por eso manda al operador conversaciones que el agente estaba resolviendo bien. La definición amplia además se superpone con el tono general del cliente, que es mucho más difícil de etiquetar con criterio estable.

**Por qué ML y no una llamada al LLM.** La señal corre en cada turno, a diferencia del juez de tono, que corre una vez sobre un conjunto de evaluación. Un modelo propio es más barato por turno, más rápido y determinista para una misma versión. **El ahorro de costo es un criterio del equipo y no una métrica del anteproyecto**: ninguno de los siete objetivos pone un número al costo, igual que dice [ADR-0042](0042-pipeline-de-decision-del-turno.md) §4.

**Por qué un repositorio aparte.** Las dependencias y los datos de entrenamiento no entran al cerebro, y los datos de entrenamiento quedan separados físicamente del conjunto de evaluación de OE4.

**Por qué salteada y no una implementación provisoria.** La regla 6 de [08-structure-rules.md](../08-structure-rules.md) dice que `reranking` *"no se crea hasta que exista"*, y [ADR-0042](0042-pipeline-de-decision-del-turno.md) la extendió a `retrieval` y `grounding`. Esa regla es sobre los pasos de `pipeline/`; acá se aplica por analogía a un clasificador, y el motivo es el mismo: un clasificador que siempre devuelve `none` produce evidencia falsa, y en este caso en la tabla que lee OE4.

**Por qué el registro va en el `triage`.** Es el único paso que corre en todos los turnos que llegan a clasificarse, así que la anotación queda en una fila que siempre existe, sin tocar el esquema ni la enumeración `PipelineStep`.

## 4. Consecuencias

### Positivas

- **La definición es etiquetable.** El `gold_value` de `annotation_type = 'frustration'` en `conversation_annotations` se anota con un criterio que dice qué mirar.
- **El costo por turno de la señal no depende del proveedor de LLM**, y su versión no se mueve cuando se cambia de modelo de generación.
- **Ninguna consulta cuenta como negativo un turno que no se evaluó**, porque esos turnos no tienen fila de `frustration`.
- **No hay migración ni ahora ni en la Fase 16**: `signal_type` ya admite `frustration`.

### Negativas / costos asumidos

- **Hasta la Fase 16, F12 tiene tres señales de cuatro y OE4 no se puede medir** en su mitad de frustración. La mitad de cambio de intención ([ADR-0023](0023-definicion-de-cambio-de-intencion.md)) no depende de esta señal.
- **Es una desviación de la fecha del anteproyecto.** OE4 declara *"para el 07 de septiembre de 2026"*, y su mitad de frustración se mide recién en la Fase 16.
- **"Salteada" sólo se ve leyendo `pipeline_steps`.** Quien mire sólo `signal_evaluations` no distingue *"salteada"* de *"todavía sin clasificar"*. Es el mismo costo que [ADR-0042](0042-pipeline-de-decision-del-turno.md) §4 asumió para los turnos cortados, ahora extendido a todos los turnos.
- **F36 y OE3 atribuyen mal mientras tanto.** Las transferencias que habría ganado `frustration` se atribuyen a `out_of_domain` o `no_resolution`. Eso baja la precisión fina de OE3 (acertar el tipo de señal, `gold_value = signal_type`) en las conversaciones que el anotador marque como `frustration`, y hace que las tasas por señal de F36 de ese período no sean comparables con las posteriores. Hay que separarlas por fecha.
- **Debilita la justificación de [ADR-0021](0021-atribucion-entre-senales-simultaneas.md).** Ese ADR pone a `frustration` antes que `out_of_domain` y `no_resolution` porque es *"estado del cliente"*, opuesto a *"capacidad del sistema"*. Con la definición acotada, la frustración es la reacción del cliente a una falla de capacidad del agente, igual que `no_resolution`. El orden no cambia, pero su argumento hay que repensarlo al cerrar la frontera de §2.1.
- **La definición deja afuera una frustración real.** El cliente enojado con su problema no dispara la señal. Es un falso negativo elegido, no uno accidental, pero es el tipo de caso que [ADR-0014](0014-medicion-de-oe3-y-oe4.md) quiere vigilar con el recall.
- **La mitad de `frustration` del pendiente de [ADR-0042](0042-pipeline-de-decision-del-turno.md) §4 se aplaza con la señal.** Hasta la Fase 16 ningún turno la evalúa, cortado o no, así que la pregunta no se plantea. Se vuelve a abrir al construir el modelo.
- **Los datos de entrenamiento son un riesgo abierto.** Si salen del mismo generador que las 500 conversaciones de OE4, el 85 % queda inflado aunque ningún ejemplo se repita. Y si se usan conversaciones reales, tienen que estar anonimizadas ([ADR-0006](0006-anonimizacion-de-pii.md), Ley 25.326).
- **El repositorio aparte no está cubierto por [ADR-0028](0028-cobertura-de-pruebas-por-servicio.md)**, que exige el 80 % por servicio de este repositorio.
- **Cómo llega el modelo al cerebro queda sin decidir.** Dentro de la imagen, engorda el cerebro con sus dependencias de inferencia. Como servicio aparte, reabre [ADR-0017](0017-decomposicion-en-despliegues-y-lenguajes.md) —que decidió dos servicios— y ADR-0028. Se decide en la Fase 16.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Frustración amplia** (con cualquier cosa) | Transfiere conversaciones que el agente estaba resolviendo, y el criterio de anotación no es estable entre anotadores |
| **Una llamada al LLM por turno** | Costo y latencia sobre el camino feliz en cada turno, y la versión del detector quedaría atada a la del proveedor |
| **Sumar un puntaje de frustración a la salida del reescritor de F04** | Casi sin costo, pero la `classifier_version` de la señal sería la versión del reescritor: cambiar el prompt del contexto dinámico movería el detector que mide OE4 |
| **Un clasificador provisorio que siempre devuelve `none`** | Escribe negativos que nadie evaluó en la tabla de OE4. Es la ficción que la regla 4 de ADR-0042 ya rechazó |
| **No registrar la señal turno por turno y declarar una fecha de corte** | Más simple, pero ADR-0014 §4 pide *"registrar explícitamente cuáles se saltearon"*, y habría que enmendarlo. Además, una fecha en un documento no entra en un `WHERE` |
| **Entrenar dentro de este repositorio** | Mezcla dependencias de entrenamiento con el cerebro y pone los datos de entrenamiento al lado del conjunto de evaluación |
