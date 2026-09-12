# ADR-0033 — Los secretos de las bandejas admiten NULL, y `0001` se congela con la primera WABA real

| Campo | Valor |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 2026-09-12 |
| **Etapa** | Construcción — Fase 4 (canal completo) |
| **Resuelve** | Cómo se representa un número que todavía no tiene credenciales, ahora que SQL no puede escribirlas, y hasta cuándo se puede reescribir `0001` |
| **Requisitos afectados** | F20 · sección 8.5 (validación HMAC-SHA256) · sección 7.3 |
| **Enmienda** | [ADR-0031](0031-cifrado-en-reposo-de-datos-sensibles.md) §4 — la consecuencia *"las cuatro columnas de `contacts` son `NULL`-ables y las otras tres no"* · [`migrations/README.md`](../../../src/core/migrations/README.md) — la regla que caduca |
| **Relacionada con** | [ADR-0012](0012-cuenta-y-bandejas-de-entrada.md), [ADR-0031](0031-cifrado-en-reposo-de-datos-sensibles.md) |

---

## 1. Contexto

[ADR-0031](0031-cifrado-en-reposo-de-datos-sensibles.md) decidió cifrar las siete columnas `bytea` del esquema con AES-256-GCM en la aplicación. Dos de ellas son de `inboxes`: `access_token_encrypted` y `app_secret_encrypted`, las credenciales de Meta de cada número.

Hasta la Fase 3 esas dos columnas **no se cifraban de verdad**. Eran `NOT NULL`, la semilla demo escribía un byte `\x00` en cada una y el repositorio lo recortaba. Era un marcador que decía "cifrado" sin serlo, y alcanzaba porque nadie leía los secretos: el envío estaba doblado y el webhook no validaba la firma.

La Fase 4 valida la firma HMAC-SHA256 de cada webhook (§8.5) con el `app_secret` de la bandeja que el payload nombra. Para eso el secreto tiene que descifrarse con el mismo mecanismo que el resto, y ahí chocan tres hechos:

- **El marcador no es un texto cifrado.** GCM lo rechaza al descifrar, y por la regla 2 de ADR-0031 un valor que no descifra tiene que fallar, no leerse como otra cosa.
- **SQL no puede cifrar.** La llave vive sólo en el núcleo (regla 8 de ADR-0031), así que ni la semilla ni un `INSERT` manual pueden producir un valor válido.
- **Las columnas eran `NOT NULL`.** Con las dos condiciones anteriores, no había forma de tener una bandeja sin credenciales reales que no fuera inventando bytes.

Aparte, [`migrations/README.md`](../../../src/core/migrations/README.md) congelaba `0001` "desde la Fase 4", porque suponía que en esa fase entraban datos reales del cooperante. La Fase 4 construyó el canal y **todavía no hay número del cooperante**: no hay ningún dato que proteger.

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **`access_token_encrypted` y `app_secret_encrypted` admiten `NULL`**, y `NULL` significa *"número que nadie conectó todavía"*. El cambio se hace editando `0001`, no con una migración nueva (regla 4) |
| 2 | **Una bandeja sin `app_secret` no autentica nada.** Todo webhook que la nombre queda en `webhook_events` con `signature_valid = false` y `status = 'discarded'`, el valor que el DER ya reserva para la firma inválida. Un secreto vacío nunca valida, porque un HMAC con clave vacía lo puede calcular cualquiera |
| 3 | **Los dos secretos se escriben únicamente a través del cifrado de ADR-0031.** Mientras no exista el ABM de bandejas, el camino es `core --connect-inbox` (`make connect-inbox`): cifra los dos juntos sobre una fila que ya existe. El resto de la fila —cuenta, agente por defecto, nombre— sigue siendo SQL, porque no tiene nada que cifrar |
| 4 | **`0001` es reescribible hasta que se conecte la primera WABA real, no hasta la Fase 4.** Desde ese momento es inmutable y todo cambio de esquema va en un archivo nuevo |

## 3. Justificación

