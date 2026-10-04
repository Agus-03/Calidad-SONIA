# ADR-0064 — La ingesta de documentos: cifrado en reposo, orden de escritura y formato admitido

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · el cifrado en la aplicación con una llave propia de documentos, los dos órdenes de escritura, sólo PDF y reindexar volviendo a `pending`, elegidos por el grupo el 2026-10-03 · **el modelo de embeddings, la estrategia de chunking y los datos personales dentro de un documento no se deciden acá**: quedan para un ADR propio, después de discutirlos · ***Lo que queda abierto* enmendado**: el chunking pasó a [ADR-0065](0065-fragmentacion-de-documentos.md) y los datos personales los cierra [ADR-0066](0066-datos-personales-en-los-documentos.md) (2026-10-03) · **regla 10 y *Lo que queda abierto* enmendadas por [ADR-0067](0067-modelo-de-embeddings-e-imagenes.md)**: el modelo de embeddings queda fijado y las imágenes se describen al ingerir · **regla 5 y dos consecuencias enmendadas por [ADR-0069](0069-almacen-de-objetos-en-firebase.md)** (2026-10-03): donde dicen Supabase, el proveedor es Cloud Storage for Firebase |
| **Fecha** | 2026-10-03 |
| **Etapa** | Construcción — Fase 12 (conocimiento) |
| **Resuelve** | Los dos pendientes que dejó [ADR-0019](0019-almacenamiento-de-objetos.md) —el cifrado de los documentos y el orden de escritura ante objetos huérfanos—, el orden entre metadatos y vectores que [ADR-0005](0005-base-de-conocimiento.md) y [ADR-0010](0010-qdrant-como-base-vectorial.md) piden decidir, y la decisión O8 de [ADR-0042](0042-pipeline-de-decision-del-turno.md) |
| **Requisitos afectados** | F06 · OE6 · sección 7.3 (Ley 25.326) |
| **Enmienda** | [ADR-0019](0019-almacenamiento-de-objetos.md), **§4** — el orden de escritura y el cifrado de los documentos dejan de estar abiertos (reglas 1 a 3 y 6) · [ADR-0031](0031-cifrado-en-reposo-de-datos-sensibles.md), **regla 9 y *Lo que queda abierto*** — el mecanismo sigue siendo uno, pero hay una segunda llave, y la fila de los documentos de F06 se cierra · [ADR-0042](0042-pipeline-de-decision-del-turno.md), **O8** — no hay un job de reindexado: reindexar es volver los documentos a `pending` (regla 11) |
| **Relacionada con** | [ADR-0005](0005-base-de-conocimiento.md), [ADR-0010](0010-qdrant-como-base-vectorial.md), [ADR-0013](0013-fuente-de-verdad-del-modelo.md), [ADR-0017](0017-decomposicion-en-despliegues-y-lenguajes.md), [ADR-0018](0018-proveedor-de-llm-y-embeddings.md), [ADR-0027](0027-nucleo-de-instancia-unica.md) |

---

## 1. Contexto

F06 pide *"ingesta de documentos del negocio (límite 100 MB por agente)"* y *"búsqueda semántica para enriquecer respuestas"*. No dice qué formatos, ni cómo se protegen los documentos, ni en qué orden se escriben.

Al empezar la Fase 12, del lado de F06 sólo existe el esquema: `knowledge_bases`, `documents`, `document_chunks` y `retrieval_logs` están en `0001`, y ni el núcleo ni el cerebro tienen código que las use. Cuatro decisiones previas dejaron puntos abiertos que hay que cerrar antes de construir:

| Dónde | Qué quedó abierto |
|---|---|
| [ADR-0019](0019-almacenamiento-de-objetos.md) §4 | El orden de escritura entre el objeto y la fila, y qué se hace con los huérfanos. Y *"el cifrado de los documentos sigue sin decidirse"* |
| [ADR-0031](0031-cifrado-en-reposo-de-datos-sensibles.md), *Lo que queda abierto* | *"El cifrado de los documentos de F06 (…) son bytes en un almacén de objetos, no una columna: otro problema, misma ley"* |
| [ADR-0005](0005-base-de-conocimiento.md) y [ADR-0010](0010-qdrant-como-base-vectorial.md) | *"La ingesta tiene que manejarlo explícitamente y decidir el orden de escritura"* entre PostgreSQL y Qdrant |
| [ADR-0042](0042-pipeline-de-decision-del-turno.md), O8 | Un job de indexación, *"sólo para reindexar por cambio de modelo de embeddings"* |

Dos hechos condicionan el cifrado y conviene tenerlos delante:

- **El texto del documento termina en claro en otros dos lugares.** `document_chunks.content` es `text` en PostgreSQL, y el texto de cada fragmento viaja al proveedor de embeddings para vectorizarse. Cifrar el archivo protege **la copia que vive en el almacén de objetos**, no el contenido en todo el sistema.
- **El que lee los bytes es el cerebro.** El `worker` baja el archivo para fragmentarlo ([ADR-0019](0019-almacenamiento-de-objetos.md) §2), y la regla 8 de [ADR-0031](0031-cifrado-en-reposo-de-datos-sensibles.md) dice que `ENCRYPTION_KEY` la tiene únicamente el núcleo.

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **Los documentos se cifran en la aplicación, con AES-256-GCM** y el mismo formato de [ADR-0031](0031-cifrado-en-reposo-de-datos-sensibles.md) (reglas 2 a 5): `version (1 byte) ‖ nonce (12 bytes) ‖ ciphertext ‖ tag (16 bytes)`, con nonce aleatorio en cada escritura. Se cifra el archivo entero antes de subirlo |
| 2 | **Con una llave propia, `DOCUMENT_ENCRYPTION_KEY`**, distinta de `ENCRYPTION_KEY`: 32 bytes en base64, en el entorno. La tienen **el núcleo**, que cifra al recibir la subida, y **el cerebro en modo `worker`**, que descifra al leer. El cerebro en modo `api` no la recibe. Se valida al arrancar en los dos procesos que la usan: si falta o no mide 32 bytes, no levantan |
| 3 | **El cifrado cubre la copia en el almacén de objetos y nada más.** `document_chunks.content` queda en claro en PostgreSQL —como `messages.content_text`, regla 10 de ADR-0031—, y el texto de cada fragmento viaja al proveedor de embeddings. **Qdrant no guarda texto:** el *payload* de cada punto lleva sólo `document_id`, y el texto se lee de `document_chunks` al recuperar |
| 4 | **`documents.checksum` es el SHA-256 del PDF en claro**, calculado por el núcleo antes de cifrar. El catálogo le asigna la detección de duplicados, y con un nonce aleatorio el hash del archivo cifrado sería distinto en cada subida del mismo documento |
| 5 | **Los identificadores los pone la aplicación, también los de afuera.** El núcleo genera `documents.id` antes de subir (C04, motivo 1), y la clave del objeto se deriva de él: `knowledge-bases/<knowledge_base_id>/<document_id>`. **El id de cada punto de Qdrant es `document_chunks.id`**, y `vector_id` lo guarda: Qdrant no genera ids, sino que exige que el cliente mande un entero o un UUID, así que no hay excepción a C04. Supabase *(Firebase, desde [ADR-0069](0069-almacen-de-objetos-en-firebase.md))* guarda un id interno propio de cada objeto, que el sistema no lee ni guarda: la referencia es la clave. La colección de un agente IA se llama `kb_<ai_agent_id>`, y la escribe el núcleo en `knowledge_bases.collection_name` al crear la base con la primera subida |
| 6 | **En el núcleo, el objeto se escribe antes que la fila.** (a) Valida el formato (regla 10) y que la base no pase de 100 MB con el archivo nuevo. (b) Genera el id, cifra y sube. (c) En una transacción, bloquea la fila de `knowledge_bases`, vuelve a verificar el límite, inserta el documento en `pending` y suma `size_bytes_total`. (d) Si la transacción falla, intenta una vez borrar el objeto; si ese borrado también falla, el huérfano queda y se registra en el log con su clave. **No hay barrido de huérfanos** |
| 7 | **En el `worker`, PostgreSQL antes que Qdrant, y lo viejo antes que lo nuevo.** Toma el documento con `FOR UPDATE SKIP LOCKED` y lo pasa a `processing` con `locked_at`. Fuera de toda transacción, baja, descifra, extrae el texto, fragmenta y vectoriza. Después: **(i)** borra de Qdrant los puntos previos del documento, por `document_id`; **(ii)** en una transacción, marca los chunks previos con `vector_id = NULL` y `deleted_at`, e inserta los nuevos; **(iii)** sube los puntos nuevos; **(iv)** marca el documento `indexed` y limpia `locked_at` y `error_message`. Cualquier falla lo deja en `failed`, con el motivo en `error_message` y `locked_at` limpio. El orden de (i) y (ii) es el que [DER/entidades.md](../DER/entidades.md) ya fija para dar de baja un chunk |
| 8 | **Un documento colgado vuelve solo.** El `worker` devuelve a `pending` todo documento en `processing` cuyo `locked_at` sea más viejo que un timeout de la configuración |
| 9 | **Dar de baja un documento es lógico (C09).** El núcleo pone `documents.deleted_at` y resta su tamaño de `size_bytes_total`. El `worker`, en el mismo barrido de la regla 8, borra de Qdrant los puntos de los documentos dados de baja que todavía tienen chunks vivos, y después marca esos chunks con el orden de la regla 7. El objeto se queda en el almacén |
| 10 | **Sólo se admite PDF.** El núcleo rechaza todo lo que no declare `application/pdf` o no empiece con la firma `%PDF-`. **Sin OCR:** un PDF sin texto extraíble termina en `failed` con ese motivo. ~~Qué se hace con las imágenes dentro de un PDF se decide junto con el modelo de embeddings~~ *(Enmendada por [ADR-0067](0067-modelo-de-embeddings-e-imagenes.md): cada imagen se describe al ingerir y la descripción entra al texto de su página.)* |
| 11 | **Reindexar no es un job: es volver a `pending`** los documentos vivos de una base, y el `worker` los reprocesa con la regla 7. No tiene pantalla: es una operación de despliegue, que se hace cuando cambia el modelo de embeddings. Si el modelo nuevo tiene otra dimensión, la colección se recrea antes, porque la regla 7 no la recrea sola |

