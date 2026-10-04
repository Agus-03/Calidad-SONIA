# ADR-0070 — La subida de documentos: duplicados, memoria y el almacén en las pruebas

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · rechazar el duplicado, cifrar el archivo entero en memoria y probar contra un `fake-gcs-server`, decididos por el grupo el 2026-10-03 · las reglas 4 a 9 son decisiones de construcción, tomadas al escribir `documents/` |
| **Fecha** | 2026-10-03 |
| **Etapa** | Construcción — Fase 12 (conocimiento) |
| **Resuelve** | Tres puntos que [ADR-0064](0064-ingesta-de-documentos.md) y [ADR-0069](0069-almacen-de-objetos-en-firebase.md) dejaron sin decir: qué se hace con un PDF repetido, cuánta memoria cuesta cifrar uno, y cómo se prueba el almacén sin salir a Google |
| **Requisitos afectados** | F06 |
| **Relacionada con** | [ADR-0028](0028-cobertura-de-pruebas-por-servicio.md), [ADR-0031](0031-cifrado-en-reposo-de-datos-sensibles.md), [ADR-0064](0064-ingesta-de-documentos.md), [ADR-0066](0066-datos-personales-en-los-documentos.md), [ADR-0067](0067-modelo-de-embeddings-e-imagenes.md), [ADR-0069](0069-almacen-de-objetos-en-firebase.md) |

---

## 1. Contexto

[ADR-0064](0064-ingesta-de-documentos.md) fijó el orden de la subida y el cifrado, y [ADR-0069](0069-almacen-de-objetos-en-firebase.md) el proveedor. Al construir `documents/` aparecieron tres preguntas que ninguno responde:

- **El duplicado.** El catálogo le asigna a `documents.checksum` detectar duplicados (ADR-0064, regla 4), pero no dice qué pasa con uno.
- **La memoria.** AES-GCM cifra el archivo entero de una vez y en el formato de ADR-0031, así que el núcleo tiene el PDF y su cifrado en memoria al mismo tiempo.
- **Las pruebas.** El *smoke* no puede escribir en el Firebase real, y ADR-0069 dejó para este momento cómo se dobla el almacén.

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **Un PDF que ya está vivo en la misma base se rechaza con un `409`** (`document_duplicate`). El duplicado es el mismo SHA-256, sin importar el nombre del archivo. Un documento dado de baja libera su *checksum*, así que el mismo archivo se puede volver a subir |
| 2 | **El archivo se cifra entero en memoria**, en el formato de ADR-0031. Una subida de 100 MB ocupa unos 200 MB de RAM del núcleo mientras dura |
| 3 | **Las pruebas usan un `fake-gcs-server`** en el compose del *smoke*, apuntado por `STORAGE_EMULATOR_HOST`, que es la variable que leen los clientes de Google. Corre el adaptador real, sin dobles en el código de producción. **La configuración rechaza el emulador fuera de `development` y `test`**, como a los otros dobles |
| 4 | **El adaptador habla la API JSON de Cloud Storage por HTTP**, con `golang.org/x/oauth2` para la cuenta de servicio, y no con el SDK de Firebase ni con el cliente oficial de Cloud Storage |
| 5 | **`DOCUMENT_ENCRYPTION_KEY` tiene que ser distinta de `ENCRYPTION_KEY`**: si son iguales, el núcleo no arranca |
| 6 | **El límite de 100 MB es por base y en megabytes binarios** (100 × 2²⁰ bytes). Pasarlo responde `413` (`knowledge_base_full`), tanto si lo pasa el archivo solo como si lo pasa la suma con lo que ya hay |
| 7 | **El duplicado y el límite se controlan en la aplicación, bajo el bloqueo de la fila de `knowledge_bases`**, antes de subir y otra vez dentro de la transacción. No hay índice único sobre el *checksum* |
| 8 | **La base se crea antes de subir el objeto**, porque la clave del objeto lleva su id (ADR-0064, regla 5). Una subida que falla después deja una base vacía |
| 9 | **El nombre del archivo se valida antes de subir**: UTF-8 válido y hasta 255 caracteres, sin caracteres de control ni la ruta que el navegador pueda mandar. Si no cumple, responde `400` (`invalid_filename`) |

## 3. Justificación

