# ADR-0039 — Los componentes del prompt y su versionado

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · revisada antes del merge, tras las revisiones de diseño y de código: la regla 6 pagina el historial, la regla 10 agrega `assembled_at` y se declara la enmienda a ADR-0007 |
| **Fecha** | 2026-09-19 |
| **Etapa** | Construcción — Fase 6 (identidad del agente IA) |
| **Resuelve** | Cuántos componentes de un tipo puede tener un agente IA, quién decide su orden, qué deja cada cambio, dónde se audita y cómo una generación nombra lo que ensambló |
| **Requisitos afectados** | F01 · OE1 · OE2 |
| **Enmienda** | [ADR-0007](0007-alcance-del-registro-de-auditoria.md) §2 — los cambios de los componentes del prompt se auditan en `prompt_component_versions` y no en `audit_logs` (regla 5) |
| **Relacionada con** | [03-capa-prompts](../03-capa-prompts.md) (reglas 1 y 2), [ADR-0007](0007-alcance-del-registro-de-auditoria.md), [ADR-0015](0015-medicion-de-oe1.md), [ADR-0033](0033-secretos-de-bandejas-nullables-y-congelamiento-de-0001.md), [ADR-0036](0036-paginacion-por-cursor-en-la-bandeja.md), [ADR-0038](0038-alta-baja-y-pausa-del-agente-ia.md), [C09](../00-convenciones.md) |

---

## 1. Contexto

F01, *"Configuración modular del agente"*, pide seis componentes independientes: objetivo, personalidad, comportamiento, tono, voz de marca y reglas. Las dos tablas existen desde `0001` —`prompt_components` con el texto vigente y `prompt_component_versions` con cada estado congelado— y el cerebro ya arma el `system` con los componentes activos, en el orden de su `position` ([03-capa-prompts](../03-capa-prompts.md), regla 1). Pero hasta esta tarea los únicos componentes eran los de la semilla: el administrador no podía escribir ninguno.

Construir la API destapó tres preguntas que ni F01 ni el catálogo responden del todo:

1. **¿Cuántos componentes de un tipo puede tener un agente IA?** El índice único de `0001` sólo impide dos componentes **activos** del mismo tipo, así que admite varios inactivos: variantes que se alternan.
2. **¿Quién decide el orden?** `position` es una columna editable, pero el orden de los bloques de un prompt no es indiferente: hay reglas de *prompting* que dicen qué va primero.
3. **¿Dónde queda quién cambió qué?** [ADR-0007](0007-alcance-del-registro-de-auditoria.md) audita los cambios de configuración del agente IA en `audit_logs`, y `prompt_component_versions` ya guarda quién, qué y cuándo de cada componente.

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **Un agente IA tiene a lo sumo un componente de cada tipo**, activo o no. Cambiar el tono es escribir una versión nueva del mismo componente, no crear otro. Un segundo componente de un tipo que el agente IA ya tiene es un 409 `component_type_taken`. **La base también lo impone**: el índice único de `(ai_agent_id, type)` de `0001` pasa a cubrir todos los no borrados, y no sólo los activos |
| 2 | **`position` la envía el panel al crear el componente, y no cambia después.** El panel conoce el orden que piden las reglas de *prompting* y lo manda; la API no lo elige. Va de 1 a 32767 (400 `invalid_position`), y dos componentes de un agente IA no comparten posición (409 `component_position_taken`), con un índice único nuevo sobre `(ai_agent_id, position)` entre los no borrados. El `type` tampoco cambia después de creado. Enviar cualquiera de los dos en un `PATCH` es un 400 `unknown_field` |
| 3 | **Cada cambio del texto o del interruptor escribe una versión nueva, en la transacción del cambio** ([03-capa-prompts](../03-capa-prompts.md), regla 2). El alta escribe la versión 1. **Un guardado sin cambios no escribe nada.** El número de versión es el siguiente del componente, contado con el componente bloqueado |
| 4 | **La baja es `deleted_at`** (C09) **y escribe una última versión apagada** con quien lo dio de baja. Un componente dado de baja deja libres su tipo y su posición |
| 5 | **La auditoría de los componentes es `prompt_component_versions`**: `changed_by_operator_id`, `change_note` y `created_at`. **No se repite en `audit_logs`** |
| 6 | **Cinco rutas bajo el agente IA, sólo de `admin`**: `GET` y `POST /api/v1/ai-agents/:id/components`, `PATCH` y `DELETE …/components/:componentId`, y `GET …/components/:componentId/versions`, con el historial del más nuevo al más viejo. **El historial pagina por número de página** (`page`, `per_page`, `total`), con 20 por defecto y 100 como máximo, igual que `contacts/` |
| 7 | **El listado muestra también los componentes apagados**, ordenados por `position` y con la versión en que está cada uno. **No pagina**: es un catálogo que no crece ([ADR-0036](0036-paginacion-por-cursor-en-la-bandeja.md), regla 9, ampliada por ADR-0038) |
| 8 | **Los componentes de un agente IA dado de baja no son alcanzables** (404, igual que el agente IA). Los de uno pausado se editan igual |
| 9 | **`change_note` es opcional**, y una nota en blanco se guarda como `NULL` |
| 10 | **Cada generación nombra la versión de cada componente que ensambló**: el cerebro lee la versión más nueva junto con el texto y escribe una fila de `generation_components` por componente que entró al prompt, en el orden en que entró y en la misma transacción que la generación. Un componente en blanco no entra al prompt y, por lo tanto, no se registra. **`generations.assembled_at` guarda el instante de esa lectura**, con el reloj de la base, y es contra ese instante que OE1 decide qué componentes debían estar |

