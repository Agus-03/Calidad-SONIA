# ADR-0030 — Autenticación de operadores: JWT de vida corta y bcrypt

| Campo | Valor |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 2026-09-06 |
| **Etapa** | Construcción — Fase 2 (login y operadores) |
| **Resuelve** | Cómo se autentica un operador ante el núcleo y cómo se guarda su contraseña |
| **Requisitos afectados** | F18 (parcial: autenticación, no todavía los tres roles finos) |

---

## 1. Contexto

La Fase 1 dejó el camino del turno andando **sin ninguna noción de quién es quien pide**. Eso alcanzaba porque el único que entraba era Meta, que no es un operador. Deja de alcanzar ahora.

**El esquema ya está, el mecanismo no.** `operators` existe desde la primera migración con `password_hash`, `role` acotado por CHECK a los tres valores del Anexo B, `active`, `deleted_at` y un índice único de email entre los no borrados. Lo que falta no es una tabla: es qué se guarda en `password_hash` y qué prueba que quien pide es el dueño de esa fila.

**Lo que hay hoy es una costura declarada como tal.** `platform/http/auth.go` lee un header `X-Operator-Id`, lo parsea como UUID y le cree. Su propio comentario lo dice: *"ActorLookup is a seam, not a mechanism: no ADR defines authentication yet"*. Cualquiera que alcance el puerto es cualquier operador que quiera nombrar. No es una vulnerabilidad todavía, porque `/api/v1` no tiene ni una ruta montada; es una vulnerabilidad **el día que la Fase 3 monte la primera**.

**Y la Fase 3 es la que sube la apuesta.** [ADR-0011](0011-acceso-a-datos-sensibles.md) resolvió el acceso a datos sensibles apoyándose en dos controles: el aislamiento por asignación, y **el módulo de contactos como exclusivo de `admin`**. Ese segundo control es lo que impide que un operador llegue al DNI, el CUIT y el CBU de cualquier cliente sin pasar por sus conversaciones asignadas — datos alcanzados por la Ley 25.326. Mientras la identidad sea un header que el cliente escribe, esa frontera está escrita en un documento y en ningún otro lado.

**Tres restricciones más, que acotan el espacio de soluciones:**

- **El frontend es React y lo desarrolla otro equipo**, servido desde un origen distinto al del núcleo. Cualquier mecanismo basado en cookies cruza una frontera de origen.
- **El núcleo es de instancia única** ([ADR-0027](0027-nucleo-de-instancia-unica.md)), así que nada obliga a un mecanismo sin estado por razones de coordinación entre procesos.
- **Casi la mitad de las 37 entidades tiene una FK a `operators`** (`created_by_`, `assigned_by_`, `uploaded_by_`). Todo lo que se construya de acá en más escribe un `operator_id`, y ese id tiene que venir de algún lado que no sea un header confiado.

## 2. Decisión

**El operador se autentica con correo y contraseña contra `POST /auth/login`, que devuelve un JWT firmado con HS256 y de vida corta. Ese token viaja en `Authorization: Bearer` en cada petición a `/api/v1`. La contraseña se guarda con bcrypt, factor de costo 12.**

Ocho reglas la acompañan, y son la decisión tanto como el titular:

| # | Regla |
|---|---|
| 1 | **El token lleva únicamente `sub`** —el `operator_id`—, más `iat` y `exp`. Ni el rol, ni el correo, ni el nombre |
| 2 | **`role` y `active` se leen de la fila de `operators` en cada petición.** El token dice quién es; la base dice qué puede |
| 3 | **Vida de 8 horas, sin *refresh token*.** Vencido, se vuelve a loguear |
| 4 | **Transporte en el header `Authorization`, nunca en cookie** |
| 5 | **El secreto sale de `JWT_SECRET`, del entorno, validado al arrancar:** si falta, el proceso no levanta (regla 10 de `CLAUDE.md`) |
| 6 | **Se usa `github.com/golang-jwt/jwt/v5`, con el algoritmo esperado verificado explícitamente** al validar |
| 7 | **No hay *logout* del lado del servidor.** El cliente descarta el token |
| 8 | **La verificación de bcrypt corre también cuando el correo no existe**, contra un hash descartable, para que el tiempo de respuesta no diga qué direcciones están registradas |

