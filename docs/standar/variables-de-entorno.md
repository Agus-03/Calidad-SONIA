# SONIA — Variables de entorno

| Campo | Valor |
|---|---|
| **Documento** | `docs/standar/variables-de-entorno.md` |
| **Estado** | Vigente |
| **Fecha** | 2026-09-20 · actualizado el 2026-10-03 (documentos de F06) y el 2026-10-04 (worker de ingesta) |
| **Fuente** | El código que las lee: [`config.go`](../../src/core/internal/platform/config/config.go) (núcleo), [`config.py`](../../src/brain/infra/config.py) (cerebro), [`docker-compose.yml`](../../deploy/docker-compose.yml) y el [`Makefile`](../../Makefile) |

---

## 0. Propósito

Este documento explica **para qué sirve cada variable de entorno**, quién la lee, qué valor toma si no se la define y qué pasa cuando falta o está mal.

Existe porque el `.env.example` dejó de explicarlas: ese archivo ahora sólo clasifica y enumera, con las mismas siete secciones que tiene este documento y en el mismo orden. Un comentario al lado de una variable se lee una vez, cuando se copia el archivo, y después envejece sin que nadie lo note; acá el envejecimiento es visible porque el documento tiene fecha y estado.

**Regla de precedencia:** ante una diferencia entre este documento y el código, **gana el código**, y este documento se corrige. Los valores por defecto que figuran acá son los que el código aplica cuando la variable está ausente, no una recomendación.

**Convención de lectura:** una variable **obligatoria** frena el arranque del proceso si falta. Una con **valor por defecto** no: el proceso levanta con ese valor, que es exactamente el riesgo de olvidarla.

---

## 1. compose

Las lee `docker compose`, ningún proceso. Las tres llevan `:?` en [`docker-compose.yml`](../../deploy/docker-compose.yml), así que **el `compose` no arranca si falta alguna**.

| Variable | Valor por defecto | Qué hace |
|---|---|---|
| `POSTGRES_DB` | obligatoria | Nombre de la base que crea el contenedor de PostgreSQL. Además entra en el `healthcheck` y en el DSN que el compose arma para los dos servicios. |
| `POSTGRES_USER` | obligatoria | Usuario dueño de esa base, con los mismos tres usos. |
| `POSTGRES_PASSWORD` | obligatoria | Su contraseña. Viaja al DSN que el compose le pasa al núcleo y al cerebro. |

Las tres se usan también fuera del compose: el `Makefile` arma con ellas el DSN de los tests de integración y del smoke.

---

## 2. compartidas

Las leen el núcleo y el cerebro por igual, con la misma semántica en los dos.

| Variable | Valor por defecto | Qué hace |
|---|---|---|
| `DATABASE_URL` | obligatoria en ambos | DSN de la única base que comparten los dos servicios (ADR-0017). El núcleo no arranca sin ella; el cerebro la valida como `PostgresDsn`. **Dentro del compose el valor del `.env` se ignora**: el compose la reescribe apuntando al servicio `db`. Sólo pesa cuando se corre un proceso fuera del compose. |
| `ENVIRONMENT` | `development` | Nombre del ambiente. Su único efecto hoy es habilitar los dobles: `WHATSAPP_USE_DOUBLE`, `LLM_USE_DOUBLE` y `STORAGE_EMULATOR_HOST` sólo se admiten con `development` o `test`. Es una lista blanca y no una prohibición de `production`, para que un `prod` mal tipeado no deje pasar respuestas enlatadas. |
| `LOG_LEVEL` | `info` | Umbral del log. Sólo se admite `debug`, `info`, `warning` o `error`; cualquier otro valor frena el arranque de los dos servicios. |
| `DB_POOL_MIN_SIZE` | `2` en el núcleo, `1` en el cerebro | Conexiones que el pool mantiene abiertas. Tiene que ser un entero positivo. |
| `DB_POOL_MAX_SIZE` | `10` en ambos | Tope de conexiones del pool. Si queda por debajo del mínimo, ninguno de los dos servicios arranca. |

---

## 3. contrato core ↔ brain

El núcleo llamando al cerebro. No confundir con la sección 5, que es el cerebro escuchando.