- **Rechazar el duplicado porque ingerirlo dos veces repite cada trozo.** La búsqueda devolvería pares iguales, que ocupan dos lugares del *top-k* de [ADR-0068](0068-recuperacion-y-grounding.md) con una sola información. Es el error que el *checksum* existe para evitar, y el admin entiende un `409` mejor que trozos repetidos que no ve.
- **Liberar el *checksum* al dar de baja** porque la baja es la forma de corregir un documento: el admin borra el viejo y sube el nuevo, o sube otra vez el mismo después de borrarlo por error.
- **Cifrar entero porque cifrar por bloques cambia el formato de ADR-0031**, que hoy comparten las columnas y los documentos, y obliga al `worker` a leer dos formatos. Las subidas son esporádicas, de a una y las hace el admin: el pico de memoria es raro y acotado.
- **Un emulador y no un doble en memoria** porque así se ejecuta el adaptador que va a producción: la URL, el escape de la clave, el cuerpo y los códigos de estado. Un doble activado por una variable probaría el resto y dejaría sin ejecutar justo la parte que habla con Google.
- **HTTP directo y no un SDK** porque el núcleo sólo sube y borra, dos llamadas. El cliente oficial de Cloud Storage arrastra gRPC y telemetría a la imagen para eso, y el SDK de Firebase lo envuelve. `x/oauth2` suma un solo módulo directo y uno indirecto.
- **Dos llaves distintas por la regla 2 de ADR-0064**: el `worker` tiene la de documentos y no debe poder leer el DNI. Si las dos fueran iguales, esa regla se cumpliría en la configuración y no en los hechos.
- **El control en la aplicación** porque el bloqueo de la base ya serializa todas las subidas y bajas de esa base (ADR-0064, regla 6), y con eso alcanza. Un índice único sobre `(knowledge_base_id, checksum)` parcial por `deleted_at` diría lo mismo con una línea de DDL más.

## 4. Consecuencias

### Positivas

- El *smoke* prueba la subida de punta a punta: la fila en `pending`, y en el almacén sólo un cifrado que la llave de documentos vuelve a abrir.
- Un archivo que no entra o que ya está no consume una transferencia: se rechaza antes de subir.
- La imagen del núcleo no suma gRPC ni el SDK de Google.

### Negativas / costos asumidos

- **Unos 200 MB de RAM por subida de 100 MB**, y dos subidas grandes simultáneas los duplican. Nada limita cuántas subidas corren a la vez.
- **Un PDF re-exportado no es un duplicado.** El mismo contenido guardado otra vez cambia los bytes y el *checksum*, y entra dos veces. La regla 1 atrapa el mismo archivo, no el mismo documento.
- **El emulador no es Firebase.** No prueba la cuenta de servicio contra Google, ni las reglas de seguridad de ADR-0069, ni los límites reales. El intercambio del token se prueba contra un servidor local en los tests unitarios, y nada más.
- **El adaptador es código nuestro contra una API de Google**, sin el SDK que la sigue. Si la API cambia, el arreglo es nuestro.
- **Sin índice único, la regla 1 depende de que toda escritura pase por el servicio.** Un `INSERT` a mano en `documents` puede dejar un duplicado.
- **Un nombre de más de 255 caracteres se rechaza** aunque el archivo sea válido: el admin tiene que renombrarlo antes de subirlo.
- **Una base vacía sobrevive a una subida fallida**, con `size_bytes_total = 0`. Es la base que la próxima subida habría creado igual.
- **El emulador agrega un servicio al compose del *smoke*** y un puerto, el 4443, en el *loopback*.
- **El núcleo no arranca sin `DOCUMENT_ENCRYPTION_KEY`, `FIREBASE_STORAGE_BUCKET` y la credencial**, también para `--seed-admin` y `--connect-inbox`, que no usan el almacén.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Aceptar el duplicado** | Se ingiere dos veces y la búsqueda devuelve trozos repetidos |
| **Reemplazar el documento viejo por el nuevo** | Cambia en silencio un documento que el admin no pidió tocar, y la baja ya sirve para eso |
| **Cifrar por bloques, en *streaming*** | Cambia el formato de ADR-0031 y obliga al `worker` a leer dos |
| **Un doble del almacén en memoria, activado por variable** | No ejecuta nunca el adaptador real, que es lo que más puede fallar |
| **El emulador oficial de Firebase** | Necesita Node y Java, que el stack no tiene |
| **El cliente oficial de Cloud Storage o el SDK de Firebase** | Mucho peso en la imagen para dos llamadas |
| **Un índice único sobre el *checksum*** | Duplica en DDL lo que el bloqueo ya garantiza |
