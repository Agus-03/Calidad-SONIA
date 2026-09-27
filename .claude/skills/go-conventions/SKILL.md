---
name: go-conventions
description: 'Idioma del Go de `src/core/`: forma de un paquete, construcción e inyección, manejo de errores, HTTP con gin, acceso a datos con pgx, configuración, logging, concurrencia y tests. Usar antes de escribir o revisar cualquier archivo `.go` de este repositorio.'
---

# Go — idioma de `src/core/`

## 0. Qué es esto y qué no

Esto es **cómo escribe Go este repositorio**, extraído del código que ya existe. No es el lugar de las convenciones de modelado: **C01–C10 viven en `docs/design/00-convenciones.md`** y las reglas de estructura en `docs/design/08-structure-rules.md`. Ante conflicto ganan ésos, y si algo de acá los contradice, se corrige acá.

Cada regla de abajo está en el código. Si una deja de ser cierta, es que el idioma cambió: se actualiza este archivo, no se lo ignora.

## 1. La forma de un paquete

- Un paquete de `internal/` es un *bounded context* y se reconoce por las tablas que escribe. Cinco archivos planos: `model.go` (structs con tags `json`), `repository.go` (SQL), `service.go` (reglas), `controller.go` (HTTP) y `routes.go` (la tabla de rutas, sección 4). Sin subcarpetas de capa.
- El comentario `// Package x …` va en **un solo archivo** del paquete —el que mejor lo define— y dice para qué existe, no qué contiene: `// Package operators owns the operators table: the person who attends, never called User (C01, F18).`
- `platform/` y `app/` son las dos excepciones declaradas. `platform/` guarda lo transversal —`http/`, `httpclient/`, `auth/`, `db/`, `config/`, `logging/`, `id/`— y `app/` el arranque.
- Cuando un archivo pasa las ~400 líneas se parte en varios archivos del mismo paquete, no en una carpeta.

## 2. Construcción e inyección

- Un `New…` por tipo, que **recibe** sus dependencias y no las busca: `NewRepository(pool)`, `NewService(repo, brain, log)`, `NewController(service, log)`.
- `internal/app/deps.go` es el único lugar donde se arma el grafo, en orden repositorio → servicio → controlador. No hay globals: `slog.SetDefault` se llama una vez en el arranque (`internal/app/`) y el logger igual viaja inyectado.
- Cuando una dependencia tiene más de tres parámetros, se pasa un struct de opciones (`brainclient.Options`, `RouterOptions`), no seis posicionales.
- El consumidor declara la interfaz mínima que necesita, del lado que la usa: `Brain` en `turn/orchestrator.go` tiene un método, `issuer` en `operators/service.go` tiene uno. No se importa el paquete concreto para tipar una dependencia.
- Defensivo sólo en el borde: `if opts.Logger == nil { opts.Logger = slog.Default() }`. Un nil no debe bajar el proceso en el primer request.

## 3. Errores

- Los errores de dominio son `var Err… = errors.New("<paquete>: <qué pasó>")`, declarados juntos arriba de `service.go`, con el nombre del paquete como prefijo del mensaje.
- El servicio devuelve errores de dominio y **no conoce códigos HTTP**; el controlador los mapea. Un `switch`/`errors.Is` en el controlador es el único lugar donde el dominio se cruza con el status.
- Cada paquete tiene su `ErrNotFound`, y **cubre también la fila borrada lógicamente**, no sólo la ausente (C09).
- Siempre envolver con contexto y `%w`: `fmt.Errorf("inbox: get generation: %w", err)`. De un repositorio nunca sale un `err` pelado.
- `errors.Is` para centinelas, `errors.As` para tipos. Un error se vuelve tipado cuando hay que **decidir** sobre él: `StatusError{Status, Body}` existe para distinguir un rechazo (4xx, no se reintenta) de una caída (5xx, se reintenta).
- `pgx.ErrNoRows` se traduce a `ErrNotFound` dentro del repositorio y no sale del paquete.

## 4. HTTP (gin)

