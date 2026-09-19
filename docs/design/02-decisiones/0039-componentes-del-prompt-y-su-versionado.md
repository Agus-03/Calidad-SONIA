# ADR-0039 — Los componentes del prompt y su versionado

| Campo | Valor |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 2026-09-19 |
| **Etapa** | Construcción — Fase 6 (identidad del agente IA) |
| **Resuelve** | Cuántos componentes de un tipo puede tener un agente IA, quién decide su orden, qué deja cada cambio y dónde se audita |
| **Requisitos afectados** | F01 · OE1 · OE2 |
| **Relacionada con** | [03-capa-prompts](../03-capa-prompts.md) (reglas 1 y 2), [ADR-0007](0007-alcance-del-registro-de-auditoria.md), [ADR-0015](0015-medicion-de-oe1.md), [ADR-0033](0033-secretos-de-bandejas-nullables-y-congelamiento-de-0001.md), [ADR-0036](0036-paginacion-por-cursor-en-la-bandeja.md), [ADR-0038](0038-alta-baja-y-pausa-del-agente-ia.md), [C09](../00-convenciones.md) |

---

## 1. Contexto

F01 pide *"configuración modular del prompt"* con seis componentes: objetivo, personalidad, comportamiento, tono, voz de marca y reglas. Las dos tablas existen desde `0001` —`prompt_components` con el texto vigente y `prompt_component_versions` con cada estado congelado— y el cerebro ya arma el `system` con los componentes activos, en el orden de su `position` ([03-capa-prompts](../03-capa-prompts.md), regla 1). Pero hasta esta tarea los únicos componentes eran los de la semilla: el administrador no podía escribir ninguno.

Construir la API destapó tres preguntas que ni F01 ni el catálogo responden del todo:

1. **¿Cuántos componentes de un tipo puede tener un agente IA?** El índice único de `0001` sólo impide dos componentes **activos** del mismo tipo, así que admite varios inactivos: variantes que se alternan.
2. **¿Quién decide el orden?** `position` es una columna editable, pero el orden de los bloques de un prompt no es indiferente: hay reglas de *prompting* que dicen qué va primero.
3. **¿Dónde queda quién cambió qué?** [ADR-0007](0007-alcance-del-registro-de-auditoria.md) audita los cambios de configuración del agente IA en `audit_logs`, y `prompt_component_versions` ya guarda quién, qué y cuándo de cada componente.

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **Un agente IA tiene a lo sumo un componente de cada tipo**, activo o no. Cambiar el tono es escribir una versión nueva del mismo componente, no crear otro. Un segundo componente de un tipo que el agente IA ya tiene es un 409 `component_type_taken` |
| 2 | **`position` la envía el panel al crear el componente, y no cambia después.** El panel conoce el orden que piden las reglas de *prompting* y lo manda; la API no lo elige. Va de 1 a 32767 (400 `invalid_position`), y dos componentes de un agente IA no comparten posición (409 `component_position_taken`). El `type` tampoco cambia después de creado. Enviar cualquiera de los dos en un `PATCH` es un 400 `unknown_field` |
| 3 | **Cada cambio del texto o del interruptor escribe una versión nueva, en la transacción del cambio** ([03-capa-prompts](../03-capa-prompts.md), regla 2). El alta escribe la versión 1. **Un guardado sin cambios no escribe nada.** El número de versión es el siguiente del componente, contado con el componente bloqueado |
| 4 | **La baja es `deleted_at`** (C09) **y escribe una última versión apagada** con quien lo dio de baja. Un componente dado de baja deja libres su tipo y su posición |
| 5 | **La auditoría de los componentes es `prompt_component_versions`**: `changed_by_operator_id`, `change_note` y `created_at`. **No se repite en `audit_logs`** |
| 6 | **Cinco rutas bajo el agente IA, sólo de `admin`**: `GET` y `POST /api/v1/ai-agents/:id/components`, `PATCH` y `DELETE …/components/:componentId`, y `GET …/components/:componentId/versions`, con el historial del más nuevo al más viejo |
| 7 | **El listado muestra también los componentes apagados**, ordenados por `position` y con la versión en que está cada uno. **No pagina**: es un catálogo que no crece ([ADR-0036](0036-paginacion-por-cursor-en-la-bandeja.md), regla 9, ampliada por ADR-0038) |
| 8 | **Los componentes de un agente IA dado de baja no son alcanzables** (404, igual que el agente IA). Los de uno pausado se editan igual |
| 9 | **`change_note` es opcional**, y una nota en blanco se guarda como `NULL` |

## 3. Justificación