**Dónde se implementa:** `platform/auth/` emite y verifica el token; `operators/` es el paquete del contexto —repositorio, servicio, controlador y su `routes.go`—; `platform/http/auth.go` cambia el header confiado por la verificación del Bearer; `app/deps.go` reemplaza el `denyEveryone` provisorio por el lookup real. `JWT_SECRET` y `JWT_TTL` entran en `.env.example` y en `platform/config`.

**Esta fase no lleva DDL.** La decisión no crea ninguna tabla.

## 3. Justificación

- **El header y no la cookie, porque el frontend está en otro origen.** Una cookie cruzando orígenes exige `SameSite=None` más `Secure`, CORS con credenciales, y defensa de CSRF encima — porque el navegador la manda sola, que es justamente lo que la hace vulnerable. `Authorization` no lo manda nadie sin escribirlo, así que no hay CSRF que defender. La cookie compra protección contra XSS que el header no da; se elige el problema que el otro equipo resuelve en su propio código sobre el que obliga a coordinar configuración entre dos equipos.

- **El rol no viaja en el token porque la fila se lee igual.** El middleware necesita saber si el operador sigue activo, y eso no se puede firmar de antemano. Ya que se va a la base, el rol sale de ahí. Lo que compra: **desactivar o borrar un operador le corta el acceso en la petición siguiente, y un cambio de rol pega enseguida** — que es exactamente lo que ADR-0011 necesita para que "el módulo de contactos es de `admin`" sea un control y no una promesa. Un token que llevara el rol adentro convertiría revocar un permiso en algo que tarda hasta ocho horas.

- **bcrypt y no argon2id, porque hay un solo número que defender.** argon2id es mejor **bien calibrado** y peor **mal calibrado**, y calibrarlo exige elegir memoria, iteraciones y paralelismo contra el hardware donde va a correr. OE7 pide despliegue en producción real y esa infraestructura no está elegida: serían tres números puestos a ciegas, que después nadie vuelve a revisar. bcrypt con costo 12 es una decisión con un parámetro, treinta años de uso y ninguna calibración pendiente. El límite de 72 bytes es real y no molesta a nadie.

- **La librería y no HMAC escrito a mano.** Firmar un JWT son sesenta líneas; verificarlo sin aceptar `alg: none` ni confundir HMAC con RSA es donde se cometen los errores conocidos. Es la clase de código donde escribirlo uno mismo no demuestra nada y cuesta una vulnerabilidad.

- **Vida corta y sin *refresh*, porque el *refresh* existe para arreglar un problema que acá no duele.** Un token de refresco es lo que permite que la sesión sobreviva sin volver a escribir la contraseña, y trae consigo almacenamiento, rotación y revocación — o sea, la tabla de sesiones que esta decisión evitó. Ocho horas es una jornada de trabajo: el operador se loguea una vez por día.

## 4. Consecuencias

### Positivas

- **Ninguna tabla nueva y ninguna migración.** La Fase 2 entera se construye sobre el esquema que ya existe.
- **La revocación de una persona es inmediata**, que es la propiedad que ADR-0011 necesitaba y la que suele perderse al elegir JWT.
- **Sirve a cualquier frontend, en cualquier origen**, sin que el otro equipo tenga que configurar nada más que un header.
- **La costura desaparece.** `auth.go` deja de tener un comentario que avisa que no es un mecanismo.

### Negativas / costos asumidos