## 3. Justificación

- **Regla 1.** El panel ofrece un solo tono para escribir: editarlo es escribir un tono nuevo, y lo que se conserva del anterior es su versión. Admitir variantes inactivas dejaría dos formas de hacer lo mismo —editar el componente o crear otro y alternar— y la segunda parte la historia de un bloque en varias filas, que es justo lo que el versionado evita. El índice original de `0001` no alcanzaba para imponerlo, porque sólo miraba los activos. Por eso el índice pasa a cubrir todos los no borrados: `0001` todavía no está congelado ([ADR-0033](0033-secretos-de-bandejas-nullables-y-congelamiento-de-0001.md)), y hacerlo después exigiría una migración nueva y limpiar antes los duplicados que existieran. El servicio lo sigue chequeando, con la fila del agente IA bloqueada, para devolver un 409 con nombre en vez de un error de índice; la base es la red para lo que no pase por la API.
- **Regla 2.** Lo que se busca es que alguien sin conocimiento de *prompting* arme un prompt bien ordenado, por ejemplo con el objetivo primero. Ese orden es conocimiento del panel, que lo manda al crear. Si pudiera moverse después, el orden del prompt dependería de cada administrador y no de esas reglas. Dos componentes en la misma posición dejarían su orden librado a cómo PostgreSQL devuelva el empate, que puede cambiar de un turno al otro: por eso la base también lo impide. `position` sigue sin versionarse porque ninguna métrica mide el orden de ensamblado ([03-capa-prompts](../03-capa-prompts.md), regla 2), y fijo no tiene cambios que versionar.
- **Regla 3.** Es la regla 2 de `03-capa-prompts` llevada a código: OE2 necesita saber contra qué texto se midió cada resultado, y OE1 qué componentes debían estar (H-20). La versión se escribe en la misma transacción que el componente, así que el texto que lee el cerebro siempre es el de la versión más nueva. El número se cuenta con el componente bloqueado, y el índice único de `(prompt_component_id, version_number)` rechazaría dos iguales si igual pasara.
- **Regla 4.** La fila no tiene `deleted_by_operator_id`, así que sin esa versión nada diría quién sacó el bloque del prompt. Que sea apagada coincide con lo que la baja significa, y la consulta de OE1 ya excluye el componente desde su `deleted_at` ([05-consultas-de-metricas](../05-consultas-de-metricas.sql), OE1), así que esa versión no la cambia. Libera el tipo porque, si no, un tono dado de baja impediría escribir otro.
- **Regla 5.** Es el mismo criterio con el que [ADR-0007](0007-alcance-del-registro-de-auditoria.md) no duplicó las reasignaciones, que ya estaban en `conversation_assignments`: la tabla de versiones responde quién, qué y cuándo, y una segunda copia en `audit_logs` sería dos registros del mismo hecho que pueden divergir.
- **Regla 6.** El Anexo B dice que el Admin *"configura los componentes del prompt de los agentes"*, y ningún otro rol tiene una pantalla que los necesite ([ADR-0038](0038-alta-baja-y-pausa-del-agente-ia.md), regla 8). Van bajo el agente IA porque no existen sin él.
- **Regla 6, el historial.** [ADR-0036](0036-paginacion-por-cursor-en-la-bandeja.md), regla 9, pide que todo listado nuevo pagine, y elige por número de página lo que sólo cambia cuando alguien lo edita. El historial es eso: crece con cada guardado del administrador, nunca con la actividad del canal.
- **Regla 7.** Un componente apagado es ablación, no baja: el administrador tiene que verlo para volver a prenderlo. La versión viaja con cada componente para que el panel pueda decir en qué estado está sin pedir el historial.
- **Regla 8.** Es la regla 1 de ADR-0038 aplicada a lo que cuelga del agente IA: dado de baja, no existe para ninguna lectura. Pausado, en cambio, es justo cuando el administrador corrige un componente.
- **Regla 10.** Sin estas filas la consulta de OE1 ([05-consultas-de-metricas](../05-consultas-de-metricas.sql)) no da vacío sino **0 %**: el lado *"debía"* sale de las versiones y tiene los componentes activos, y el lado *"se ensambló"* no tiene nada. Se registra lo que entró al prompt y no lo que se leyó, porque la diferencia entre las dos cosas es justo lo que OE1 mide ([ADR-0015](0015-medicion-de-oe1.md)). Leer texto y versión en una sola consulta alcanza porque la regla 3 los escribe juntos. `assembled_at` existe porque `created_at` se escribe cuando el modelo ya respondió, segundos después de la lectura: si el administrador prende un componente en ese medio, comparar contra `created_at` lo cuenta como faltante aunque el ensamblador hizo lo correcto, y el sesgo va siempre hacia abajo. Es una columna aparte y no un `created_at` reescrito, para que `created_at` siga diciendo cuándo se escribió la fila (C06). El plan de trabajo lo ubicaba en la Fase 9, con F05; se adelanta porque OE1 es el objetivo del motor de composición y el cierre de esta fase lo prueba de punta a punta.

