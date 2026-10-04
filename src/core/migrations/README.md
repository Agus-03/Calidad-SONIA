# Migraciones — el único dueño del esquema

Esta carpeta es el **único lugar del sistema que hace DDL** (regla 3 de `CLAUDE.md`). El cerebro escribe filas y nunca tablas; el frontend no toca la base.

Los `.sql` viajan **dentro del binario** (`embed.go`), así que el núcleo y su esquema no pueden ser dos versiones distintas.

---

## Cómo se aplican

**Solas, al arrancar el núcleo.** `app.Run` llama al corredor después de abrir el pool y antes de servir el primer request. No hay un paso manual que alguien pueda olvidar.

El corredor —`internal/platform/db/migrate.go`— hace cuatro cosas:

1. Toma un *advisory lock* de PostgreSQL, para que dos arranques simultáneos no apliquen el mismo archivo dos veces.
2. Crea `schema_migrations` si no existe: `version`, `checksum`, `applied_at`.
3. Aplica lo pendiente **en orden de nombre**, cada archivo en su propia transacción. Un archivo que falla a la mitad no deja ni su DDL ni su fila.
4. Verifica el checksum de lo ya aplicado. Si un archivo cambió después de haberse corrido, **el proceso no levanta**.

Ese último punto es a propósito: significa que la base y el repositorio dicen cosas distintas, y arrancar igual dejaría al núcleo escribiendo contra un esquema que nadie sabe cuál es.

## Cómo se nombra un archivo

```
NNNN_descripcion_en_ingles.sql
```

El prefijo numérico **es el orden**. Cuatro dígitos, correlativo, sin saltos.

## La regla que caduca: `0001` es reescribible hasta que se conecte la primera WABA real

Mientras no haya datos que le importen a nadie, corregir `0001_initial_schema.sql` y **recrear la base** es mejor que arrastrar una migración correctiva desde el primer commit.

- **Hasta la primera WABA real:** si el esquema estaba mal, se edita `0001`, se baja la base con su volumen (`docker compose down -v`) y se vuelve a levantar. El corredor va a frenar el arranque avisando que el archivo cambió — eso es la señal de que hay que recrear, no un error.
- **Desde que se conecta la primera WABA real:** entran datos reales del cooperante. A partir de ahí `0001` es inmutable y **todo cambio va en un archivo nuevo**.

**El vencimiento se corrió.** Esta regla decía "hasta la Fase 4", porque se suponía que los datos reales llegaban con el canal. En la Fase 4 el canal se construyó y todavía no hay número del cooperante, así que el equipo decidió (2026-09-12) atar la regla al hecho que protege y no al número de fase. Congelar antes no protege ningún dato y obliga a hacer migraciones correctivas sobre una base que se puede recrear.

El motivo de escribirlo acá y no confiar en la memoria: es una regla con fecha de vencimiento, y las reglas con fecha de vencimiento que viven en la cabeza de alguien se aplican mal en las dos direcciones.

## Qué NO va acá

| Cosa | Dónde va |
|---|---|
| Datos de demo, cuentas de prueba, un agente de ejemplo | `../seeds/` — idempotentes, corridos por el `Makefile`, **nunca** por la aplicación al arrancar |
| Listas de bloqueo, reglas del guardrail, prompts internos | `src/brain/rules/` — YAML versionado (regla 9) |
| Los seis componentes del prompt del agente | La tabla `prompt_components`: los edita el admin (F01) |

## Convenciones que el DDL respeta

- **C04** — `id uuid PRIMARY KEY` **sin `DEFAULT`**: el UUID v7 lo genera la aplicación con `platform/id`.
- **C05** — ninguna FK tiene `ON DELETE CASCADE`. Es incompatible con el borrado lógico y arrastraría el historial que F32 exige preservar.
- **C06** — todo instante es `timestamptz`, y lo escribe `now()` de PostgreSQL: es el único reloj que comparten los tres escritores.
- **C07** — las 22 enumeraciones son `varchar` + `CHECK`, **nunca** un `ENUM` nativo.
- **C08** — dinero en `numeric`, jamás punto flotante. `sales.amount` con escala 2; `generations.cost` con escala 6, porque el costo por turno es una fracción de centavo.
- **C09** — `deleted_at` por defecto. Las excepciones están declaradas: `messages` no admite borrado de ninguna clase, y `contact_labels` y `conversation_labels` admiten borrado físico.