| Variable | Valor por defecto | Qué hace |
|---|---|---|
| `BRAIN_INTERNAL_TOKEN` | obligatoria en ambos | Secreto compartido con el que el núcleo firma cada llamada al cerebro y el cerebro la verifica (ADR-0017 (k)). Es la única variable que **los dos** procesos exigen con el mismo valor: si no coinciden, el arranque pasa y lo que falla es cada turno. |
| `BRAIN_BASE_URL` | obligatoria | Dónde vive el cerebro, desde el punto de vista del núcleo. Dentro del compose se reescribe a `http://brain:8000`. |
| `BRAIN_TIMEOUT_MS` | `10000` | Lo que el núcleo espera por el turno entero antes de cortar. Es el techo del que cuelgan los tiempos del cerebro: `LLM_TIMEOUT_SECONDS` tiene que quedar por debajo. |
| `BRAIN_PREVIEW_TIMEOUT_MS` | `3000` | Lo mismo, pero para la vista previa, que es interactiva y no puede hacer esperar a un operador diez segundos. |

---

## 4. core (Go)

Las lee el núcleo al arrancar.

### Servidor

| Variable | Valor por defecto | Qué hace |
|---|---|---|
| `CORE_HTTP_ADDR` | `:8080` | Dirección donde escucha el núcleo, en formato `host:puerto`. Dentro del compose se reescribe a `:8080`. |

### El turno

| Variable | Valor por defecto | Qué hace |
|---|---|---|
| `TURN_MAX_RETRIES` | `3` | Reintentos de un turno que falló. Admite `0`, que significa no reintentar. |
| `TURN_RETRY_BACKOFF_MS` | `250` | Espera entre un intento y el siguiente. |
| `TURN_BUDGET_MS` | `30000` | Acota cuánto vive una goroutine de turno, y con eso cuánto espera un despliegue antes de cortar (ADR-0029). |

### El temporizador del turno (F10, ADR-0041)

Los cuatro primeros valores son un punto de partida y no una medición: el número correcto se descubre con tráfico real, y `message_batches.timer_ms_used` guarda cuál se aplicó en cada bloque para poder compararlos después.

| Variable | Valor por defecto | Qué hace |
|---|---|---|
| `TURN_BATCH_WINDOW_MS` | `10000` | Cuánto espera un bloque al mensaje siguiente. Cada mensaje que llega la reinicia: eso es lo adaptativo del temporizador. |
| `TURN_POLL_INTERVAL_MS` | `1000` | Cada cuánto el poller pregunta qué bloques vencieron. **No puede ser mayor que la ventana**, o el tick pasa a ser la ventana real; el núcleo rechaza esa combinación al arrancar. |
| `TURN_BATCH_MAX_MESSAGES` | `10` | Tope de mensajes por bloque. Cierra sin que la ventana venza, que es el único caso en que se corta a un cliente escribiendo. |
| `TURN_BATCH_MAX_DURATION_MS` | `60000` | Lo máximo que un bloque puede durar desde que se abrió. **No puede ser menor que la ventana**, o todos los bloques cerrarían por duración máxima sin esperar nunca; el núcleo también rechaza eso al arrancar. |
| `TURN_POLL_LIMIT` | `10` | Cuántos bloques cierra un tick como mucho, para que un núcleo que estuvo caído no dispare cien turnos en el mismo segundo. |

### Autenticación de operadores (ADR-0030)

| Variable | Valor por defecto | Qué hace |
|---|---|---|
| `JWT_SECRET` | obligatoria | Firma el token de sesión. Si falta **o mide menos de 32 bytes**, el núcleo no arranca. Rotarla desloguea a todos. |
| `JWT_TTL_MS` | `43200000` (12 h) | Cuánto vive una sesión. Doce horas para que sobreviva una jornada larga y venza fuera del horario de atención, nunca en medio de una conversación. |

### Cifrado en reposo (ADR-0031)

| Variable | Valor por defecto | Qué hace |
|---|---|---|
| `ENCRYPTION_KEY` | obligatoria | Clave con la que se cifran los secretos guardados en la base. Base64 de 32 bytes crudos: si falta, no decodifica o no mide 32, el núcleo no arranca, aunque todavía no haya nada cifrado. Perderla es perder los datos y no una contraseña que se resetea: entra en el procedimiento de respaldo. |

### Documentos de F06 (ADR-0064, ADR-0069)

