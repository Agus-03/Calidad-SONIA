# ADR-0065 — La fragmentación de los documentos

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · el solapamiento inicial en 0, a validar con una evaluación, una sola estrategia, la recursiva, a la que se suman otras sólo cuando la evaluación muestre un tipo de documento donde falla, trozos de unos 500 tokens, y la versión del chunking, la del prompt de descripción y el modelo de embeddings registrados en cada fila de `document_chunks`, decididos por el grupo el 2026-10-03 · **reglas 3 y 4 enmendadas el 2026-10-04 por [ADR-0075](0075-medicion-de-oe6-y-evaluacion-de-recuperacion.md)**: la evaluación se difiere hasta poder pagar Voyage, se corre con el anteproyecto como documento de prueba y sin comparar solapamientos, que quedan en 0 · **§3 enmendada el 2026-10-05 por [ADR-0076](0076-extraccion-del-texto-en-modo-layout.md)**: medido sobre el anteproyecto, la mayoría de los trozos no termina donde termina una idea · **§4 enmendada el 2026-10-05 por [ADR-0078](0078-sin-comparacion-de-modelos-de-embeddings.md)**: la evaluación no compara modelos |
| **Fecha** | 2026-10-03 |
| **Etapa** | Construcción — Fase 12 (conocimiento) |
| **Resuelve** | Cómo y con qué parámetros se parte un documento en `document_chunks`, el pendiente de chunking que dejó [ADR-0064](0064-ingesta-de-documentos.md) |
| **Requisitos afectados** | F06 · OE6 |
| **Relacionada con** | [ADR-0005](0005-base-de-conocimiento.md), [ADR-0016](0016-rastro-del-pipeline-de-respuesta.md), [ADR-0024](0024-unidad-y-alcance-de-oe6.md), [ADR-0026](0026-conjuntos-de-evaluacion-simulados.md), [ADR-0064](0064-ingesta-de-documentos.md), [ADR-0067](0067-modelo-de-embeddings-e-imagenes.md) |

---

## 1. Contexto

F06 pide *"búsqueda semántica para enriquecer respuestas"* y no dice cómo se fragmenta un documento. [ADR-0005](0005-base-de-conocimiento.md) fijó que los fragmentos son filas de `document_chunks` y que son la unidad que se recupera, y [ADR-0064](0064-ingesta-de-documentos.md) dejó el chunking para un ADR propio.

**Los documentos del cooperante son variados**, y eso pesa sobre dos decisiones:

- **La estrategia.** Las que se evaluaron son la de tamaño fijo, la recursiva (prueba separadores de mayor a menor —sección, párrafo, oración, palabra— hasta que el trozo entra en el tamaño), por oración, por estructura del PDF, semántica, padre-hijo y contextual. Ninguna es la mejor para todos los documentos, y la tentación es elegir una distinta para cada uno.
- **El solapamiento**: cuánto texto del final de un trozo se repite al principio del siguiente. Sirve cuando un corte cae a mitad de una idea, porque la idea queda entera en uno de los dos trozos. Cuesta trozos repetidos: más vectores, más tokens al vectorizar y, en el prompt, el mismo texto dos veces si se recuperan dos trozos vecinos.

