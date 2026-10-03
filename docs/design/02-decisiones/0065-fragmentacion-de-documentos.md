# ADR-0065 — La fragmentación de los documentos

| Campo | Valor |
|---|---|
| **Estado** | **Propuesta** · el solapamiento inicial en 0, a validar con una evaluación, decidido por el grupo el 2026-10-03 · **la estrategia de fragmentación y el tamaño de los trozos siguen en discusión**, y este ADR se completa cuando se decidan |
| **Fecha** | 2026-10-03 |
| **Etapa** | Construcción — Fase 12 (conocimiento) |
| **Resuelve** | Con qué parámetros se parte un documento en `document_chunks`, el pendiente de chunking que dejó [ADR-0064](0064-ingesta-de-documentos.md) |
| **Requisitos afectados** | F06 · OE6 |
| **Relacionada con** | [ADR-0005](0005-base-de-conocimiento.md), [ADR-0016](0016-rastro-del-pipeline-de-respuesta.md), [ADR-0024](0024-unidad-y-alcance-de-oe6.md), [ADR-0064](0064-ingesta-de-documentos.md) |

---

## 1. Contexto

F06 pide *"búsqueda semántica para enriquecer respuestas"* y no dice cómo se fragmenta un documento. [ADR-0005](0005-base-de-conocimiento.md) fijó que los fragmentos son filas de `document_chunks` y que son la unidad que se recupera, y [ADR-0064](0064-ingesta-de-documentos.md) dejó el chunking para un ADR propio.

Cualquier estrategia que limite el tamaño de los trozos tiene un parámetro de **solapamiento**: cuánto texto del final de un trozo se repite al principio del siguiente. Sirve cuando un corte cae a mitad de una idea, porque la idea queda entera en uno de los dos trozos. Cuesta trozos repetidos: más vectores, más tokens al vectorizar y, en el prompt, el mismo texto dos veces si se recuperan dos trozos vecinos.

El valor habitual se elige de memoria, y el que sirve depende de los documentos del cooperante, que son variados. Ningún número se puede defender sin medirlo.

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **El solapamiento arranca en 0.** Cada trozo empieza donde terminó el anterior |
| 2 | **El valor se versiona en `rules/chunking.yaml`**, con los demás parámetros de la fragmentación, por la regla 9 de [08-structure-rules](../08-structure-rules.md): cambiarlo cambia lo que se recupera, y con eso OE6 |
| 3 | **Se valida a posteriori, con una evaluación de recuperación offline** en `src/test/evals/`: un conjunto de preguntas sobre los documentos del cooperante, cada una con el fragmento que la responde, y la proporción de preguntas cuyo fragmento aparece entre los primeros *k* recuperados (*recall@k*). Se compara el solapamiento 0 con al menos un valor mayor, sobre los mismos documentos y con todo lo demás fijo |
| 4 | **Si la evaluación muestra que un solapamiento mayor recupera mejor, se sube**: se edita `chunking.yaml`, sube su `version` y se reindexa volviendo los documentos a `pending` ([ADR-0064](0064-ingesta-de-documentos.md), regla 11). El criterio de "mejor" —cuánta diferencia de *recall@k* justifica el cambio— se fija al armar la evaluación, antes de mirar los resultados |

## 3. Justificación

- **Arrancar en 0 porque es el punto de partida que no supone nada.** Un solapamiento de 10 o 15 % es una costumbre, no un dato sobre estos documentos. Empezando sin solapamiento, cualquier valor que se agregue después tiene que ganarse su costo en la evaluación.
- **Con cortes entre párrafos, el solapamiento aporta poco.** Si la estrategia corta en los separadores naturales del texto, la mayoría de los trozos terminan donde termina una idea, y repetir el final del anterior no agrega contexto.
- **Fijar el criterio antes de medir** evita elegir el umbral que favorece al resultado que ya se vio.
- **Cambiar después es barato.** El valor es un dato versionado y reindexar es la misma ingesta, así que equivocarse cuesta una reindexación, no código.

## 4. Consecuencias

### Positivas

- La decisión queda escrita como hipótesis con su forma de comprobarla, no como un número elegido de memoria.
- La evaluación de la regla 3 sirve también para el resto de los parámetros de la fragmentación y para comparar modelos de embeddings.
- Con 0 no hay trozos repetidos: menos vectores y ningún texto duplicado en el prompt.

### Negativas / costos asumidos

- **Hasta que exista la evaluación, la decisión no está validada.** Un corte a mitad de una idea la parte en dos trozos, y puede que ninguno de los dos se recupere bien. Es el riesgo que se acepta a cambio de no suponer un número.
- **La evaluación hay que construirla**, y necesita documentos reales del cooperante y preguntas con su respuesta marcada a mano. Sin eso, la regla 3 no se puede cumplir.
- **Las preguntas las escribe el equipo**, que conoce el sistema. Es el mismo sesgo que [ADR-0026](0026-conjuntos-de-evaluacion-simulados.md) declara para los conjuntos de OE3 y OE4, y hay que declararlo igual.
- **Un cambio de solapamiento vuelve incomparables las mediciones de OE6 de antes y de después.** La `version` de `chunking.yaml` es lo que permite separarlas, y por ahora no viaja a ninguna fila de la recuperación.

### Lo que queda abierto

| Qué | Estado |
|---|---|
| **La estrategia de fragmentación** (recursiva, por estructura, adaptada a cada documento) | En discusión |
| **El tamaño de los trozos** | En discusión |
| **Dónde queda registrada la `version` de `chunking.yaml`** con la que se fragmentó cada documento | Se decide con la estrategia |
