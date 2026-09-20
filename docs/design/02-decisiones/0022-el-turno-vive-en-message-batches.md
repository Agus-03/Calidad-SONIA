# ADR-0022 — El número de turno vive en `message_batches`

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · §4 enmendada por [ADR-0041](0041-temporizador-del-turno-por-poller.md), regla 1: el bloque es inmutable **una vez cerrado**, no desde que se abre — mientras está abierto, su `expires_at` se reinicia con cada mensaje. `turn_number` sigue escribiéndose una sola vez, al abrir |
| **Fecha** | 2026-09-04 |
| **Etapa** | Diseño — Paso 6 (pendiente abierto por ADR-0014) |
| **Resuelve** | **H-19** — `message_batches` no tiene `turn_number` pese a ser el turno |
| **Requisitos afectados** | F10 · OE2, OE3, OE4 |

---

## 1. Contexto

`03-capa-prompts.md` define el **turno** como el bloque cerrado y procesado — es decir, una fila de `message_batches`. Pero esa tabla **no tiene el número de turno**, y sí lo tienen `conversation_contexts`, `conversation_context_versions` y `transfer_events`, cada una calculándolo por su cuenta.

El resultado es que la entidad que **es** el turno no puede responder "¿qué turno fui?", y dos tablas mantienen el mismo número por caminos independientes. En algún momento discrepan sobre cuál fue el turno 5 — y las consultas de OE2, OE3 y OE4 agrupan por turno.

## 2. Decisión

Se agrega **`turn_number` (integer) a `message_batches`**, correlativo por conversación. Es la **fuente de verdad** del número de turno.

Las otras tres tablas conservan su columna, pero pasa a ser una **copia escrita desde el bloque**, nunca un cálculo propio. Se declara como regla de aplicación.

La salida completa —que las tres alcancen el bloque por FK y se borren sus copias— **queda diferida al Paso 7**, porque `conversation_contexts` hoy no tiene ningún camino a `message_batches` y abrirlo es una decisión de modelo físico.

## 3. Justificación

- **El dato pertenece a la entidad que materializa el concepto.** El turno es el bloque; el número del turno va en el bloque.
- **Elimina el cálculo duplicado**, que es la causa de que dos tablas puedan discrepar. Que sigan siendo copias es peor que una FK, pero mucho mejor que dos derivaciones independientes.
- **Es una columna y no hay datos encima.** Agregarla ahora es gratis; agregarla con la Fase 4 corriendo obliga a reconstruir el número de turno de cada bloque histórico.

## 4. Consecuencias

### Positivas

- Existe una fuente de verdad única del número de turno.
- `message_batches` deja de ser la única de las cuatro tablas que no puede decir a qué turno corresponde.
- Las consultas de métricas que agrupan por turno tienen de dónde leerlo sin reconstruirlo.

### Negativas / costos asumidos

- **Siguen existiendo cuatro columnas con el mismo número.** La consistencia pasa a ser una regla de aplicación —"se copia del bloque, no se calcula"— y no una garantía del modelo. Es una denormalización deliberada, y como tal hay que sostenerla a mano.
- **La solución limpia queda diferida**, así que este ADR mejora el problema sin cerrarlo. Si el Paso 7 no lo retoma, la regla se va a violar tarde o temprano.
- Un atributo más en una tabla del bloque inmutable — no rompe la inmutabilidad, porque se escribe una sola vez al abrir el bloque.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Dejarlo como está** | Dos tablas calculan el mismo número por caminos separados. La discrepancia no es hipotética: es cuestión de tiempo, y afecta a tres objetivos. |
| **FK a `message_batches` desde las otras tres y borrar sus columnas** | Es la solución correcta, pero `conversation_contexts` no tiene camino a `message_batches` y abrirlo es una decisión de modelo físico. Diferida al Paso 7, no descartada. |
| **Derivarlo por conteo en cada consulta** (`ROW_NUMBER` sobre los bloques de la conversación) | Evita la columna pero mete la definición del turno adentro de cada consulta, donde nadie la ve y cada una puede escribirla distinto. |
