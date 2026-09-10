# ADR-0031 — Cifrado en reposo: AES-256-GCM en la aplicación, llave del entorno

| Campo | Valor |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 2026-09-09 |
| **Etapa** | Construcción — Fase 3 (contactos y etiquetas) |
| **Resuelve** | Con qué se cifran las siete columnas `bytea` que el esquema declara cifradas y nunca dijo cómo |
| **Requisitos afectados** | F31 · sección 7.3 (Ley 25.326) |
| **Relacionada con** | [ADR-0006](0006-anonimizacion-de-pii.md), [ADR-0012](0012-cuenta-y-bandejas-de-entrada.md), [ADR-0017](0017-decomposicion-en-despliegues-y-lenguajes.md), [ADR-0030](0030-autenticacion-de-operadores.md) |

---

## 1. Contexto

La sección 7.3 se compromete a *"almacenamiento encriptado para datos sensibles (DNI, CUIL, CBU) según Ley 25.326"*, y la migración `0001_initial_schema.sql` lo tomó en serio: hay **siete columnas `bytea`** cuyo nombre termina en `_encrypted`.

| Tabla | Columnas | Qué guardan |
|---|---|---|
| `contacts` | `dni_encrypted`, `cuit_encrypted`, `cuil_encrypted`, `cbu_encrypted` | Los cuatro datos que nombra ADR-0006 |
| `contact_attributes` | `value_encrypted` | Lo que el clasificador de extracción saca de la conversación (F31) |
| `inboxes` | `access_token_encrypted`, `app_secret_encrypted` | Las credenciales de Meta de cada número (ADR-0012) |

**Ningún documento dice con qué se cifran.** El nombre de la columna afirma una propiedad que hoy no existe en ninguna parte del sistema: no hay algoritmo elegido, ni llave, ni formato del texto cifrado, ni quién tiene permiso de descifrar.

[ADR-0006](0006-anonimizacion-de-pii.md) **no cubre esto y lo dice explícitamente**: resuelve el reemplazo de PII por marcadores *antes de que el texto salga hacia el proveedor de LLM*, y en su §3 separa las dos cosas — *"cifrar no es anonimizar (…) esto protege el dato en tránsito hacia un tercero, que es un problema distinto"*. El problema de acá es el otro: **el dato quieto en la base**.

La Fase 3 es la que construye `contacts/`, así que es la primera que tiene que escribir en cuatro de esas columnas. La decisión no se puede seguir difiriendo: un `INSERT` necesita saber qué bytes pone.

## 2. Decisión

**Se cifra en la aplicación, con AES-256-GCM, con una llave que sale del entorno y se valida al arrancar.**

| # | Regla |
|---|---|
| 1 | **Algoritmo: AES-256-GCM**, de la biblioteca estándar de Go (`crypto/aes` + `crypto/cipher`). Sin dependencia nueva |
| 2 | **Es cifrado autenticado.** Un texto cifrado alterado en la base **falla al descifrar**, no devuelve basura: la etiqueta de autenticación de GCM lo detecta |
| 3 | **Formato guardado en la columna `bytea`:** `version (1 byte) ‖ nonce (12 bytes) ‖ ciphertext ‖ tag (16 bytes)`. Todo en la misma columna, sin tablas ni campos auxiliares |
| 4 | **El nonce es aleatorio y distinto en cada escritura**, de `crypto/rand`. Reutilizar un nonce con la misma llave rompe GCM por completo, así que nunca se deriva del `id` ni de un contador |
| 5 | **El byte de versión vale `0x01` y existe para la rotación de llave**: permite que convivan filas cifradas con llaves distintas sin migrar la tabla entera de una vez. Hoy sólo hay una versión |
| 6 | **La llave sale de `ENCRYPTION_KEY`**, en base64, y son exactamente **32 bytes crudos** al decodificar. Vive en el entorno, nunca en un archivo del repositorio |
| 7 | **Se valida al arrancar** (regla 10 de `CLAUDE.md`): si falta, si no decodifica de base64 o si no mide 32 bytes, **el proceso no levanta** — igual que `JWT_SECRET` en [ADR-0030](0030-autenticacion-de-operadores.md) |
| 8 | **La llave la tiene únicamente el núcleo (Go). El cerebro no la recibe y no descifra nada** |
| 9 | **Alcance: las siete columnas de §1, con el mismo mecanismo.** Un solo camino de cifrado en el sistema, no uno para PII y otro para credenciales |
| 10 | **`messages.content_text` NO se cifra**, ni ninguna otra columna de texto. Lo que se cifra es lo que el sistema guarda **como dato estructurado del contacto**, no lo que el cliente escribió en el chat |
| 11 | **El código vive en `platform/encryption`**, un solo lugar con dos funciones —cifrar y descifrar—, y ningún paquete de contexto implementa lo suyo |