El valor habitual de cada parámetro se elige de memoria, y el que sirve depende de documentos que todavía no medimos. Ningún número se puede defender sin medirlo.

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **El solapamiento arranca en 0.** Cada trozo empieza donde terminó el anterior |
| 2 | **Los parámetros se versionan en `rules/chunking.yaml`**, por la regla 9 de [08-structure-rules](../08-structure-rules.md): cambiarlos cambia lo que se recupera, y con eso OE6 |
| 3 | **Se valida a posteriori, con una evaluación de recuperación offline** en `src/test/evals/`: un conjunto de preguntas sobre los documentos del cooperante, cada una con el fragmento que la responde, y la proporción de preguntas cuyo fragmento aparece entre los primeros *k* recuperados (*recall@k*). Se compara el solapamiento 0 con al menos un valor mayor, sobre los mismos documentos y con todo lo demás fijo *(Enmendada por [ADR-0075](0075-medicion-de-oe6-y-evaluacion-de-recuperacion.md), reglas 3 a 5: se difiere hasta poder pagar Voyage, el documento de prueba es el anteproyecto y no los del cooperante, y no se compara el solapamiento: queda en 0.)* |
| 4 | **Si la evaluación muestra que un solapamiento mayor recupera mejor, se sube**: se edita `chunking.yaml`, sube su `version` y se reindexa volviendo los documentos a `pending` ([ADR-0064](0064-ingesta-de-documentos.md), regla 11). El criterio de "mejor" —cuánta diferencia de *recall@k* justifica el cambio— se fija al armar la evaluación, antes de mirar los resultados *(Deja de aplicar por [ADR-0075](0075-medicion-de-oe6-y-evaluacion-de-recuperacion.md), regla 5: el solapamiento queda en 0.)* |
| 5 | **Hay una sola estrategia, la recursiva**, para todos los documentos |
| 6 | **Otra estrategia se suma sólo cuando la evaluación de la regla 3 muestre un tipo de documento en el que la recursiva recupera mal**, y con ella un selector que decide cuál aplicar a cada documento. **El selector mira rasgos del PDF** —proporción de títulos, densidad de tablas, largo medio de los párrafos— **y no le pregunta a un LLM**: tiene que dar siempre la misma respuesta para el mismo documento *(Con un solo documento de prueba —[ADR-0075](0075-medicion-de-oe6-y-evaluacion-de-recuperacion.md), regla 4— la evaluación no puede mostrar un tipo de documento en el que la recursiva falle: esta regla espera a una evaluación con más de un tipo de documento.)* |
| 7 | **Mientras haya una sola estrategia, es una función y no una interfaz.** La interfaz común aparece con la segunda implementación, como pide la regla 11 de [08-structure-rules](../08-structure-rules.md) para los puertos |
| 8 | **Un trozo tiene como máximo unos 500 tokens**, contados como los cuenta `document_chunks.token_count`. Es un parámetro de `chunking.yaml`, y la evaluación de la regla 3 lo valida igual que al solapamiento *(El solapamiento ya no se evalúa: [ADR-0075](0075-medicion-de-oe6-y-evaluacion-de-recuperacion.md), regla 5.)* |
| 9 | **Cada fila de `document_chunks` registra con qué se hizo**: `chunking_version` (la `version` de `chunking.yaml`), `description_prompt_version` (la del prompt que describe las imágenes, [ADR-0067](0067-modelo-de-embeddings-e-imagenes.md) regla 7) y `embedding_model` (el modelo que vectorizó el trozo). El `worker` las escribe al crear el trozo y no cambian nunca. Cuando exista la segunda estrategia, la estrategia usada se suma como otra columna |

## 3. Justificación

- **Arrancar en 0 porque es el punto de partida que no supone nada.** Un solapamiento de 10 o 15 % es una costumbre, no un dato sobre estos documentos. Empezando sin solapamiento, cualquier valor que se agregue después tiene que ganarse su costo en la evaluación.
- **Con cortes entre párrafos, el solapamiento aporta poco.** La recursiva corta en los separadores naturales del texto, así que la mayoría de los trozos terminan donde termina una idea, y repetir el final del anterior no agrega contexto. *(Enmendado por [ADR-0076](0076-extraccion-del-texto-en-modo-layout.md): medido sobre el anteproyecto, el 40 % de los trozos termina a mitad de una oración, así que este argumento no se sostiene. El solapamiento sigue en 0 por [ADR-0075](0075-medicion-de-oe6-y-evaluacion-de-recuperacion.md), regla 5.)*
- **La recursiva porque se adapta sola a documentos variados.** Usa los separadores que cada documento tenga y no falla cuando no tiene ninguno: un PDF con títulos se parte por secciones y uno sin estructura, por párrafos u oraciones. Las demás estrategias rinden mejor sólo en un tipo de documento, o cuestan una llamada a un modelo por oración o por trozo al ingerir.
- **Una estrategia por documento cuesta más en la validación que en el código.** Escribir otra función de fragmentación y un selector es poco código. Lo caro es que cada estrategia y el propio selector necesitan su medición —un selector que se equivoca es invisible si no se mide— y que OE6 sólo es reproducible si se sabe con qué estrategia se partió cada documento. Sumarlas cuando un dato lo justifica paga ese costo una vez por estrategia que sirve, no por cada una que se pruebe.
- **Un selector determinista y no un LLM** porque el mismo documento tiene que fragmentarse igual en cada reindexación. Un LLM agregaría además una llamada por documento.
- **Fijar el criterio antes de medir** evita elegir el umbral que favorece al resultado que ya se vio.
- **Unos 500 tokens** porque un trozo tiene que traer una idea completa —una cláusula, un ítem de una política— y a la vez ser lo bastante chico para que la búsqueda apunte a lo que se preguntó y entren varios en el prompt sin inflar su costo. `voyage-4` acepta mucho más, pero un trozo más largo mezcla temas y su vector se vuelve un promedio de todos. Es un número de partida, como el solapamiento: lo sostiene la evaluación, no la costumbre.
- **La versión en el trozo y no en el documento** porque el trozo es la fila que no cambia. Una reindexación crea trozos nuevos y marca los viejos con `deleted_at`, que se conservan porque `retrieval_logs` los referencia. Si la versión estuviera en `documents`, se pisaría al reindexar, y una medición vieja de OE6 leería, para los trozos que recuperó, la versión con la que se hicieron los trozos nuevos.
- **Cambiar después es barato.** Los parámetros son datos versionados y reindexar es la misma ingesta, así que equivocarse cuesta una reindexación, no código.