- El handler parsea, valida, delega y responde. Ninguna regla de negocio vive ahí.
- Se responde **siempre** por los helpers de `platform/http`: `OK`, `Created`, `NoContent`, `Fail`. Nunca un `c.JSON(500, …)` a mano.
- Los errores de API se construyen con `BadRequest`/`Unauthorized`/`Forbidden`/`NotFound`/`Conflict`/`Unavailable`/`Internal`, con un `code` estable en `snake_case` que el frontend puede switchear. La causa técnica va con `WithCause(err)`: termina en el log, nunca en el body.
- El contexto del request es `ctx.Request.Context()`. `context.Background()` en un handler es un error.
- Un identificador que llega por la URL se parsea con `uuid.Parse` y su fallo es un 400 con código propio (`invalid_<recurso>_id`), no un 500.
- **Las rutas son datos, no efectos.** Cada contexto declara las suyas en `routes.go`, implementando `Routes() []soniahttp.Route`; `internal/app/deps.go` lo pasa en `Controllers` y **sólo `platform/http` toca gin**. `/health` queda fuera de autenticación y todo lo demás bajo `/api/v1`.
- Una fila lleva los cuatro datos que son una sola decisión: `Method`, `Path`, `Roles` y `Handler`. Van juntos porque separarlos deja publicar un endpoint sin protección sin que se note; una fila sin `Roles` **hace entrar en pánico al router al arrancar**, que es el único momento en que ese error es barato.
- Los roles se enumeran incluso cuando entran los tres: explícito, nunca por omisión. El prefijo del contexto se escribe una vez en un `const routeBase` del paquete.
- El handler que va en la tabla **sigue siendo privado** (`c.create`): viaja como valor. Que las rutas se declaren afuera del `controller.go` no es motivo para exportar nada.
- Middleware transversal y en este orden: `RequestID`, `AccessLog`, `Recovery`. El `X-Request-Id` vuelve en la respuesta y aparece en cada línea de log.

## 5. Acceso a datos (pgx)

- El repositorio recibe un `db.Executor` —que satisfacen el pool y `pgx.Tx`—, no el pool. No abre conexiones, no conoce el DSN y no sabe si está dentro de una transacción.
- Placeholders `$1`, siempre. La única concatenación admitida es de **SQL literal** —un fragmento `const` más su `WHERE`, como `conversationSelect` u `operatorSelect`—, nunca de un valor.
- SQL en crudo, sin ORM, con las palabras clave alineadas (`SELECT` / `FROM` / `JOIN` / `WHERE` / `ORDER BY`) para que el diff se lea.
- `defer rows.Close()` y **`return out, rows.Err()`** al cerrar el recorrido: el error del iterador no se ignora.
- El scan de una fila compleja se factoriza en `scanX(row pgx.Row)`, como `scanInbox` en `whatsapp/repository.go`. Los nulos se leen en punteros locales y se trasladan al modelo sólo si el grupo está completo.
- Transacción: `db.InTx(ctx, pool, func(db.Executor) error)`, con `defer tx.Rollback(ctx)` adentro. La abre quien conduce, nunca el repositorio (regla *«La transacción la abre quien conduce»*), y le pasa el `Executor` a cada repositorio que participa.
- Un slice de salida se inicializa `out := []X{}`, no `var out []X`: en JSON tiene que ser `[]` y no `null`.
- **Los timestamps los escribe `now()` de PostgreSQL**, no el reloj del proceso: es el único reloj que comparten los tres escritores (`migrations/README.md`).
- Nunca `float64` para dinero (C08).

## 6. Llamadas salientes

- Timeout por llamada con `context.WithTimeout`, y **distinto según quién espera**: 3 s la previsualización porque hay un administrador tipeando, 10 s el turno.
- Reintentos acotados, configurables por entorno, y **sólo sobre 5xx**: un 4xx no mejora si se pregunta otra vez.
- Backoff lineal, esperando en un `select` que también escucha `ctx.Done()`.
- Cuerpo de respuesta acotado con `io.LimitReader`: un servicio que responde mal no llena la memoria del núcleo.
- `defer func() { _ = res.Body.Close() }()` — ese `_ =` explícito es la única forma admitida de descartar un error.

## 7. Configuración