### Sobre la regla 8 — por qué el cerebro no tiene la llave

No es una restricción que haya que sostener con disciplina: **el reparto de escrituras ya la hace innecesaria.** La regla 3 de `CLAUDE.md` establece que el cerebro escribe **únicamente tablas de evidencia**, y `contact_attributes` está nombrada explícitamente entre las que **no** puede tocar. El clasificador de extracción (F31) **devuelve** el valor extraído; el núcleo lo cifra y lo persiste.

Ninguna de las siete columnas está en el conjunto que el cerebro escribe ni en el que necesita leer. Darle la llave sería ampliar la superficie de exposición **sin ningún caso de uso que la justifique**.

### Sobre la regla 10 — dónde termina el cifrado

Es la regla que evita una expectativa falsa. Si un cliente escribe *"mi CBU es 0170099220000067797151"* en el chat, **ese número queda en claro en `messages.content_text`**, y lo ve cualquiera que pueda ver esa conversación.

Cifrar los mensajes rompería tres cosas que el sistema necesita: la búsqueda en la bandeja, el historial que lee el operador y el propio armado del prompt. Y no compraría casi nada, porque el núcleo tendría que descifrarlos en cada turno de todos modos. **Lo que protege el texto del chat es [ADR-0006](0006-anonimizacion-de-pii.md) en tránsito hacia el LLM, y el control de acceso adentro** — no el cifrado en reposo.

## 3. Justificación

- **En la aplicación y no en la base, porque la llave no debe estar donde están los datos.** Con `pgcrypto` la llave viaja en la sentencia SQL: aparece en `pg_stat_activity`, en el log de consultas lentas y en cualquier volcado de tráfico contra la base. Un volcado robado de la base traería consigo la forma de leerlo, que es exactamente el ataque contra el que 7.3 se protege.
- **AES-256-GCM porque es cifrado autenticado, y la alternativa silenciosa es peor.** Un modo sin autenticación —CBC, CTR— devuelve bytes plausibles ante un texto cifrado corrompido. Para un CBU, "devolver un número distinto" es un resultado mucho peor que "fallar".
- **De la biblioteca estándar, sin dependencia nueva.** Es criptografía de la que hay que poder decir en la defensa que no se escribió a mano: la decisión es *elegir* el modo y el formato, no implementarlos.
- **La llave en el entorno, con el mismo trato que `JWT_SECRET`.** El precedente ya existe y funciona: variable de entorno, validada al arrancar, el proceso no levanta si falta. No se inventa un mecanismo paralelo para el segundo secreto del sistema.
- **Un solo mecanismo para las siete columnas.** Las credenciales de Meta no son datos personales, pero el problema técnico es idéntico y ya tienen la columna `bytea` esperando. Dos mecanismos serían dos superficies para auditar, dos formatos que documentar y una segunda oportunidad de equivocarse.
- **El byte de versión ahora, porque después cuesta caro.** Cuesta un byte por fila y es lo único que hace posible rotar la llave sin detener el sistema. Agregarlo cuando ya hay filas obliga a distinguir las viejas de las nuevas por adivinanza.

## 4. Consecuencias

### Positivas

- El compromiso de 7.3 pasa de ser un nombre de columna a ser un mecanismo verificable, con un test que lo prueba.
- Un volcado de la base **no alcanza** para leer los cuatro datos personales ni las credenciales de Meta.
- El texto cifrado alterado se detecta, en vez de degradar en un valor incorrecto que nadie nota.
- La rotación de llave queda **posible** sin migración masiva, aunque todavía no esté procedimentada.
- La regla 10 deja escrito el límite, así que nadie va a leer "almacenamiento encriptado" y suponer que incluye las conversaciones.

### Negativas / costos asumidos