| Variable | Valor por defecto | Qué hace |
|---|---|---|
| `DOCUMENT_ENCRYPTION_KEY` | obligatoria | Clave con la que el núcleo cifra cada documento antes de subirlo, y con la que el `worker` lo descifra al ingerirlo. Base64 de 32 bytes crudos, con las mismas condiciones que `ENCRYPTION_KEY`, y **tiene que ser distinta de ella**: si son iguales, el núcleo no arranca, porque el `worker` que la tiene no debe poder leer los datos sensibles (ADR-0064, regla 2). Perderla es perder los documentos guardados. |
| `FIREBASE_STORAGE_BUCKET` | obligatoria | Bucket de Cloud Storage for Firebase donde quedan los documentos cifrados. Tiene que estar en `us-east1`, `us-west1` o `us-central1`, las regiones que cubre la cuota gratuita (ADR-0069). |
| `GOOGLE_APPLICATION_CREDENTIALS` | obligatoria salvo con `STORAGE_EMULATOR_HOST` | Ruta, **dentro del contenedor**, al JSON de la cuenta de servicio de Firebase. El archivo va en `secrets/`, que git ignora, y el compose monta esa carpeta en `/run/secrets`. Es el nombre que leen los clientes de Google, así que el `worker` encuentra el mismo archivo. Si el archivo no existe o Google rechaza la cuenta, el núcleo no arranca. |
| `STORAGE_TIMEOUT_MS` | `120000` (2 min) | Lo que el núcleo espera por una subida o un borrado en el almacén. Es largo porque una subida de hasta 100 MB viaja entera en una sola llamada. |

### Canal de WhatsApp (Meta Cloud API)

El access token de Meta no está acá: es de la bandeja y lo configura la pantalla de F29, porque una cuenta conecta varios números.

| Variable | Valor por defecto | Qué hace |
|---|---|---|
| `META_GRAPH_BASE_URL` | `https://graph.facebook.com` | Raíz de la API de Meta. Se cambia para apuntar a un doble o a un proxy. |
| `META_API_VERSION` | `v21.0` | Versión del path de la Graph API. Fijada y nunca flotante: Meta versiona el path, y una versión sin fijar cambia el contrato sin un deploy. |
| `WHATSAPP_TIMEOUT_MS` | `5000` | Lo que el núcleo espera por Meta. Más corto que el del cerebro: Meta responde en cientos de milisegundos, y el turno ya gastó su presupuesto aguas arriba. |

---

## 5. brain (Python)

Las lee el cerebro al arrancar.

### Servidor

| Variable | Valor por defecto | Qué hace |
|---|---|---|
| `BRAIN_HTTP_HOST` | `0.0.0.0` | Interfaz donde escucha el cerebro. Dentro del compose se reescribe a `0.0.0.0`. |
| `BRAIN_HTTP_PORT` | `8000` | Su puerto. Tiene que estar entre 1 y 65535. |

### Proveedores de LLM

| Variable | Valor por defecto | Qué hace |
|---|---|---|
| `COHERE_API_KEY` | vacía | Credencial de Cohere. |
| `ANTHROPIC_API_KEY` | vacía | Credencial de Anthropic. |
| `OPENAI_API_KEY` | vacía | Credencial de OpenAI. |
| `LLM_TIMEOUT_SECONDS` | `8` | Lo que el cerebro espera por una llamada al modelo. Tiene que quedar por debajo de `BRAIN_TIMEOUT_MS`, que es lo que el núcleo espera por el turno entero. |

### Worker de ingesta de F06 (ADR-0064, ADR-0067, ADR-0071)

Las lee el cerebro sólo en modo `worker`, que **no arranca** si falta alguna de las obligatorias y las lista todas juntas. El modo `api` no las necesita, y el compose le vacía `DOCUMENT_ENCRYPTION_KEY` (ADR-0064, regla 2). Al `worker`, a su vez, le vacía `ENCRYPTION_KEY`. `DOCUMENT_ENCRYPTION_KEY`, `FIREBASE_STORAGE_BUCKET`, `GOOGLE_APPLICATION_CREDENTIALS` y `STORAGE_EMULATOR_HOST` son las mismas que lee el núcleo (sección 4).

