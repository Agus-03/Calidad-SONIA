# ADR-0067 — El modelo de embeddings y las imágenes de los documentos

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · arrancar con `voyage-4` de Voyage AI a 1024 dimensiones, compararlo después con `embed-multilingual-v3.0` y `text-embedding-3-small`, y vectorizar sólo texto describiendo cada imagen al ingerir con Claude Sonnet 5.5, elegidos por el grupo el 2026-10-03 · **regla 3 enmendada el 2026-10-04 por [ADR-0075](0075-medicion-de-oe6-y-evaluacion-de-recuperacion.md)**: la evaluación con la que se comparan los modelos se difiere y usa el anteproyecto · **regla 3 sin efecto desde el 2026-10-05 por [ADR-0078](0078-sin-comparacion-de-modelos-de-embeddings.md)**: no se comparan modelos |
| **Fecha** | 2026-10-03 |
| **Etapa** | Construcción — Fase 12 (conocimiento) |
| **Resuelve** | El modelo exacto de embeddings y su dimensión, que [ADR-0018](0018-proveedor-de-llm-y-embeddings.md) dejó abierto con fecha límite —*"antes de crear la colección de Qdrant"*—, y qué se hace con las imágenes de un PDF, que [ADR-0064](0064-ingesta-de-documentos.md) regla 10 difirió |
| **Requisitos afectados** | F06 · OE6 |
| **Enmienda** | [ADR-0018](0018-proveedor-de-llm-y-embeddings.md), **§2** — los embeddings dejan de ser de Cohere y el modelo exacto deja de estar abierto · **§3** — el argumento *"embeddings del mismo proveedor que el primario"* deja de aplicarse · **§4** — generación y recuperación dejan de compartir proveedor · **§5** — se adoptan dos alternativas descartadas: *"embeddings de otro proveedor"* y *"fijar el modelo exacto ahora, sin evaluarlo"*, esta última con otro argumento: con la dimensión fija en una que dan todos los candidatos, revertir cuesta una reindexación y no una colección nueva · [ADR-0064](0064-ingesta-de-documentos.md), **regla 10 y *Lo que queda abierto*** — las imágenes se describen y el modelo queda fijado |
| **Relacionada con** | [ADR-0005](0005-base-de-conocimiento.md), [ADR-0010](0010-qdrant-como-base-vectorial.md), [ADR-0016](0016-rastro-del-pipeline-de-respuesta.md), [ADR-0017](0017-decomposicion-en-despliegues-y-lenguajes.md), [ADR-0065](0065-fragmentacion-de-documentos.md), [ADR-0066](0066-datos-personales-en-los-documentos.md) |

---

## 1. Contexto

[ADR-0018](0018-proveedor-de-llm-y-embeddings.md) eligió Cohere para los embeddings y dejó abierto el modelo exacto, porque la dimensión de la colección de Qdrant se fija al crearla y la Fase 12 no puede crear ninguna sin ella. Descartó fijarlo *"sin evaluarlo"*: la dimensión era lo más caro de revertir. Y descartó los embeddings de otro proveedor *"sin beneficio identificado"*, dejando escrito que se revisaba *"si la calidad multilingüe en español no alcanza"*.

Se compararon tres proveedores. Los precios por millón de tokens se tomaron de las páginas oficiales el 2026-10-03, salvo el de Cohere, que su página de precios no publica para la API y que se cita de memoria:

| Modelo | $ por 1M tokens | Dimensiones |
|---|---|---|
| OpenAI `text-embedding-3-small` | 0,02 | 1536, reducible a 1024 |
| Voyage `voyage-4-lite` | 0,02 | 1024 por defecto |
| Voyage `voyage-4` | 0,06 | 1024 por defecto |
| Voyage `voyage-4-large` | 0,12 | 1024 por defecto |
| OpenAI `text-embedding-3-large` | 0,13 | 3072, reducible a 1024 |
| Cohere `embed-multilingual-v3.0` | ~0,10 *(sin verificar)* | 1024 |

Voyage da además 200 millones de tokens gratis por cuenta.

**El precio no distingue a ninguno.** Una base de 100 MB de texto puro —el peor caso de F06, unos 25 millones de tokens— cuesta menos de $5 de indexar con cualquiera, y vectorizar la pregunta de cada turno es despreciable. Lo que los distingue es la calidad de recuperación en español sobre los documentos del cooperante, y eso no lo dice ninguna tabla.

**Anthropic no tiene modelo de embeddings.** Su documentación recomienda Voyage AI, que es una empresa aparte: es de MongoDB desde 2025.

