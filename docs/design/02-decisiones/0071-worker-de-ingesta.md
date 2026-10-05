# ADR-0071 — El worker de ingesta: con qué lee, cómo cuenta y qué se dobla en las pruebas

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · `pypdf` para leer los PDF, los tokens estimados por caracteres, un Qdrant real con Voyage y la descripción doblados en el *smoke*, y `google-cloud-storage` para leer Firebase, decididos por el grupo el 2026-10-04 · las reglas 5 a 12 son decisiones de construcción, tomadas al escribir `worker/` · **regla 3 enmendada por [ADR-0072](0072-qdrant-en-el-compose.md)** (2026-10-04): Qdrant corre en el compose base, que el *smoke* también levanta, y no en uno propio del *smoke* · el modo de extracción de la regla 1, fijado el 2026-10-05 por [ADR-0076](0076-extraccion-del-texto-en-modo-layout.md): *layout*, con el normal por página cuando pierde texto, y `pypdf` fijo en 6.19.0 |
| **Fecha** | 2026-10-04 |
| **Etapa** | Construcción — Fase 12 (conocimiento) |
| **Resuelve** | Cómo se construye el `worker` de [ADR-0064](0064-ingesta-de-documentos.md): con qué bibliotecas, cómo se cuentan los 500 tokens de [ADR-0065](0065-fragmentacion-de-documentos.md) y cómo se prueba sin gastar créditos ni sacar documentos |
| **Requisitos afectados** | F06 · OE6 |
| **Relacionada con** | [ADR-0010](0010-qdrant-como-base-vectorial.md), [ADR-0017](0017-decomposicion-en-despliegues-y-lenguajes.md), [ADR-0064](0064-ingesta-de-documentos.md), [ADR-0065](0065-fragmentacion-de-documentos.md), [ADR-0067](0067-modelo-de-embeddings-e-imagenes.md), [ADR-0069](0069-almacen-de-objetos-en-firebase.md), [ADR-0070](0070-subida-de-documentos.md) |

---

## 1. Contexto

ADR-0064, ADR-0065 y ADR-0067 fijaron qué hace la ingesta: el orden de escritura, la fragmentación recursiva de unos 500 tokens, `voyage-4` y la descripción de las imágenes con Claude Sonnet 5.5. No fijaron con qué se construye, y cuatro elecciones cambian el resultado o el costo:

- **La biblioteca que lee el PDF.** Decide qué texto y qué imágenes ve el resto del pipeline.
- **Cómo se cuentan los tokens.** ADR-0065 dice *"contados como los cuenta `document_chunks.token_count`"*, y no dice cómo se calcula esa columna.
- **Qué se dobla en las pruebas.** Voyage cobra por token y recibe el texto de los documentos.
- **El cliente de Firebase en Python.** El núcleo habla con Firebase por HTTP directo ([ADR-0070](0070-subida-de-documentos.md), regla 4).

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **El PDF se lee con `pypdf`**: texto e imágenes por página. Es Python puro y tiene licencia BSD |
| 2 | **Los tokens se estiman por caracteres, no se cuentan.** `rules/chunking.yaml` declara `chars_per_token: 4`: un trozo tiene como máximo `max_tokens × chars_per_token` caracteres, y `token_count` es su largo dividido por esa equivalencia, redondeado hacia arriba |
| 3 | **En el *smoke*, Qdrant es real y Voyage y la descripción están doblados.** ~~Qdrant corre en un contenedor del compose del *smoke*.~~ *(Enmendada por [ADR-0072](0072-qdrant-en-el-compose.md), regla 3: corre como servicio del compose base, que el *smoke* también levanta.)* Voyage se dobla con `EMBEDDINGS_USE_DOUBLE` y la descripción con `LLM_USE_DOUBLE`, que se admiten sólo en `development` y `test`, como los demás dobles |
| 4 | **El `worker` lee Firebase con `google-cloud-storage`**, que lee `STORAGE_EMULATOR_HOST` y `GOOGLE_APPLICATION_CREDENTIALS` como el núcleo |
| 5 | **Qdrant se usa por su API REST**, con `httpx`, y no con `qdrant-client` |
| 6 | **El cliente de Voyage vive en `adapters/embeddings/`**, con un doble determinista. No es uno de los cuatro puertos de ADR-0017: es un proveedor, como dice ADR-0067, regla 1 |
| 7 | **El `worker` sondea cada 5 segundos** y procesa los documentos de a uno, hasta vaciar los pendientes. Un documento en `processing` hace más de 15 minutos vuelve a `pending` (ADR-0064, regla 8) |
| 8 | **La descripción de una imagen entra al final del texto de su página**, como `[Imagen: …]`. `pypdf` no da la posición de la imagen dentro de la página |
| 9 | **Un PDF sin texto extraíble falla antes de describir sus imágenes** (ADR-0064, regla 10). Un escaneo no paga descripciones para terminar en `failed` |
| 10 | **El separador queda pegado al final de la parte anterior**, así que un corte después de `". "` conserva el punto |
| 11 | **El `worker` no recibe `ENCRYPTION_KEY`**, y el brain en modo `api` no recibe `DOCUMENT_ENCRYPTION_KEY`: el compose les vacía la que no les corresponde (ADR-0064, regla 2) |
| 12 | **El brain exige Python `<3.15`**, porque `voyageai` lo exige |