| Variable | Valor por defecto | Qué hace |
|---|---|---|
| `QDRANT_URL` | obligatoria en el `worker` | Dónde está Qdrant, que corre fuera de este compose. En el *smoke* es el contenedor `qdrant`. |
| `QDRANT_API_KEY` | vacía | Clave de Qdrant, si el servicio la pide. Viaja en el encabezado `api-key`. |
| `QDRANT_TIMEOUT_SECONDS` | `30` | Lo que el `worker` espera por cada llamada a Qdrant. |
| `VOYAGE_API_KEY` | obligatoria en el `worker`, salvo con `EMBEDDINGS_USE_DOUBLE` | Credencial de Voyage AI, que vectoriza con `voyage-4` (ADR-0067). Antes de mandarle el primer documento hay que desactivar el entrenamiento en su organización (ADR-0067, regla 10). |
| `VOYAGE_TIMEOUT_SECONDS` | `30` | Lo que el `worker` espera por cada lote de embeddings. |
| `STORAGE_TIMEOUT_SECONDS` | `120` | Lo que el `worker` espera por la descarga de un documento de hasta 100 MB. |
| `DESCRIPTION_TIMEOUT_SECONDS` | `60` | Lo que el `worker` espera por la descripción de una imagen. Es más largo que `LLM_TIMEOUT_SECONDS` porque no hay una clienta esperando. |
| `WORKER_POLL_INTERVAL_SECONDS` | `5` | Cada cuánto el `worker` busca documentos pendientes y barre los dados de baja. |
| `WORKER_LOCK_TIMEOUT_SECONDS` | `900` (15 min) | Cuánto puede estar un documento en `processing` antes de que el `worker` lo dé por abandonado y lo vuelva a `pending` (ADR-0064, regla 8). |

**Ninguna de las tres claves es obligatoria al arrancar, y es deliberado:** el proveedor se elige por agente (`ai_agents.llm_provider`), así que exigir las tres impediría levantar el cerebro para usar uno solo. La contracara es que una clave faltante no se descubre al arrancar sino en el primer turno que la necesita.

---

## 6. sólo comandos

No las lee ningún arranque: las lee un `make` puntual. Con todas vacías los dos servicios levantan igual, y lo único que falla es el comando (regla 3 de la estructura del código).

| Variable | Valor por defecto | Qué hace |
|---|---|---|
| `SEED_ADMIN_EMAIL` | vacía | Correo del primer admin que crea `make seed-admin`. |
| `SEED_ADMIN_PASSWORD` | vacía | Su contraseña. Se hashea con bcrypt al sembrar, así que la única copia en claro es la línea del `.env`: cambiarla después del primer login. |
| `SEED_ADMIN_ACCOUNT_NAME` | `SONIA` | Nombre de la cuenta que se crea si la base todavía no tiene ninguna: instalar es crear la cuenta y su primer admin. |
| `CONNECT_INBOX_PHONE_NUMBER_ID` | vacía | Número de Meta que `make connect-inbox` conecta. La fila de `inboxes` tiene que existir antes (ADR-0031). |
| `CONNECT_INBOX_ACCESS_TOKEN` | vacía | Token de Meta de esa bandeja. El comando lo cifra en la fila; **borrarlo del `.env` después de conectar**, porque la copia que vale es la cifrada y la del archivo sería la única en claro. |
| `CONNECT_INBOX_APP_SECRET` | vacía | App secret de Meta de esa bandeja, con el mismo tratamiento. |

---

## 7. sólo desarrollo y test

Nada de esto vive en producción.