**Los PDF pueden traer imágenes**: planos, tablas escaneadas, fotos. Hay dos maneras de aprovecharlas: vectorizarlas con un modelo multimodal, o describirlas con un modelo con visión al ingerir y vectorizar la descripción como texto.

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **El modelo de embeddings es `voyage-4` de Voyage AI.** Es un proveedor nuevo, con su propia credencial, y su cliente vive en `adapters/` como el de los demás proveedores ([ADR-0017](0017-decomposicion-en-despliegues-y-lenguajes.md)) |
| 2 | **La dimensión es 1024**, y las colecciones de Qdrant se crean con ella y con distancia coseno |
| 3 | **Se compara después con `embed-multilingual-v3.0` y `text-embedding-3-small`**, con la evaluación de recuperación de [ADR-0065](0065-fragmentacion-de-documentos.md) (*recall@k* sobre los documentos del cooperante, con todo lo demás fijo). Si uno recupera claramente mejor, se cambia y se reindexa ([ADR-0064](0064-ingesta-de-documentos.md), regla 11). Cambiar de proveedor enmienda este ADR *(Enmendada por [ADR-0075](0075-medicion-de-oe6-y-evaluacion-de-recuperacion.md), reglas 3 y 4: la evaluación se difiere y se corre con el anteproyecto, no con los documentos del cooperante. Si la comparación de modelos sigue dentro de ella, queda para decidir al armarla.)* *(Deja de aplicar por [ADR-0078](0078-sin-comparacion-de-modelos-de-embeddings.md): no se comparan modelos, y el de embeddings es `voyage-4`.)* |
| 4 | **Se vectoriza sólo texto.** Las imágenes no se vectorizan |
| 5 | **Cada imagen de un PDF se describe al ingerir con Claude Sonnet 5.5**, y la descripción entra al texto de la página donde estaba la imagen, antes de fragmentar. Así el chunking recursivo la deja junto a su contexto |
| 6 | **Las imágenes chicas no se describen**: logos e íconos no aportan contenido. El tamaño mínimo es un parámetro versionado en `rules/` |
| 7 | **El prompt de la descripción se versiona en `rules/`** (regla 9 de [08-structure-rules](../08-structure-rules.md)), en español, y pide decir lo que no se lee en vez de completarlo |
| 8 | **La llamada pasa por el puerto de LLM**, que hoy acepta sólo texto: se le agregan imágenes, en los cuatro adaptadores. Ningún módulo importa el SDK de Anthropic por fuera de su adaptador |
| 9 | **Si la descripción falla, la imagen se omite y el documento se ingiere igual**, con la falla en el log. Un documento no pierde su texto porque una imagen no se pudo leer |
| 10 | **El uso de los datos para entrenar se desactiva en la organización de Voyage antes de mandarle el primer documento.** Voyage (MongoDB) lo tiene activado por defecto |

## 3. Justificación

- **Voyage porque es lo que recomienda Anthropic** y porque es la generación actual: `embed-multilingual-v3.0` es de una generación anterior a los otros dos candidatos. ADR-0018 dejó abierta la puerta a otro proveedor si la calidad en español no alcanzaba; el grupo prefirió arrancar con el modelo más nuevo y que la evaluación de la regla 3 diga si Cohere lo alcanza, en vez de arrancar con el más viejo y esperar que la evaluación diga que no alcanza.
- **`voyage-4` y no `voyage-4-lite` ni `voyage-4-large`** porque es el del medio: al volumen de una cooperante la diferencia de precio son centavos, y la evaluación dirá si hace falta subir o se puede bajar.
- **1024 porque la dan los tres candidatos.** Fijar la dimensión en una que todos pueden dar convierte un cambio de modelo en una reindexación, que es la misma ingesta, en vez de una colección nueva.
- **Fijar el modelo antes de medir**, que es lo que ADR-0018 descartó, porque su argumento ya no vale: lo caro de revertir era la dimensión, y la dimensión queda fija sea cual sea el modelo. Esperar la evaluación bloquearía la fase, y la evaluación necesita documentos reales del cooperante que todavía no están. *(La evaluación usa el anteproyecto desde [ADR-0075](0075-medicion-de-oe6-y-evaluacion-de-recuperacion.md), regla 4.)*
- **Sólo texto, porque lo que se recupera tiene que poder entrar al prompt.** El `user` de la Regla 1 de [03-capa-prompts](../03-capa-prompts.md) es texto. Una imagen recuperada habría que mandársela al LLM en cada turno que la use —de 1.300 a 4.800 tokens más por imagen y por turno— con un modelo con visión en la generación. Y la respuesta llega por WhatsApp como texto: lo que la clienta necesita de un plano es lo que dice.
- **Un modelo multimodal, además, obliga a vectorizar todo con él**, porque una colección tiene que tener todos sus vectores en el mismo espacio. Elegir imágenes sería elegir también el modelo del texto.
- **Describir al ingerir se paga una sola vez por imagen**, no en cada turno.
- **Sonnet 5.5 y no Haiku 4.5** por la resolución: Sonnet 5.5 lee imágenes hasta 2576 px de lado largo, y Haiku 4.5 las achica a 1568 px. En un plano o una tabla con letra chica, es la diferencia entre leer los números o no. El costo no distingue a los dos al volumen de una cooperante: unos $4,60 contra $2,30 cada mil imágenes de 1000×1000.
- **Anthropic ya es el respaldo de ADR-0018**, así que la descripción no suma proveedor.
- **Desactivar el entrenamiento (regla 10)** porque la Ley 25.326 deja que un proveedor procese datos por cuenta del negocio sin el consentimiento de las personas sólo si no los usa para otro fin (art. 25). Con el entrenamiento activado, Voyage usaría el contenido de los documentos para sus propios modelos.