## 3. Justificación

- **Cifrado en la aplicación y no sólo el del proveedor (reglas 1 a 3).** El riesgo que [ADR-0019](0019-almacenamiento-de-objetos.md) nombra es que *"los documentos del cooperante salen de nuestra infraestructura"*. El cifrado en reposo del proveedor deja la llave en manos del proveedor, así que no cubre ese riesgo. Reusar el formato de ADR-0031 evita inventar un segundo formato.
- **Una llave propia y no `ENCRYPTION_KEY` (regla 2).** El `worker` tiene que descifrar, y la regla 8 de ADR-0031 se sostiene porque el cerebro no lee ninguna de las siete columnas cifradas. Darle `ENCRYPTION_KEY` le abriría el DNI, el CBU y las credenciales de Meta sin ningún caso de uso. **Esto choca con una alternativa que ADR-0031 descartó**, *"llaves distintas por tabla"*, por multiplicar la custodia sin comprar nada. Acá sí compra algo: que la regla 8 siga en pie.
- **El objeto antes que la fila (regla 6).** Con la fila primero, el `worker` podría tomar un documento `pending` cuyos bytes todavía no existen, o haría falta un estado nuevo en `DocumentStatus` para la subida en curso. Con el objeto primero, lo peor que pasa es un huérfano que nadie referencia. **Verificar el límite dos veces**, la segunda con la base bloqueada, es lo que impide que dos subidas simultáneas pasen juntas los 100 MB.
- **Sin barrido de huérfanos (regla 6).** Un huérfano sólo aparece si la transacción falla después de una subida exitosa y el borrado de compensación también falla. Con el núcleo de instancia única ([ADR-0027](0027-nucleo-de-instancia-unica.md)) y una cooperante, es raro, y detectable comparando las claves del almacén con las filas.
- **PostgreSQL antes que Qdrant (regla 7).** La recuperación va de Qdrant a PostgreSQL: busca puntos y lee el texto de los chunks vivos. Un punto sin fila viva se descarta, y una fila sin punto no aparece en ninguna búsqueda. Con este orden, **ninguna falla a mitad de camino devuelve un texto equivocado**: a lo sumo, un documento deja de encontrarse hasta el próximo intento.
- **Qdrant sin texto (regla 3).** Es una copia en claro menos fuera de PostgreSQL, y una consulta más por turno, por ids, para leer los textos.
- **Sólo PDF (regla 10).** El anteproyecto no fija formatos, así que acotar es una decisión del grupo. Acota la extracción a una sola biblioteca y a un solo conjunto de fallas que probar.
- **Reindexar volviendo a `pending` (regla 11).** La cola ya existe ([ADR-0017](0017-decomposicion-en-despliegues-y-lenguajes.md) (g)), y un job aparte sería un segundo camino para hacer lo mismo, con su propio estado.

## 4. Consecuencias

### Positivas

- Los dos pendientes de ADR-0019 quedan cerrados, y el de ADR-0031 también.
- Un volcado del almacén de objetos, o una cuenta de Supabase *(Firebase, desde [ADR-0069](0069-almacen-de-objetos-en-firebase.md))* comprometida, no alcanza para leer los documentos del cooperante.
- La regla 8 de ADR-0031 sigue en pie: el cerebro no puede descifrar ningún dato personal estructurado.
- Ninguna falla parcial de la ingesta hace que el AIAgent cite un texto que no corresponde.
- Reindexar no agrega código: es la misma ingesta.

### Negativas / costos asumidos