| Variable | Valor por defecto | Qué hace |
|---|---|---|
| `PGADMIN_EMAIL` | obligatoria para `make pgadmin` | Usuario con el que se entra a pgadmin en `localhost:5050`. Lleva `:?` en [`docker-compose.dev.yml`](../../deploy/docker-compose.dev.yml): sin ella el compose de desarrollo no arranca, pero el stack normal no la necesita. |
| `PGADMIN_PASSWORD` | obligatoria para `make pgadmin` | Su contraseña, con la misma condición. |
| `SONIA_TEST_DATABASE_URL` | vacía | DSN de la base contra la que corren los tests de integración. Si no está, **esos tests se saltean en silencio en lugar de fallar**, que es lo que hace el CI. El `Makefile` la arma desde las `POSTGRES_*` y la exporta, así que el valor del `.env` sólo pesa al correr `go test` a mano. |
| `WHATSAPP_USE_DOUBLE` | `false` | Responde sin salir a Meta. Sólo se admite con `ENVIRONMENT` en `development` o `test`; en otro caso el proceso no arranca. |
| `LLM_USE_DOUBLE` | `false` | Responde texto enlatado en vez de llamar al modelo, que es lo que permite correr sin una clave. Misma restricción de ambiente. |
| `EMBEDDINGS_USE_DOUBLE` | `false` | Vectoriza con una bolsa de palabras en lugar de llamar a Voyage, sin clave y sin sacar los documentos. Sólo se admite con `ENVIRONMENT` en `development` o `test` (ADR-0071). |
| `STORAGE_EMULATOR_HOST` | vacía | Manda los documentos a un `fake-gcs-server` en lugar de Firebase, sin credencial. Sólo se admite con `ENVIRONMENT` en `development` o `test`. Es el nombre que leen los clientes de Google: el compose del smoke la apunta a `http://storage:4443` (ADR-0070). |

---

## 8. Declaradas en el `.env.example`, sin lector

Estas cinco secciones de variables figuran en el `.env.example` pero **ningún proceso las lee hoy**: no están en el `Settings` del cerebro ni en el `Config` del núcleo, y como el `Settings` declara `extra="ignore"`, se descartan en silencio. Cambiar su valor no tiene ningún efecto.

| Variable | Situación |
|---|---|
| `ANTHROPIC_REFUSAL_FALLBACK` | Sin lector en el código actual. |
| `REWRITER_LLM_PROVIDER` | Sin lector. Referenciada sólo en una copia vieja de `config.py` bajo `z_helps/`, que no tiene peso oficial. |
| `REWRITER_LLM_MODEL` | Sin lector. |
| `REWRITER_TIMEOUT_SECONDS` | Sin lector. |
| `REWRITER_MAX_RETRIES` | Sin lector. |
| `LLM_CONTEXT_WINDOW_TOKENS` | Sin lector. Referenciada sólo en esa misma copia vieja. |
| `PROMPT_SAFETY_MARGIN_TOKENS` | Sin lector, ídem. |
| `PROMPT_HISTORY_MAX_TURNS` | Sin lector, ídem. |
| `TOKEN_COUNT_MODE` | Sin lector. Los valores que el `.env.example` admite son `estimate` y `exact`. |
| `RULES_DIR` | Sin lector. |

No se documenta para qué sirven porque no hay código que lo diga, y suponerlo sería inventarlo. Quedan listadas para que la pregunta —si son de una fase futura o restos de un refactor— sea visible y no se responda por omisión.

---

## 9. Diferencias conocidas

Tres cosas que este relevamiento dejó a la vista y que todavía no están decididas:

1. **`DATABASE_URL` repite lo que dicen las `POSTGRES_*`.** El compose arma el DSN desde las tres y pisa lo que diga el `.env`, así que el mismo dato vive dos veces y nada garantiza que coincidan.
2. **El mismo DSN aparece una tercera vez** en `SONIA_TEST_DATABASE_URL`, que el `Makefile` también calcula desde las `POSTGRES_*`.
3. **`DB_POOL_MIN_SIZE` tiene defaults distintos en los dos servicios**: `2` en el núcleo y `1` en el cerebro. Con la variable definida en el `.env` la diferencia no se nota; sin ella, los dos pools arrancan distinto.

Una diferencia menor y sin consecuencia funcional: el núcleo recibe el DSN con esquema `postgres://` y el cerebro con `postgresql://`, porque cada driver acepta el suyo.

---

## 10. Fuera del `.env`

Las variables `SONIA_SMOKE_CORE_URL`, `SONIA_SMOKE_BRAIN_URL`, `SONIA_SMOKE_ENCRYPTION_KEY`, `SONIA_SMOKE_DOCUMENT_ENCRYPTION_KEY`, `SONIA_SMOKE_STORAGE_URL` y `SONIA_SMOKE_QDRANT_URL` las exporta el `Makefile` en el objetivo `smoke` y no se definen en el `.env`. Las dos de llaves toman su valor de `ENCRYPTION_KEY` y `DOCUMENT_ENCRYPTION_KEY`, la del almacén apunta al `fake-gcs-server` del compose del smoke, en `localhost:4443`, y la de Qdrant al contenedor del smoke, en `localhost:6333`.
