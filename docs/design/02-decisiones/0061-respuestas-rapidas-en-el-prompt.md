# ADR-0061 — Las respuestas rápidas en el prompt: cuáles llegan al modelo, dónde y cómo las usa

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · que entren todas las activas, hasta un tope (regla 2), y que *"sugerir"* sea el borrador del modo sugerencia (regla 7) los eligió el grupo el 2026-10-02 |
| **Fecha** | 2026-10-02 |
| **Etapa** | Construcción — Fase 11 (resto del CRUD operativo) |
| **Resuelve** | Cómo se construye F07: qué respuestas rápidas lee el cerebro en cada turno, en qué parte del prompt entran, qué se le dice al modelo para que las use y qué significa *"sugerir"* |
| **Requisitos afectados** | F07 · F09 · F02 · F03 · OE1 |
| **Enmienda** | [03-capa-prompts.md](../03-capa-prompts.md), **Regla 1** — el `user` pasa de cuatro secciones a cinco: las respuestas rápidas entran entre la base de conocimiento y las intenciones |
| **Relacionada con** | [ADR-0040](0040-previsualizacion-del-prompt.md), [ADR-0046](0046-modo-sugerencia.md), [ADR-0060](0060-respuestas-rapidas.md) |

---

## 1. Contexto

F07 dice *"El agente IA puede usar o sugerir las respuestas rápidas precargadas por el administrador"*. [ADR-0060](0060-respuestas-rapidas.md) construyó el catálogo de F09: lo carga el `admin`, los operadores lo eligen desde el chat, y una respuesta pausada deja de verse. [ADR-0017](0017-decomposicion-en-despliegues-y-lenguajes.md) le asignó F07 al cerebro, que lee la tabla del núcleo sin escribirla.

F07 no dice cuáles respuestas llegan al modelo, ni dónde entran en el prompt, ni qué distingue *"usar"* de *"sugerir"*. La Regla 1 de [03-capa-prompts.md](../03-capa-prompts.md) fija las secciones del `user`, y no tenía lugar para ellas.

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **El cerebro lee el catálogo en cada turno**, junto con los componentes del agente, y no lo guarda en memoria: una respuesta que el `admin` pausa sale del prompt siguiente. Lee las de la cuenta del agente que estén activas y no borradas, que es lo mismo que ven los operadores ([ADR-0060](0060-respuestas-rapidas.md), regla 3) |
| 2 | **Entran todas, hasta un tope**, ordenadas por título. Elegido por el grupo el 2026-10-02. El tope es `quick_replies_limit` en `rules/generation.yaml`, hoy 30. Si el catálogo lo supera, entran las primeras por título y el cerebro deja un `warning` en el log |
| 3 | **Entran en el `user`, en una sección propia** —`[RESPUESTAS RÁPIDAS DE LA EMPRESA]`—, después de la base de conocimiento y antes de las intenciones. Es la enmienda a la Regla 1 |
| 4 | **Cada respuesta va numerada, con su título arriba de su contenido**, y el contenido conserva sus saltos de línea |
| 5 | **Sin catálogo no hay sección**: una cuenta sin respuestas no imprime el encabezado, como el resto del `user` (Regla 1) |
| 6 | **El cierre fijo del `system` le dice al modelo cómo usarlas**: si una contesta lo que pide el cliente, usa su contenido como base y lo adapta a la conversación; si ninguna viene al caso, no la nombra. `generation.yaml` pasa a la versión 3 (regla 9 de [08-structure-rules.md](../08-structure-rules.md)) |
| 7 | ***"Sugerir"* es el borrador del modo sugerencia** ([ADR-0046](0046-modo-sugerencia.md)): la respuesta generada con el catálogo en el prompt le llega al operador para copiarla. Elegido por el grupo el 2026-10-02. El modelo no informa qué respuesta usó, y el contrato no cambia |
| 8 | **La previsualización del prompt no cambia** ([ADR-0040](0040-previsualizacion-del-prompt.md)): muestra el `system`, y las respuestas viven en el `user` |