- **Un segundo secreto que custodiar y respaldar.** Si se pierde `DOCUMENT_ENCRYPTION_KEY`, los documentos del almacén quedan ilegibles: los chunks ya indexados siguen sirviendo, pero no se puede reindexar. Entra al mismo procedimiento de respaldo que ADR-0031 dejó sin escribir. La rotación tampoco está procedimentada, aunque el byte de versión la permite.
- **El mismo formato, en dos lenguajes.** El núcleo cifra en Go y el `worker` descifra en Python, así que hace falta un test que cruce los dos, con un archivo cifrado por Go y descifrado por Python. Suma además una dependencia al cerebro para AES-GCM.
- **100 MB en memoria.** El núcleo cifra el archivo entero y el `worker` lo descifra entero, así que un documento grande ocupa del orden del doble de su tamaño mientras se procesa. Con una sola instancia del núcleo, dos subidas simultáneas suman.
- **El cifrado cubre una sola copia.** El texto queda en claro en `document_chunks.content` y viaja al proveedor de embeddings. Tiene que aparecer en el DFD del Paso 12, igual que `messages`.
- **Puede quedar un objeto huérfano** sin que nada lo barra.
- **Mientras se reindexa, el documento no se encuentra**: entre los pasos (i) y (iii) de la regla 7 no tiene vectores.
- **Un documento que tumba el proceso** —por memoria, por ejemplo— vuelve a `pending` por la regla 8 y lo vuelve a tumbar, sin límite: no hay un contador de intentos, porque no hay columna para guardarlo. Se ve en el log, no en la base.
- **La baja deja los bytes en el proveedor.** El borrado es lógico (C09), así que el PDF de un documento dado de baja sigue en Supabase *(Firebase, desde [ADR-0069](0069-almacen-de-objetos-en-firebase.md))* hasta que alguien lo borre. No hay purga decidida.
- **La llave depende del modo de arranque.** Es la misma imagen para `api` y `worker`, así que el compose tiene que pasársela sólo al `worker`.
- **Quien tenga documentos en otro formato los tiene que convertir a PDF.**

### Lo que queda abierto

| Qué | Dónde se decide |
|---|---|
| ~~**El modelo de embeddings** y su dimensión, y qué se hace con las imágenes de un PDF. [ADR-0018](0018-proveedor-de-llm-y-embeddings.md) lo dejó abierto, y la colección de Qdrant no se puede crear sin él~~ | ~~Un ADR propio de esta fase, después de discutirlo~~ *(Cerrado por [ADR-0067](0067-modelo-de-embeddings-e-imagenes.md).)* |
| **La estrategia de chunking**: unidad, tamaño, solapamiento y dónde se versionan los parámetros | ~~Ídem~~ [ADR-0065](0065-fragmentacion-de-documentos.md), ~~en *Propuesta*~~ Aceptada |
| ~~**Los datos personales dentro de un documento**, que llegan al proveedor de embeddings y al prompt, y que el guardrail de salida rechaza (D3 de [ADR-0042](0042-pipeline-de-decision-del-turno.md))~~ | ~~Ídem~~ *(Cerrado por [ADR-0066](0066-datos-personales-en-los-documentos.md): se avisa y se acepta al subir, y no se redactan.)* |
| **Qué pasa con la colección de un agente IA dado de baja.** [ADR-0010](0010-qdrant-como-base-vectorial.md) dice que *"dar de baja un agente es borrar su colección"*, y la baja de [ADR-0038](0038-alta-baja-y-pausa-del-agente-ia.md) es lógica | Sin fecha |

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Sólo el cifrado en reposo del proveedor**, con el bucket privado | La llave queda en el proveedor, que es justo de quien ADR-0019 dice que hay que cuidarse |
| **Darle `ENCRYPTION_KEY` al cerebro** | Enmienda la regla 8 de ADR-0031 y le abre al cerebro los datos personales y las credenciales de Meta, sin necesitarlos |
| **Que el núcleo descifre y le sirva los bytes al `worker`** | Exige que el cerebro llame al núcleo, y la autenticación de [ADR-0017](0017-decomposicion-en-despliegues-y-lenguajes.md) (k) es unidireccional a propósito |
| **La fila antes que el objeto** | El `worker` podría tomar un documento sin bytes, o haría falta un estado nuevo para la subida en curso |
| **Un barrido periódico de huérfanos** | Código y una tarea periódica para un caso raro y detectable a mano |
| **Qdrant antes que PostgreSQL** | Igual de seguro para la recuperación, pero el punto se crearía con el id de una fila que todavía no existe |
| **Guardar el texto en el *payload* de Qdrant** | Ahorra una consulta por turno a cambio de una copia más del contenido en claro, fuera de la base que se respalda |
| **Un job de reindexado (O8 tal como estaba)** | Un segundo camino para lo que la cola ya hace |
| **Cifrado por bloques, en *streaming*** | Evitaría tener 100 MB en memoria, pero es un formato propio y más código que probar en dos lenguajes. No se justifica con una cooperante |
