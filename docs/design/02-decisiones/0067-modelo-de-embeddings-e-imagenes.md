# ADR-0067 — El modelo de embeddings y las imágenes de los documentos

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · arrancar con `embed-multilingual-v3.0` de Cohere a 1024 dimensiones, compararlo después con `voyage-4` y `text-embedding-3-small`, y vectorizar sólo texto describiendo cada imagen al ingerir con Claude Sonnet 5.5, elegidos por el grupo el 2026-10-03 |
| **Fecha** | 2026-10-03 |
| **Etapa** | Construcción — Fase 12 (conocimiento) |
| **Resuelve** | El modelo exacto de embeddings y su dimensión, que [ADR-0018](0018-proveedor-de-llm-y-embeddings.md) dejó abierto con fecha límite —*"antes de crear la colección de Qdrant"*—, y qué se hace con las imágenes de un PDF, que [ADR-0064](0064-ingesta-de-documentos.md) regla 10 difirió |
| **Requisitos afectados** | F06 · OE6 |
| **Enmienda** | [ADR-0018](0018-proveedor-de-llm-y-embeddings.md), **§2** — el modelo exacto deja de estar abierto · **§5** — la alternativa *"fijar el modelo exacto ahora, sin evaluarlo"* se adopta, con otro argumento: con la dimensión fija en una que dan todos los candidatos, revertir cuesta una reindexación y no una colección nueva · [ADR-0064](0064-ingesta-de-documentos.md), **regla 10 y *Lo que queda abierto*** — las imágenes se describen y el modelo queda fijado |
| **Relacionada con** | [ADR-0005](0005-base-de-conocimiento.md), [ADR-0010](0010-qdrant-como-base-vectorial.md), [ADR-0016](0016-rastro-del-pipeline-de-respuesta.md), [ADR-0065](0065-fragmentacion-de-documentos.md), [ADR-0066](0066-datos-personales-en-los-documentos.md) |

---

## 1. Contexto

[ADR-0018](0018-proveedor-de-llm-y-embeddings.md) eligió Cohere para los embeddings y dejó abierto el modelo exacto, porque la dimensión de la colección de Qdrant se fija al crearla y la Fase 12 no puede crear ninguna sin ella. Descartó fijarlo *"sin evaluarlo"*: la dimensión era lo más caro de revertir.

Se compararon tres proveedores. Los precios por millón de tokens se tomaron de las páginas oficiales el 2026-10-03, salvo el de Cohere, que su página de precios no publica para la API y que se cita de memoria:

| Modelo | $ por 1M tokens | Dimensiones |
|---|---|---|
| OpenAI `text-embedding-3-small` | 0,02 | 1536, reducible a 1024 |
| Voyage `voyage-4-lite` | 0,02 | 1024 por defecto |
| Voyage `voyage-4` | 0,06 | 1024 por defecto |
| Voyage `voyage-4-large` | 0,12 | 1024 por defecto |
| OpenAI `text-embedding-3-large` | 0,13 | 3072, reducible a 1024 |
| Cohere `embed-multilingual-v3.0` | ~0,10 *(sin verificar)* | 1024 |

**El precio no distingue a ninguno.** Una base de 100 MB de texto puro —el peor caso de F06, unos 25 millones de tokens— cuesta menos de $5 de indexar con cualquiera, y vectorizar la pregunta de cada turno es despreciable. Lo que los distingue es la calidad de recuperación en español sobre los documentos del cooperante, y eso no lo dice ninguna tabla.

**Anthropic no tiene modelo de embeddings.** Su documentación recomienda Voyage AI, que es una empresa aparte.

**Los PDF pueden traer imágenes**: planos, tablas escaneadas, fotos. Hay dos maneras de aprovecharlas: vectorizarlas con un modelo multimodal, o describirlas con un modelo con visión al ingerir y vectorizar la descripción como texto.

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **El modelo de embeddings es `embed-multilingual-v3.0` de Cohere**, el proveedor que fijó ADR-0018 |
| 2 | **La dimensión es 1024**, y las colecciones de Qdrant se crean con ella y con distancia coseno |
| 3 | **Se compara después con `voyage-4` y `text-embedding-3-small`**, con la evaluación de recuperación de [ADR-0065](0065-fragmentacion-de-documentos.md) (*recall@k* sobre los documentos del cooperante, con todo lo demás fijo). Si uno recupera claramente mejor, se cambia y se reindexa ([ADR-0064](0064-ingesta-de-documentos.md), regla 11). Cambiar a otro proveedor enmienda ADR-0018 |
| 4 | **Se vectoriza sólo texto.** Las imágenes no se vectorizan |
| 5 | **Cada imagen de un PDF se describe al ingerir con Claude Sonnet 5.5**, y la descripción entra al texto de la página donde estaba la imagen, antes de fragmentar. Así el chunking recursivo la deja junto a su contexto |
| 6 | **Las imágenes chicas no se describen**: logos e íconos no aportan contenido. El tamaño mínimo es un parámetro versionado en `rules/` |
| 7 | **El prompt de la descripción se versiona en `rules/`** (regla 9 de [08-structure-rules](../08-structure-rules.md)), en español, y pide decir lo que no se lee en vez de completarlo |
| 8 | **La llamada pasa por el puerto de LLM**, que hoy acepta sólo texto: se le agregan imágenes, en los cuatro adaptadores. Ningún módulo importa el SDK de Anthropic por fuera de su adaptador |
| 9 | **Si la descripción falla, la imagen se omite y el documento se ingiere igual**, con la falla en el log. Un documento no pierde su texto porque una imagen no se pudo leer |

## 3. Justificación