- **Un token robado vale hasta que expire, y no hay forma de matarlo.** Sin estado del lado del servidor no hay lista de revocación: se puede desactivar al operador —y eso sí corta—, pero no invalidar un token puntual sin desactivar a la persona. Las ocho horas y HTTPS acotan la ventana; acotar no es controlar, y esto es lo que se paga por no tener tabla de sesiones.
- **El *logout* es una promesa del cliente.** Si el frontend no borra el token, la sesión sigue viva aunque el usuario haya apretado el botón. El servidor no tiene manera de saberlo ni de garantizarlo.
- **`JWT_SECRET` es un secreto nuevo, y rotarlo desloguea a todos a la vez.** No hay procedimiento escrito. Es el mismo costo que [ADR-0017](0017-decomposicion-en-despliegues-y-lenguajes.md) (k) declaró para `BRAIN_INTERNAL_TOKEN`, y conviene decirlo antes de que aparezca como sorpresa.
- **El JWT no ahorra la ida a la base que suele ser su argumento.** Se paga firmar y verificar, y se consulta Postgres igual en cada petición. Es deliberado —la regla 2 es lo que compra la revocación—, pero deja escrito el reverso: **si algún día ese ahorro hiciera falta, habría que meter el rol en el token y se perdería la revocación inmediata.** Es una decisión que se puede revertir, no una que se pueda tener de los dos lados.
- **El login pasa a ser el endpoint más caro del núcleo.** bcrypt con costo 12 cuesta unos cientos de milisegundos de CPU por intento, que es precisamente lo que lo hace útil contra un atacante con la base robada. También vuelve barato un ataque de agotamiento de CPU a fuerza de intentos fallidos, **y esta fase no construye ninguna limitación de intentos.**
- **Los intentos fallidos no quedan registrados en ningún lado.** `audit_logs` existe, pero [ADR-0007](0007-alcance-del-registro-de-auditoria.md) lo acota a cambios de configuración del agente IA, y ampliarlo es una decisión suya, no de ésta.
- **Una dependencia directa más en el núcleo**, que hoy tiene tres.

### Lo que queda abierto

| Qué | Por qué no se resuelve acá |
|---|---|
| **Limitación de intentos o bloqueo de cuenta** | Ningún requisito de F01–F39 lo pide, y ponerlo sin decidir umbral ni ventana es inventar requisito. Sin fase asignada |
| **Política de contraseñas** — longitud mínima, cambio obligatorio, recupero | F18 no la declara. El admin de semilla es hoy el único camino de alta |
| **Rotación de `JWT_SECRET`** | Sin procedimiento |
| **CORS en el núcleo** | No hay middleware: nada lo necesitaba, porque Meta no es un navegador. El equipo de React lo va a necesitar el primer día |

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Token opaco en una tabla `sessions`** | Es la opción que compra revocación completa, incluido el logout real. Cuesta una tabla número 38 que el Anexo C no declara y que el DER no tiene, más su ciclo de vida y su limpieza. Se descarta porque **la regla 2 recupera la parte de la revocación que importa** —desactivar a una persona— sin esquema nuevo, y lo que queda afuera es invalidar un token puntual, que ningún requisito pide |
| **Cookie firmada HttpOnly** | Protege del robo por XSS, que es una ventaja real. Cuesta `SameSite=None`, `Secure`, CORS con credenciales y defensa de CSRF, coordinados con un equipo distinto sobre un frontend que todavía no existe |
| **argon2id** | Recomendación primera de OWASP, y descartada por sus tres parámetros: exigen calibrarse contra un hardware de despliegue que OE7 todavía no eligió |
| **scrypt** | Duro en memoria como argon2id, con el mismo problema de calibración y menos uso para contraseñas. Ninguna ventaja sobre los otros dos acá |
| **JWT firmado a mano con HMAC-SHA256** | Ahorra una dependencia a cambio de escribir el código donde vive `alg: none` |
| **Meter el rol dentro del token** | Ahorraría la lectura de la fila, y a cambio una baja o un cambio de rol tardarían hasta ocho horas en tener efecto. Es lo contrario de lo que ADR-0011 necesita |
| **Agregar un *refresh token*** | Trae de vuelta el almacenamiento y la rotación que la decisión evitó, para resolver una fricción —volver a loguearse una vez por jornada— que nadie reportó |
| **Delegar la autenticación en Supabase** | [ADR-0019](0019-almacenamiento-de-objetos.md) declara que Supabase se usa **exclusivamente** como almacén de objetos, y explícitamente no como proveedor de autenticación |

## 6. Relación con ADR-0017 (k), que descartó JWT

[ADR-0017](0017-decomposicion-en-despliegues-y-lenguajes.md) (k) descartó JWT para el salto núcleo↔cerebro con este argumento: *"su ventaja es propagar identidad con expiración. Acá no hay identidad que propagar"*.

**No hay contradicción: son dos fronteras distintas.** El núcleo llama al cerebro **como sí mismo**, y (k) es explícito en que la identidad del operador no viaja en ese salto. Entre el operador y el núcleo, en cambio, la identidad **es exactamente lo que hay que transportar**, y la expiración es exactamente lo que se quiere. El secreto compartido en `X-Internal-Token` sigue vigente tal cual está y esta decisión no lo toca.