## 4. Consecuencias

### Positivas

- Los seis componentes de F01 quedan construidos y probados de punta a punta, contra la base real y en el smoke. **F01 sigue parcial**, como dice [ADR-0038](0038-alta-baja-y-pausa-del-agente-ia.md): la parte de *"cuándo delegar a un humano"* es `transfer_policies`.
- Cada estado de cada componente queda congelado, con quién lo dejó así y por qué: OE2 puede decir contra qué texto se midió, y OE1 qué componentes debían estar.
- El orden del prompt sigue las reglas de *prompting* del panel y no depende de cada administrador.

### Negativas / costos asumidos

- **Un componente por tipo impide alternar variantes preparadas.** Si OE2 quisiera comparar dos tonos, hay que editar el tono y volver atrás, y el historial queda como una sola línea de versiones. Es más lento que prender una variante y apagar otra.
- **Reordenar los componentes exige darlos de baja y crearlos de nuevo**, y el componente nuevo empieza su historial en la versión 1, con otro id. El historial del anterior sigue en la base, pero ya no cuelga del mismo componente.
- **"Uno por tipo" y "una posición por componente" viven en dos lugares**: el servicio los chequea para devolver un 409 con nombre, y los índices de `0001` los imponen. Cambiar la regla exige cambiar los dos.
- **Cambiar `0001` obliga a recrear la base de cada entorno** (`down -v` y la semilla). Hoy no hay datos reales que perder, y por eso se hizo ahora.
- **La versión de la baja repite el texto de la anterior.** Es una fila más, y tiene sentido sólo por quién y cuándo.
- **Cada turno escribe hasta seis filas más** en `generation_components`. Es la tabla de evidencia que crece a la par de `generations`.
- **El `1..*` de `generation_components` lo sostiene el panel, no la API.** [relaciones.md](../DER/relaciones.md) declara que toda generación ensambla al menos un componente, y el equipo decidió que así sea: algunos componentes son obligatorios y el panel los exige antes de guardar. La API no lo impide, así que un agente IA creado sin el panel, y sin componentes, genera sin escribir ninguna fila; OE1 la deja fuera del denominador (supuesto S3) y el control C1 de la consulta la cuenta. Cuáles son obligatorios lo decide el panel.
- **Queda una ventana de milisegundos que `assembled_at` no cierra.** El `created_at` de una versión es el comienzo de la transacción que la escribió, no su confirmación: una edición que empezó antes de la lectura y confirmó después tiene hora anterior a `assembled_at` y no se vio. Es un orden de milisegundos contra los segundos que cerró la columna.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Variantes inactivas del mismo tipo**, que el administrador alterna | El panel ofrece un solo texto por tipo, y la historia de un bloque se partiría en varias filas. El equipo eligió un componente por tipo |
| **`position` fija por tipo en la API**, sin que el panel la mande | Encierra en el núcleo un conocimiento de *prompting* que el equipo pidió que viva en el panel |
| **`position` editable** | El orden del prompt dependería de cada administrador y no de las reglas de *prompting* que el panel aplica |
| **Auditar también en `audit_logs`** | Duplica lo que ya guarda `prompt_component_versions`, y dos registros del mismo hecho pueden divergir. El equipo pidió no duplicarlo |
| **La baja sin versión**, con sólo `deleted_at` | Nada diría quién sacó el componente del prompt, porque la fila no tiene `deleted_by_operator_id` |
| **"Uno por tipo" sólo en el servicio** | Una escritura por SQL a mano o un error futuro dejaría dos tonos, o dos componentes en la misma posición, y el orden del prompt quedaría al azar. Después del congelamiento de `0001` costaría una migración y una limpieza |
| **`generation_components` en la Fase 9**, con F05 | Hasta entonces OE1 daría 0 % sobre cada turno real, y el cierre de esta fase no podría probar el ensamblador de punta a punta |