- **`NULL` y no un marcador, porque "sin dato" no es "cifrado".** ADR-0031 §4 ya exige que *"sin dato" nunca pueda confundirse con "cifrado vacío"*. El marcador `\x00` hacía exactamente esa confusión, y con el cifrado real peor todavía: un número sin conectar se leía como un texto cifrado corrupto, que es un error. Así se estaba tratando como falla un estado legítimo.
- **El mismo trato que ya tienen las cuatro columnas de `contacts`.** Ahí `NULL` ya significa "el contacto no tiene el dato". Las dos de `inboxes` pasan a esa forma, y queda una sola columna cifrada `NOT NULL`: `contact_attributes.value_encrypted`, porque lo que extrae F31 siempre existe cuando se escribe.
- **Escribir desde Go, como `--seed-admin`.** El precedente ya existe: el hash bcrypt del primer admin tampoco se puede producir en SQL, y por eso lo escribe el núcleo. La alternativa de cifrar en la base con `pgcrypto` ya la descartó ADR-0031, porque la llave viajaría en la sentencia.
- **Actualizar y no insertar.** Lo único que SQL no puede hacer es cifrar. Hacer que el comando cree la fila entera sería construir el ABM de bandejas desde la línea de comandos, y ese ABM está diferido.
- **El vencimiento se ata al hecho que protege.** La regla existe para no reescribir un esquema con datos reales del cooperante, no para marcar un número de fase. Congelar sin datos obliga a hacer migraciones correctivas sobre una base que se puede recrear, y no protege nada.

## 4. Consecuencias

### Positivas

- El cifrado de ADR-0031 cubre de verdad las siete columnas: ya no queda ningún marcador que diga "cifrado" sin serlo.
- Un número sin conectar es un estado explícito y verificable (`IS NULL`), no un valor que hay que saber interpretar.
- La validación de firma no tiene casos especiales: sin secreto no hay firma válida, y el resultado queda en `webhook_events`, como cualquier otro intento.
- El esquema se puede seguir corrigiendo en `0001` mientras no haya nada que proteger.

### Negativas / costos asumidos

- **Un número real que alguien olvidó conectar se ve como un ataque.** Sus mensajes quedan `discarded` igual que una falsificación, y no hay nada en la bandeja que avise que falta el secreto: sólo el log y la fila en `webhook_events`. Hasta que exista el ABM, conectar un número y verificar que el primer webhook entre como válido es un paso manual que alguien tiene que acordarse de hacer.
- **Conectar un número se hizo más caro.** Son dos pasos —el `INSERT` y el comando— y hacen falta acceso al contenedor del núcleo y la llave. Quien sólo tiene acceso a la base ya no puede hacerlo. Es lo que ADR-0031 busca, pero es un costo operativo real.
- **Los secretos pasan en claro por el `.env` durante la conexión.** `.env.example` indica borrarlos después, y eso depende de que alguien lo haga.
- **El congelamiento de `0001` depende de que alguien note el hecho.** Una fecha fija se ve venir; "la primera WABA real" es un evento que nadie tiene que registrar para que ocurra. Si pasa sin que se note, alguien puede reescribir `0001` sobre datos reales y el corredor lo va a frenar al arrancar, pero recién ahí.
- **El código trata dos estados por columna.** El repositorio descifra `NULL` como vacío, y el envío y el webhook tienen que tratar vacío como "sin conectar". Es el mismo costo que ADR-0031 ya asumió para `contacts`.

### Lo que queda abierto

| Qué | Por qué no se resuelve acá |
|---|---|
| **Cómo se entera un operador de que un número está sin conectar** | Es trabajo de pantalla y le corresponde al ABM de bandejas, que está diferido hasta que el frontend lo necesite o haya más de un número real |
| **Dónde queda registrado el congelamiento de `0001`** | El día que se conecte la primera WABA real hay que dejarlo escrito. El lugar natural es `migrations/README.md`, pero hoy ningún procedimiento lo obliga |

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Mantener `NOT NULL` y cifrar un valor centinela** (por ejemplo, `"sin-conectar"`) | La semilla no puede cifrar, así que igual haría falta Go. Y es peor que eso: el centinela sería una clave HMAC conocida, escrita en el repositorio, con la que cualquiera podría firmar webhooks válidos contra una bandeja sin conectar |
| **Mantener el marcador `\x00` y tratarlo como "sin conectar" en el repositorio** | Obliga al descifrado a tener un caso especial, y deja un dato corrupto indistinguible de un número sin conectar, que es justamente lo que prohíbe la regla 2 de ADR-0031 |
| **Una migración nueva (`0002`, `ALTER COLUMN … DROP NOT NULL`) en vez de editar `0001`** | Es lo correcto una vez que haya datos reales. Antes de eso es una migración correctiva desde el primer día sobre una base que se puede recrear, que es lo que la regla de `migrations/README.md` evita |
| **Que `--seed-admin` también cargue secretos de demo en la bandeja demo** | La clave de la bandeja demo quedaría en el repositorio, y cualquiera que lo lea podría firmar webhooks contra una instancia de desarrollo. Además mezcla datos de demo con el instalador, que nunca siembra demo |
| **Construir ya el ABM de bandejas** | El plan lo difiere, no hay número real que gestionar y nadie lo está pidiendo. El comando cubre el hueco con una fracción del trabajo y no impide construir el ABM después |