## Lo que el esquema no trae, a propósito

**Índices de rendimiento.** Salvo los que el diseño declara como invariantes —los únicos parciales de `relaciones.md` §9—, no hay índices. PostgreSQL no indexa las claves foráneas solo, así que van a hacer falta; se agregan **cuando haya una consulta real y lenta que los justifique**, no antes. Adivinar índices sobre un esquema sin datos produce índices que nadie usa y que igual hay que mantener.

**Excepción declarada: el orden de la bandeja** ([ADR-0036](../../../docs/design/02-decisiones/0036-paginacion-por-cursor-en-la-bandeja.md), regla 10). `idx_conversations_last_activity_at_id` e `idx_messages_conversation_id_created_at_id` entraron antes de tener una consulta lenta, porque la consulta existe y corre en cada carga de la bandeja, y porque después de congelar `0001` no se pueden construir con `CONCURRENTLY`.

**Excepción declarada: el temporizador del turno** ([ADR-0041](../../../docs/design/02-decisiones/0041-temporizador-del-turno-por-poller.md), regla 4). `idx_message_batches_expires_at` entra sin una consulta lenta que lo justifique, por el mismo motivo que las otras excepciones: la consulta existe, **corre una vez por segundo** mientras el núcleo esté levantado, y recorre una tabla que crece con cada turno. Es parcial sobre los bloques abiertos —los cerrados son todos menos uno por conversación— así que indexa una fracción mínima de la tabla.

**Excepción declarada: contar los mensajes de un bloque** ([ADR-0041](../../../docs/design/02-decisiones/0041-temporizador-del-turno-por-poller.md), reglas 4 y 5). `idx_messages_message_batch_id` es la contrapartida de haber decidido que `messages_count` **no se mantiene** mientras el bloque está abierto: si el número no vive en una columna, hay que contarlo, y se cuenta dos veces por mensaje —al agregarlo, para saber si llegó al tope, y al cerrar el bloque—. Sin índice eso es un recorrido completo de `messages`, que es la tabla que más crece del esquema. Es parcial sobre los mensajes que pertenecen a algún bloque: una respuesta del AIAgent no pertenece a ninguno, y es la mitad de lo que se escribe ahí.

A diferencia del anterior, **este índice no lo nombra ninguna regla de ADR-0041**, y no se le agrega una: las reglas de un ADR no se editan. El ADR decidió dónde vive el contador; el índice es lo que cuesta esa decisión, y el lugar de eso es este archivo. Entró el 2026-09-19, mientras `0001` todavía se puede reescribir.

**Excepción declarada: el reparto** ([ADR-0037](../../../docs/design/02-decisiones/0037-toda-conversacion-nace-con-vendedor.md), regla 8). `idx_conversations_contact_id_created_at_id` e `idx_conversation_assignments_to_operator_id_created_at_id` entraron por el mismo motivo: las dos consultas del reparto existen y corren con cada conversación nueva, dentro de la transacción del webhook.

**Excepción declarada: la búsqueda de F06** ([ADR-0073](../../../docs/design/02-decisiones/0073-busqueda-semantica.md), nota). `idx_documents_knowledge_base_id` e `idx_document_chunks_document_id` entraron el 2026-10-04 sin una consulta lenta medida, a pedido de la revisión de la búsqueda y por decisión del grupo. La consulta existe y corre en **cada turno que recupera**: lee los documentos vivos de la base del agente y, por cada uno, si tiene trozos vivos de otro modelo. `document_chunks` sólo crece, porque los trozos retirados se conservan como evidencia de OE6, así que los dos índices son parciales sobre las filas vivas. El de los trozos sirve además al `worker`, que los retira y barre por documento.