- **Regla 1.** El panel ofrece un solo tono para escribir: editarlo es escribir un tono nuevo, y lo que se conserva del anterior es su versión. Admitir variantes inactivas dejaría dos formas de hacer lo mismo —editar el componente o crear otro y alternar— y la segunda parte la historia de un bloque en varias filas, que es justo lo que el versionado evita. El índice de `0001` no alcanza para imponerlo, porque sólo mira los activos: está pensado para la ablación ([relaciones.md](../DER/relaciones.md) §9). Por eso, en esta tarea, la regla vive en el servicio: el alta bloquea la fila del agente IA, así que dos altas del mismo tipo al mismo tiempo se ordenan y la segunda ve a la primera. Llevarla también a la base es posible —`0001` todavía no está congelado ([ADR-0033](0033-secretos-de-bandejas-nullables-y-congelamiento-de-0001.md))—, pero cambia un índice que el catálogo documenta, y queda como decisión aparte.
- **Regla 2.** Lo que se busca es que alguien sin conocimiento de *prompting* arme un prompt bien ordenado, por ejemplo con el objetivo primero. Ese orden es conocimiento del panel, que lo manda al crear. Si pudiera moverse después, el orden del prompt dependería de cada administrador y no de esas reglas. Dos componentes en la misma posición dejarían su orden librado a cómo PostgreSQL devuelva el empate. `position` sigue sin versionarse porque ninguna métrica mide el orden de ensamblado ([03-capa-prompts](../03-capa-prompts.md), regla 2), y fijo no tiene cambios que versionar.
- **Regla 3.** Es la regla 2 de `03-capa-prompts` llevada a código: OE2 necesita saber contra qué texto se midió cada resultado, y OE1 qué componentes debían estar (H-20). La versión se escribe en la misma transacción que el componente, así que el texto que lee el cerebro siempre es el de la versión más nueva. El número se cuenta con el componente bloqueado, y el índice único de `(prompt_component_id, version_number)` rechazaría dos iguales si igual pasara.
- **Regla 4.** La fila no tiene `deleted_by_operator_id`, así que sin esa versión nada diría quién sacó el bloque del prompt. Que sea apagada coincide con lo que la baja significa, y la consulta de OE1 ya excluye el componente desde su `deleted_at` ([05-consultas-de-metricas](../05-consultas-de-metricas.sql), OE1), así que esa versión no la cambia. Libera el tipo porque, si no, un tono dado de baja impediría escribir otro.
- **Regla 5.** Es el mismo criterio con el que [ADR-0007](0007-alcance-del-registro-de-auditoria.md) no duplicó las reasignaciones, que ya estaban en `conversation_assignments`: la tabla de versiones responde quién, qué y cuándo, y una segunda copia en `audit_logs` sería dos registros del mismo hecho que pueden divergir.
- **Regla 6.** El Anexo B dice que el Admin *"configura los componentes del prompt de los agentes"*, y ningún otro rol tiene una pantalla que los necesite ([ADR-0038](0038-alta-baja-y-pausa-del-agente-ia.md), regla 8). Van bajo el agente IA porque no existen sin él.
- **Regla 7.** Un componente apagado es ablación, no baja: el administrador tiene que verlo para volver a prenderlo. La versión viaja con cada componente para que el panel pueda decir en qué estado está sin pedir el historial.
- **Regla 8.** Es la regla 1 de ADR-0038 aplicada a lo que cuelga del agente IA: dado de baja, no existe para ninguna lectura. Pausado, en cambio, es justo cuando el administrador corrige un componente.

## 4. Consecuencias

### Positivas

- F01 queda construido y probado de punta a punta, contra la base real y en el smoke.
- Cada estado de cada componente queda congelado, con quién lo dejó así y por qué: OE2 puede decir contra qué texto se midió, y OE1 qué componentes debían estar.
- El orden del prompt sigue las reglas de *prompting* del panel y no depende de cada administrador.

### Negativas / costos asumidos

- **Un componente por tipo impide alternar variantes preparadas.** Si OE2 quisiera comparar dos tonos, hay que editar el tono y volver atrás, y el historial queda como una sola línea de versiones. Es más lento que prender una variante y apagar otra.
- **Reordenar los componentes exige darlos de baja y crearlos de nuevo**, y el componente nuevo empieza su historial en la versión 1, con otro id. El historial del anterior sigue en la base, pero ya no cuelga del mismo componente.
- **"Uno por tipo" no lo impone la base, por ahora.** Una escritura por SQL a mano puede dejar dos componentes inactivos del mismo tipo; la API no los crea, pero tampoco los repara. Queda pendiente decidir si el índice de `0001` pasa a cubrirlo.
- **La versión de la baja repite el texto de la anterior.** Es una fila más, y tiene sentido sólo por quién y cuándo.
- **Nada escribe todavía `generation_components`.** Las versiones existen, pero ninguna generación nombra cuál ensambló, así que el lado *"se ensambló"* de OE1 sigue vacío. No es parte de esta tarea.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Variantes inactivas del mismo tipo**, que el administrador alterna | El panel ofrece un solo texto por tipo, y la historia de un bloque se partiría en varias filas. El equipo eligió un componente por tipo |
| **`position` fija por tipo en la API**, sin que el panel la mande | Encierra en el núcleo un conocimiento de *prompting* que el equipo pidió que viva en el panel |
| **`position` editable** | El orden del prompt dependería de cada administrador y no de las reglas de *prompting* que el panel aplica |
| **Auditar también en `audit_logs`** | Duplica lo que ya guarda `prompt_component_versions`, y dos registros del mismo hecho pueden divergir. El equipo pidió no duplicarlo |
| **La baja sin versión**, con sólo `deleted_at` | Nada diría quién sacó el componente del prompt, porque la fila no tiene `deleted_by_operator_id` |