## 4. Consecuencias

### Positivas

- La decisión queda escrita como hipótesis con su forma de comprobarla, no como números elegidos de memoria.
- La evaluación de la regla 3 sirve también para el resto de los parámetros, para justificar cada estrategia nueva y para comparar modelos de embeddings. *(La comparación de modelos deja de hacerse por [ADR-0078](0078-sin-comparacion-de-modelos-de-embeddings.md).)*
- Con 0 no hay trozos repetidos: menos vectores y ningún texto duplicado en el prompt.
- Una sola estrategia es un solo camino que probar mientras no haya datos.
- Cada medición de OE6 sabe con qué versión de chunking, de prompt de descripción y de modelo se hizo cada trozo que recuperó, también después de una reindexación.

### Negativas / costos asumidos

- **Hasta que exista la evaluación, nada de esto está validado.** Un corte a mitad de una idea la parte en dos trozos, y puede que ninguno de los dos se recupere bien. Es el riesgo que se acepta a cambio de no suponer un número.
- **Un tipo de documento en el que la recursiva rinde mal sigue rindiendo mal** hasta que la evaluación lo encuentre. Si la evaluación no tiene preguntas sobre ese tipo, no lo va a encontrar nunca.
- **La evaluación hay que construirla**, y necesita documentos reales del cooperante y preguntas con su respuesta marcada a mano. Sin eso, las reglas 3, 4 y 6 no se pueden cumplir. *(Enmendada por [ADR-0075](0075-medicion-de-oe6-y-evaluacion-de-recuperacion.md), regla 4: el documento de prueba es el anteproyecto, y la evaluación se difiere.)*
- **Las preguntas las escribe el equipo**, que conoce el sistema. Es el mismo sesgo que [ADR-0026](0026-conjuntos-de-evaluacion-simulados.md) declara para los conjuntos de OE3 y OE4, y hay que declararlo igual.
- **Un cambio de parámetros o de estrategia vuelve incomparables las mediciones de OE6 de antes y de después.** La regla 9 permite separarlas, pero no compararlas.
- **Los 500 tokens se eligen de memoria**, con el mismo riesgo que la regla 1: hasta la evaluación, nada dice que sea el tamaño que mejor recupera en estos documentos.
- **Las tres columnas repiten el mismo valor en todos los trozos de un documento.** Son unos bytes por fila, a cambio de que la versión esté en la fila que no cambia.
- **Saber con qué está indexado hoy un documento obliga a mirar sus trozos activos**: no hay una columna en `documents` que lo diga.
- **Tres columnas nuevas en `document_chunks`**, que se agregan editando `0001` mientras no haya una WABA real ([ADR-0033](0033-secretos-de-bandejas-nullables-y-congelamiento-de-0001.md)).

### Lo que queda abierto

| Qué | Estado |
|---|---|
| ~~**El tamaño de los trozos**~~ | ~~En discusión~~ *(Cerrado el 2026-10-03: regla 8.)* |
| ~~**Dónde queda registrada la `version` de `chunking.yaml`** —y, desde la segunda estrategia, cuál se usó— con la que se fragmentó cada documento~~ | ~~En discusión. Es una columna, así que toca `0001`~~ *(Cerrado el 2026-10-03: regla 9.)* |

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Un solapamiento de 10 a 15 % desde el arranque** | Es el valor de costumbre, y no hay ningún dato sobre estos documentos que lo sostenga |
| **Varias estrategias y un selector desde el primer día** | Multiplica las mediciones antes de saber si hace falta más de una estrategia |
| **Un selector que le pregunte a un LLM** | No es determinista, así que el mismo documento podría fragmentarse distinto en cada reindexación, y cuesta una llamada por documento |
| **Que el admin elija el tipo de documento al subirlo** | Le traslada una decisión técnica que no tiene por qué conocer, y suma una columna antes de que la evaluación muestre que hace falta |
| **La estrategia contextual** (un LLM antepone a cada trozo una frase sobre el documento entero) | Mejora la recuperación, pero cuesta una llamada al LLM por trozo al ingerir. Queda como mejora medible después: no exige DDL |
| **Registrar las versiones en `documents`** | Una columna por documento en vez de una por trozo, pero se pisa al reindexar, y las mediciones viejas de OE6 leerían la versión de los trozos nuevos |
| **Trozos más grandes** (1000 tokens o más) | Menos vectores, pero cada trozo mezcla temas, su vector se diluye y cada uno recuperado ocupa más del prompt |
| **Trozos más chicos** (200 tokens o menos) | Búsqueda más precisa, pero parten cláusulas e ítems y obligan a recuperar más trozos para tener una idea entera |
| **Una interfaz de estrategias con una sola implementación** | Es abstracción sin una segunda implementación a la vista, que la regla 11 de `08-structure-rules` pide no crear |