## 3. Justificación

- **`pypdf` porque es la opción sin licencia que condicione el trabajo.** PyMuPDF extrae mejor, pero es AGPL. `pypdf` no necesita binarios en la imagen.
- **Estimar por caracteres porque es lo que decidió el grupo y porque no depende de nada externo.** El tokenizador de Voyage se descarga de Hugging Face la primera vez, lo que hace que el `worker` dependa de un servicio más para arrancar y que los tests necesiten red. Con una equivalencia fija, el corte es determinista y se calcula sin descargar nada.
- **Que la equivalencia esté en `chunking.yaml`** la hace una decisión versionada: cambiarla cambia los cortes, sube la `version` y queda registrada en `chunking_version` (ADR-0065, regla 9).
- **Un Qdrant real porque lo que el `worker` escribe es lo que la búsqueda va a leer.** Un doble de Qdrant probaría un contrato que nadie verificó contra el Qdrant de verdad, y el contenedor es local y no cuesta nada.
- **Voyage y la descripción doblados porque cobran y porque reciben el contenido de los documentos.** El doble de embeddings es una bolsa de palabras normalizada: dos textos que comparten palabras quedan cerca, así que una búsqueda doblada todavía encuentra el trozo que dice lo que se preguntó.
- **`google-cloud-storage` en Python y HTTP directo en Go** porque en cada lenguaje se eligió lo más liviano para lo que hace cada servicio: el núcleo sube y borra, y el `worker` sólo descarga. En Python el cliente oficial no arrastra gRPC, y lee las mismas dos variables sin código propio.
- **REST para Qdrant** porque el `worker` y la búsqueda usan cuatro llamadas: crear la colección, subir puntos, borrar por documento y buscar. `qdrant-client` suma gRPC y protobuf a la imagen para eso.
- **El separador pegado a la parte anterior** porque un trozo que pierde el punto con que termina una oración pierde texto, y la evaluación de ADR-0065 compararía cortes que no dicen lo mismo que el documento.
- **Fallar el escaneo antes de describir** porque la regla 10 de ADR-0064 ya lo va a fallar, y describir sus imágenes sólo agregaría costo.

## 4. Consecuencias

### Positivas

- El *smoke* recorre F06 de punta a punta: la subida, la ingesta, los trozos con sus tres versiones, los puntos en Qdrant y la baja que los saca.
- El `worker` arranca sin red más allá de la base, el almacén, Qdrant y Voyage.
- Un mismo vector cifrado, en `contracts/fixtures/document-cipher.json`, lo abren el test de Go y el de Python: si alguno cambia el formato, falla (ADR-0064 §4).

### Negativas / costos asumidos

- **`token_count` es una estimación**, y los 500 tokens de ADR-0065 también. Cuatro caracteres por token es la equivalencia habitual en inglés, y nada dice que valga para el español: un trozo puede tener bastantes más o menos tokens reales. La evaluación de ADR-0065 es la que puede corregir la equivalencia, y cambiarla obliga a reindexar.
- **`pypdf` extrae peor que PyMuPDF** en tablas, columnas y texto con codificaciones raras. Un documento mal extraído se fragmenta mal, y eso sólo se ve en la evaluación.
- **La descripción no queda junto a la imagen sino al final de la página.** En una página larga, la descripción puede caer en otro trozo que el texto que la rodeaba.
- **El *smoke* no prueba ni Voyage ni la descripción de verdad.** Que `voyage-4` y Sonnet 5.5 respondan como se espera sólo se ve con las claves reales.
- **Dos clientes distintos de Firebase**, uno por lenguaje, cada uno con su forma de autenticarse.
- **El adaptador de Qdrant es código nuestro** contra una API que no versionamos.
- **El brain queda atado a Python 3.14** hasta que `voyageai` acepte 3.15.
- **Un `worker` procesa de a un documento.** Una base recién cargada con muchos PDF se indexa en serie.
- **`voyageai` arrastra dependencias que no se usan**, entre ellas `numpy` y `langchain-text-splitters`, que engordan la imagen del brain.
- **El solapamiento todavía no está implementado.** `chunking.yaml` lo declara en 0 y el cargador rechaza otro valor: subirlo, como permite la regla 4 de ADR-0065, es un cambio de código además de un cambio de datos. *(La regla 4 de ADR-0065 dejó de aplicar por [ADR-0075](0075-medicion-de-oe6-y-evaluacion-de-recuperacion.md): el solapamiento queda en 0.)*

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **PyMuPDF** | Extrae mejor, pero es AGPL |
| **Contar con el tokenizador de Voyage** | Exacto para el modelo, pero lo descarga de Hugging Face y suma un servicio al arranque y a los tests. El grupo eligió la equivalencia |
| **Doblar también Qdrant** | Probaría un contrato que nadie verificó contra el Qdrant real |
| **Voyage real en el *smoke*** | Gasta créditos en cada corrida y manda los documentos de prueba afuera |
| **HTTP directo a Firebase también en Python** | Más código propio para una sola llamada que el cliente oficial ya resuelve |
| **`qdrant-client`** | gRPC y protobuf en la imagen para cuatro llamadas |
| **Describir las imágenes de un escaneo** | Es OCR por otro camino, y ADR-0064 lo dejó afuera |