## 3. Justificación

- **Todas y no las relevantes (regla 2)** porque elegir por relevancia necesita embeddings y la recuperación de la Fase 12, que no existe todavía. El catálogo de una cuenta es chico, y el modelo distingue cuál viene al caso mejor que una búsqueda por texto. El tope existe para que un catálogo que crece no encarezca todos los turnos.
- **En el `user` y no en el `system` (regla 3).** Las respuestas son material que la empresa escribió de antemano, como la base de conocimiento, y la Regla 1 pone ese material en el `user`. Si la Fase 12 empieza a elegirlas por relevancia, van a cambiar en cada turno, y en el `user` ese cambio no toca nada más. Van antes de las intenciones porque la Regla 1 deja al final lo que el modelo tiene que tener fresco: lo que pidió el cliente y su mensaje.
- **La instrucción en el cierre (regla 6)** porque es fija, como la de contestar todas las intenciones: en el `user` se repetiría en cada turno.
- **Leer en cada turno (regla 1)** porque pausar una respuesta tiene que surtir efecto en el turno siguiente, y una consulta más con la misma conexión cuesta milisegundos.
- **Sugerir sin un mecanismo propio (regla 7)** porque el modo sugerencia ya pone el borrador del modelo delante del operador, y en ese borrador el modelo ya usó el catálogo. Que además nombre la respuesta elegida exigiría cambiar la salida del modelo, el contrato y el esquema, para un dato que ningún requisito pide medir.

## 4. Consecuencias

### Positivas

- F07 existe: el agente contesta con los textos que la empresa ya aprobó, y en modo sugerencia el operador recibe un borrador basado en ellos.
- El catálogo que ve el modelo es el mismo que ven los operadores, sin una segunda regla de qué está activo.
- `generations.prompt_rendered` guarda la sección, así que cada generación muestra qué respuestas tenía el modelo a mano.

### Negativas / costos asumidos

- **Cada turno paga los tokens del catálogo entero**, venga o no al caso, y en el `user` no se aprovecha el caché de prompt que la Regla 1 protege en el `system`. Con el tope en 30 y textos cortos es acotado, pero crece con el catálogo.
- **Si el catálogo supera el tope, entran las primeras por título.** Las demás no llegan al modelo, y sólo el log lo dice: el `admin` no lo ve en el panel.
- **El tope cuenta respuestas, no tokens.** Treinta respuestas largas pueden pesar más que lo previsto.
- **La Regla 5 de `03-capa-prompts` no las menciona.** Cuando se construya el recorte por presupuesto de tokens, hay que decidir si se recortan y en qué orden.
- **No se puede medir qué respuesta usó el modelo** (regla 7). Sólo se ve comparando el texto generado con el catálogo, a mano.
- **El modelo puede copiar una respuesta que no corresponde**, o mezclar dos. La instrucción del cierre está en lenguaje natural y ningún código la hace cumplir, igual que el resto del andamiaje.
- **Una respuesta pensada para el operador llega igual al modelo.** El catálogo no distingue respuestas para el bot de respuestas para personas, y F09 no lo pide.
- **El cambio de versión de `generation.yaml` separa las mediciones.** Las generaciones anteriores se armaron con la versión 2 y no son comparables una a una con las nuevas (regla 9 de `08-structure-rules`).

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Elegir las relevantes por similitud** | Necesita embeddings y la recuperación de la Fase 12, que no existen |
| **Ponerlas en el `system`** | Se aprovecharía el caché, pero la Regla 1 pone en el `user` lo que la empresa escribió de antemano, y una selección por turno de la Fase 12 obligaría a moverlas de nuevo |
| **Guardar el catálogo en memoria del cerebro** | Una respuesta pausada seguiría llegando al modelo hasta que se renovara la memoria |
| **Que el modelo devuelva qué respuesta eligió** | Cambia la salida del modelo, el contrato y el esquema, para un dato que ningún requisito mide |
| **Sin tope** | Un catálogo grande encarece todos los turnos sin que nadie lo note |