- **No se puede buscar ni indexar por esos campos, nunca.** El nonce aleatorio hace que el mismo DNI produzca bytes distintos en cada fila, así que `WHERE dni_encrypted = $1` no existe como consulta, y un índice sobre esa columna no sirve para nada. **Buscar un contacto por DNI queda descartado**, y ninguna funcionalidad puede apoyarse en eso más adelante sin volver sobre esta decisión.
- **Tampoco se puede detectar un DNI duplicado.** Si el mismo documento se carga en dos contactos, la base no tiene cómo verlo. Es el mismo costo que el anterior, con otra cara.
- **La llave perdida es dato perdido.** No hay recuperación: no es una contraseña que se pueda resetear. Un despliegue nuevo con una `ENCRYPTION_KEY` distinta deja las filas existentes ilegibles, y como GCM autentica, **fallan al descifrar en vez de devolver basura** — visible, pero irreversible. Esto **obliga a que la llave entre en el procedimiento de respaldo**, y hoy ese procedimiento no está escrito.
- **Costo por lectura.** Cada ficha de contacto que muestre los cuatro campos hace cuatro descifrados. Es despreciable para AES —hay aceleración por hardware— pero deja de serlo si alguna vez se descifra un listado entero fila por fila. **Un listado de contactos no debe descifrar nada**, y por [ADR-0032](0032-acceso-a-contactos-y-datos-sensibles.md) tampoco tiene por qué.
- **Las cuatro columnas de `contacts` son `NULL`-ables y las otras tres no.** El código tiene que tratar los dos casos, y "sin dato" nunca puede confundirse con "cifrado vacío".
- **La sombra de la regla 10 es real.** El dato sensible que el cliente escribió en el chat **no está cifrado**. Es una decisión, no un olvido, y **tiene que aparecer en el informe** cuando se hable de la Ley 25.326: el DFD del Paso 12 debe mostrar `messages` como almacenamiento en claro.

### Lo que queda abierto

| Qué | Por qué no se resuelve acá |
|---|---|
| **Procedimiento de rotación de la llave** | El formato lo soporta (regla 5) y nada más. Falta decidir si se re-cifra en caliente o por lote, y quién dispara la operación. Ningún requisito lo pide y no hay fase asignada |
| **Dónde se custodia la llave en producción** | Hoy es una variable de entorno, como `JWT_SECRET`. Si el despliegue termina teniendo un gestor de secretos, cambia el origen y no el mecanismo |
| **El respaldo de la llave** | Consecuencia directa del punto anterior: sin llave no hay datos, y no hay procedimiento escrito |
| **El cifrado de los documentos de F06** | Sigue abierto desde [ADR-0019](0019-almacenamiento-de-objetos.md). Son bytes en un almacén de objetos, no una columna: otro problema, misma ley |
| **Qué es un atributo sensible en `contact_attributes`** | La columna es `NOT NULL`, así que **todo** lo que extrae F31 se cifra, incluso lo que no es dato personal. Se cifra de más, que es el lado seguro del error. Definir el criterio es de las Fases 8 y 9, cuando exista el clasificador de extracción |
| **Registro de quién descifró qué** | Ver [ADR-0032](0032-acceso-a-contactos-y-datos-sensibles.md) §4 y [ADR-0007](0007-alcance-del-registro-de-auditoria.md). El cifrado protege de quien no debería entrar; no dice nada de quien entró con permiso |

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **`pgcrypto` (`pgp_sym_encrypt` en la base)** | La llave viaja en cada sentencia SQL: queda en `pg_stat_activity`, en el log de consultas y en el tráfico. Un volcado de la base más un log llevaría la llave adentro. Además ata el cifrado a PostgreSQL, y el descifrado pasaría a consumir CPU de la base, que es el recurso que menos escala |
| **Cifrado de disco o TDE** | Protege ante el robo del disco físico y **nada más**: con la base levantada, cualquier `SELECT` devuelve texto en claro. No cubre el escenario realista, que es un volcado o una credencial de base filtrada |
| **Un gestor de secretos (Vault, KMS, Secrets Manager)** | Es mejor custodia de la llave, y no es incompatible: cambia de dónde sale `ENCRYPTION_KEY`, no cómo se cifra. Se descarta **ahora** porque agrega un componente de infraestructura que 7.3 no contempla y que el cooperante tendría que operar |
| **Cifrado determinístico (AES-SIV) para poder buscar por DNI** | Permitiría `WHERE dni_encrypted = $1` y detectar duplicados. Se descarta porque **filtra igualdad**: dos filas con el mismo texto cifrado revelan que tienen el mismo DNI sin necesidad de la llave, y sobre un padrón chico eso alcanza para inferir bastante. Ningún requisito pide buscar por DNI, así que se paga una propiedad de seguridad por una funcionalidad que nadie pidió |
| **Cifrar también `messages.content_text`** | Rompe la búsqueda en la bandeja, el historial y el armado del prompt, y no compra casi nada: el núcleo descifraría en cada turno de todos modos. Ver la regla 10 |
| **Llaves distintas por tabla o por cuenta** | Reduce el impacto de una llave filtrada, pero multiplica el problema no resuelto —custodia, respaldo, rotación— por la cantidad de llaves. Con el núcleo de instancia única de [ADR-0027](0027-nucleo-de-instancia-unica.md) y una sola cuenta ([ADR-0012](0012-cuenta-y-bandejas-de-entrada.md)), no compra nada real hoy |