## 4. Consecuencias

### Positivas

- La fase deja de estar bloqueada: las colecciones se pueden crear.
- Se arranca con un modelo de la generación actual.
- Cambiar de modelo de embeddings cuesta una reindexación, no un rediseño.
- Una caída de Cohere ya no se lleva la búsqueda: generación y recuperación dejan de compartir proveedor.
- Los PDF con imágenes se aprovechan: lo que dice un plano o una tabla escaneada se puede recuperar.
- El pipeline de recuperación y el prompt siguen siendo de texto.

### Negativas / costos asumidos

- **El modelo se fija sin la prueba en español que pedía ADR-0018.** Hasta que exista la evaluación de ADR-0065, nada dice que sea el mejor de los tres.
- **Suma un proveedor**: una cuenta, una credencial más (`VOYAGE_API_KEY`, a agregar en [variables-de-entorno](../../standar/variables-de-entorno.md) cuando entre el código), un cliente y un adaptador nuevo en `adapters/`. Es exactamente lo que ADR-0018 §3 quería evitar.
- **Voyage no está entre los candidatos de la sección 8.4** del anteproyecto (*"GPT-4o / Claude (vía API) / Cohere / Otros"*). Cabe en *"Otros"*, pero hay que decirlo así en el informe.
- **Un proveedor más recibe el contenido de los documentos.** El aviso de [ADR-0066](0066-datos-personales-en-los-documentos.md) nombra a los proveedores que procesan el contenido, y tiene que nombrar a Voyage.
- **El precio de Cohere no está verificado** en una fuente oficial, así que la comparación de la regla 3 arranca con un costo de memoria.
- **Generación, recuperación y descripción dependen de proveedores externos.** Si Voyage cae, se cae la ingesta y la búsqueda, y no hay respaldo: el de Anthropic cubre sólo la generación (ADR-0018). Si Anthropic cae, los documentos se ingieren sin sus imágenes (regla 9) y nadie se entera salvo por el log.
- **Una descripción equivocada pasa a ser dato de la base**, y la evaluación de ADR-0065 no la detecta salvo que tenga preguntas sobre imágenes.
- **Las imágenes salen hacia Anthropic**, con lo que tengan adentro. Lo cubre el mismo aviso de ADR-0066.
- **Agregar imágenes al puerto de LLM toca los cuatro adaptadores** —Cohere, Anthropic, OpenAI y el doble de los tests—, aunque sólo uno las use para esto.
- **El bot no puede mandar una imagen.** Si algún día hace falta, la imagen original sigue en el almacén, pero es otro trabajo.
- **Una imagen que se omite por falla no deja rastro en la base**, sólo en el log.
- **La regla 10 es una configuración de la cuenta, no código.** Nada en el sistema la verifica: si alguien crea una organización nueva y se olvida, los documentos alimentan los modelos de Voyage sin que nadie se entere.

### Lo que queda abierto

| Qué | Estado |
|---|---|
| ~~**Qué se registra por documento**: la versión de `chunking.yaml`, la del prompt de descripción y el modelo de embeddings con que se procesó. Sin eso, una medición de OE6 no sabe con qué se indexó cada documento~~ | ~~Se decide junto con el pendiente de [ADR-0065](0065-fragmentacion-de-documentos.md); es una o más columnas en `documents`~~ *(Cerrado por [ADR-0065](0065-fragmentacion-de-documentos.md), regla 9: las tres van en cada fila de `document_chunks`, no en `documents`.)* |
| **El tamaño mínimo de una imagen para describirla** | Se fija al construir el `worker` (T7), en `rules/` |

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **`embed-multilingual-v3.0` de Cohere desde el arranque** | No sumaba proveedor: ADR-0018 ya lo había elegido y el adaptador existe. Pero es de una generación anterior a los otros dos, y el grupo prefirió arrancar con el más nuevo. Queda en la comparación de la regla 3 |
| **`text-embedding-3-small` de OpenAI desde el arranque** | El más barato, pero suma un proveedor igual que Voyage sin ser el que recomienda Anthropic, y el precio no distingue a ninguno. Queda en la comparación de la regla 3 |
| **Esperar la evaluación para elegir** | Bloquea la fase hasta tener documentos reales del cooperante, y la dimensión fija ya hace barato cambiar después |
| **Embeddings multimodales** (`voyage-multimodal-3.5` o similar) | Obligan a vectorizar todo con ese modelo, a mandar imágenes al LLM en cada turno que las use, y la respuesta igual es texto |
| **Ignorar las imágenes** | Pierde lo que esté sólo en un plano o en una tabla escaneada |
| **Mandarle a Claude cada página entera del PDF para transcribirla** | Resolvería también los PDF escaneados, pero cada página paga tokens de imagen aunque sea sólo texto, y reabre el *"sin OCR"* de ADR-0064 |
| **Describir con Haiku 4.5** | La mitad de caro, pero achica las imágenes a 1568 px, y en planos y tablas con letra chica eso pierde los números |