- Se lee del entorno en `platform/config`, una sola vez en `main.go`, y se inyecta. Nunca un global ni `os.Getenv` disperso en los paquetes.
- **Se acumulan todos los problemas y se reportan juntos** (`problems []string`) en lugar de fallar en el primero: arrancar tres veces para descubrir tres variables faltantes es peor que leer una lista.
- Los valores por defecto van en el struct, vía `env(key, fallback)`. Las duraciones se nombran con la unidad (`BRAIN_TIMEOUT_MS`) y se validan positivas.
- Si falta un secreto, el proceso no levanta.

## 8. Logging

- `*slog.Logger` inyectado, salida JSON, con `service` y `environment` ya puestos. Nunca `fmt.Println` ni `log.Printf`.
- Campos tipados (`slog.String`, `slog.Int`, `slog.Int64`), nombres en `snake_case` y con la unidad adentro: `latency_ms`.
- Un 5xx se loguea en `Error` y un 4xx en `Warn`. La causa técnica va al log; el cliente recibe código y mensaje.

## 9. Identificadores, tiempo y concurrencia

- `id.New()` para toda clave primaria: UUID v7 generado en la aplicación (C04). `uuid.New()` (v4) sólo en tests, nunca en una fila persistida.
- Todo identificador que viene de afuera se parsea antes de tocar una consulta.
- `time.Time` en el modelo, `time.Duration` en la configuración; una duración que se persiste o se loguea va en milisegundos, con el sufijo en el nombre.
- Concurrencia poca y explícita: la goroutine del servidor en `app/app.go`, con su error por un canal buffereado, la del poller en `turn/poller.go`, que cierra los bloques vencidos con su propio contexto (ADR-0041), una por turno en `turn/orchestrator.go` (ADR-0029), contada en un `sync.WaitGroup` y con su propio deadline, y en `inbox/websocket.go` el worker del hub más una por socket abierto (ADR-0051), contadas en otro. Toda goroutine tiene cómo terminar y alguien que la espera: `signal.NotifyContext` en `main.go`, y después `Hub.Close`, `server.Shutdown`, `Poller.Wait` y `Orchestrator.Wait` con timeout, porque un turno en vuelo tiene un cliente esperando.
- `panic` sólo donde es correcto que el proceso no pueda seguir —`id.New()` es el caso— y `Recovery` lo convierte en 500 sin bajar el proceso.

## 10. Comentarios

- En **inglés** (C02), de una línea, y dicen *por qué*, no *qué*: `an unverified pool fails on the first customer message`, `a 4xx will not get better by asking again`.
- El que ancla una decisión la cita: `(C04)`, `(ADR-0017 (e))`, `(regla 9)`, `(Paso 8, decision 3)`, `(F05)`. Ese anclaje es lo que permite revisar el código contra el diseño.
- Lo obvio no se comenta: `// NewService builds the service.` es el techo de verbosidad aceptable para un constructor.

## 11. Tests

- `_test.go` en el **mismo** paquete, para poder probar lo no exportado sin abrirlo.
- Sólo la biblioteca estándar: `testing`, `net/http/httptest`. Sin librerías de assertions.
- Tabla de casos con campo `name` y `t.Run(tc.name, …)`, donde el nombre **es la regla enunciada**: `"a 400 will not get better by asking again: the request itself is what was refused"`.
- `t.Fatalf` mostrando obtenido y esperado.
- El test nombra el requisito que protege (`TestPromptComponentTypesMatchF01`, `TestAgentModeCoversF02`, `TestVerifyGuardsTheSubscriptionOfF20`), y los de enumeración prueban también **lo inválido** —`AgentMode("agent")`, `GenerationStatus("sent")`— porque C01 y los renombres se violan por costumbre, no por decisión.
- El controlador se prueba montando el router real con un `ActorLookup` falso y el servicio sobre un store de prueba (`stub_test.go`): los casos de permiso se resuelven en el router, antes de llegar al servicio.

## 12. Lo que no hay

Sin ORM, sin framework de inyección, sin librería de assertions, sin logger de terceros. Las dependencias directas son `gin`, `pgx` y `uuid`, más las dos que trajo ADR-0030 —`golang-jwt/jwt` y `golang.org/x/crypto` para bcrypt— y `coder/websocket`, que trajo ADR-0051 para F21, y nada más.

Si algo de eso hace falta, es una decisión de arquitectura: va a un ADR, no a un `go get`.