- **Cohere porque no suma proveedor.** ADR-0018 ya lo eligió para embeddings, el SDK está en el cerebro y el adaptador existe. Voyage —lo que recomienda Anthropic— sería una cuenta, un SDK y un modo de falla más, y OpenAI otro tanto para los embeddings.
- **1024 porque la dan los tres candidatos.** Fijar la dimensión en una que todos pueden dar convierte un cambio de modelo en una reindexación, que es la misma ingesta, en vez de una colección nueva.
- **Fijar el modelo antes de medir**, que es lo que ADR-0018 descartó, porque su argumento ya no vale: lo caro de revertir era la dimensión, y la dimensión queda fija sea cual sea el modelo. Esperar la evaluación bloquearía la fase, y la evaluación necesita documentos reales del cooperante que todavía no están.
- **Sólo texto, porque lo que se recupera tiene que poder entrar al prompt.** El `user` de la Regla 1 de [03-capa-prompts](../03-capa-prompts.md) es texto. Una imagen recuperada habría que mandársela al LLM en cada turno que la use —de 1.300 a 4.800 tokens más por imagen y por turno— con un modelo con visión en la generación. Y la respuesta llega por WhatsApp como texto: lo que la clienta necesita de un plano es lo que dice.
- **Un modelo multimodal, además, obliga a vectorizar todo con él**, porque una colección tiene que tener todos sus vectores en el mismo espacio. Elegir imágenes sería elegir también el modelo del texto.
- **Describir al ingerir se paga una sola vez por imagen**, no en cada turno.
- **Sonnet 5.5 y no Haiku 4.5** por la resolución: Sonnet 5.5 lee imágenes hasta 2576 px de lado largo, y Haiku 4.5 las achica a 1568 px. En un plano o una tabla con letra chica, es la diferencia entre leer los números o no. El costo no distingue a los dos al volumen de una cooperante: unos $4,60 contra $2,30 cada mil imágenes de 1000×1000.
- **Anthropic ya es el respaldo de ADR-0018**, así que la descripción no suma proveedor.

## 4. Consecuencias

### Positivas

- La fase deja de estar bloqueada: las colecciones se pueden crear.
- Cambiar de modelo de embeddings cuesta una reindexación, no un rediseño.
- Los PDF con imágenes se aprovechan: lo que dice un plano o una tabla escaneada se puede recuperar.
- El pipeline de recuperación y el prompt siguen siendo de texto.

### Negativas / costos asumidos

- **El modelo se fija sin la prueba en español que pedía ADR-0018.** Hasta que exista la evaluación de ADR-0065, nada dice que sea el mejor de los tres.
- **El precio de Cohere no está verificado** en una fuente oficial.
- **`embed-multilingual-v3.0` es de una generación anterior** a los de Voyage y OpenAI que se comparan. Puede perder en la evaluación.
- **Generación, recuperación y descripción dependen de proveedores externos.** Si Cohere cae, se cae la ingesta y la búsqueda: el respaldo de Anthropic cubre sólo la generación (ADR-0018). Si Anthropic cae, los documentos se ingieren sin sus imágenes (regla 9) y nadie se entera salvo por el log.
- **Una descripción equivocada pasa a ser dato de la base**, y la evaluación de ADR-0065 no la detecta salvo que tenga preguntas sobre imágenes.
- **Las imágenes salen hacia Anthropic**, con lo que tengan adentro. Lo cubre el aviso de [ADR-0066](0066-datos-personales-en-los-documentos.md), que nombra a los proveedores que procesan el contenido.
- **Agregar imágenes al puerto de LLM toca los cuatro adaptadores** —Cohere, Anthropic, OpenAI y el doble de los tests—, aunque sólo uno las use para esto.
- **El bot no puede mandar una imagen.** Si algún día hace falta, la imagen original sigue en el almacén, pero es otro trabajo.
- **Una imagen que se omite por falla no deja rastro en la base**, sólo en el log.

### Lo que queda abierto

| Qué | Estado |
|---|---|
| **Qué se registra por documento**: la versión de `chunking.yaml`, la del prompt de descripción y el modelo de embeddings con que se procesó. Sin eso, una medición de OE6 no sabe con qué se indexó cada documento | Se decide junto con el pendiente de [ADR-0065](0065-fragmentacion-de-documentos.md); es una o más columnas en `documents` |
| **El tamaño mínimo de una imagen para describirla** | Se fija al construir el `worker` (T7), en `rules/` |

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Voyage desde el arranque** | Es lo que recomienda Anthropic y Voyage dice ser el mejor en multilingüe, pero suma un proveedor y enmienda ADR-0018 sin un dato propio que lo justifique. Queda en la comparación de la regla 3 |
| **`text-embedding-3-small` de OpenAI desde el arranque** | El más barato, pero suma un proveedor para los embeddings, y el precio no distingue a ninguno |
| **Esperar la evaluación para elegir** | Bloquea la fase hasta tener documentos reales del cooperante, y la dimensión fija ya hace barato cambiar después |
| **Embeddings multimodales** (`voyage-multimodal-3.5` o similar) | Obligan a vectorizar todo con ese modelo, a mandar imágenes al LLM en cada turno que las use, y la respuesta igual es texto |
| **Ignorar las imágenes** | Pierde lo que esté sólo en un plano o en una tabla escaneada |
| **Mandarle a Claude cada página entera del PDF para transcribirla** | Resolvería también los PDF escaneados, pero cada página paga tokens de imagen aunque sea sólo texto, y reabre el *"sin OCR"* de ADR-0064 |
| **Describir con Haiku 4.5** | La mitad de caro, pero achica las imágenes a 1568 px, y en planos y tablas con letra chica eso pierde los números |
