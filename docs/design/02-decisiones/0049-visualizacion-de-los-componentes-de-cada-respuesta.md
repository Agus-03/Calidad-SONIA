# ADR-0049 — La visualización del razonamiento: qué componentes se muestran de cada respuesta, y a quién el texto

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · **§2 enmendado por [ADR-0063](0063-paquetes-por-razon-de-cambio.md)** (2026-10-02): F05 vive en `reasoning/`, que pide el alcance del hilo a `inbox/` |
| **Fecha** | 2026-09-26 |
| **Etapa** | Construcción — Fase 9 (contexto dinámico y transferencia) |
| **Resuelve** | Qué significa *"influyeron"* en F05, qué ve cada rol de los componentes de una respuesta, cómo se pide esa lectura, y qué pasa con `generations.reasoning_json` |
| **Requisitos afectados** | F05 · F01 · OE1 · Anexo B (roles) |
| **Naturaleza** | F05 dice *"El operador ve qué componentes del prompt influyeron en cada respuesta generada"*. **No dice qué es influir ni si el operador lee el texto de cada componente.** §2.1 es una lectura del DER; §2.2 es una decisión del equipo que **restringe la letra de F05** y se declara así |
| **Enmienda** | [DER/entidades.md](../DER/entidades.md), **`generations.reasoning_json`** — queda sin uso: la evidencia de F05 es `generation_components` |
| **Relacionada con** | [ADR-0015](0015-medicion-de-oe1.md), [ADR-0017](0017-decomposicion-en-despliegues-y-lenguajes.md), [ADR-0035](0035-reglas-de-la-bandeja-manual.md), [ADR-0039](0039-componentes-del-prompt-y-su-versionado.md), [ADR-0046](0046-modo-sugerencia.md) |

---

## 1. Contexto

Desde la Fase 6, cada generación deja en `generation_components` la **versión exacta** de cada componente que se ensambló, con su posición. El DER le da a esa tabla un solo origen: F05. Lo que faltaba era la lectura: nada del núcleo mostraba esas filas.

F05 deja abiertas dos preguntas. La primera es qué significa que un componente *"influyó"*: el modelo no dice qué parte del prompt pesó en cada respuesta. La segunda es si el Operator ve el **texto** de los componentes, que escribe el administrador en la configuración del AIAgent y que hasta ahora sólo el administrador veía.

Además, el DER tiene una columna `generations.reasoning_json` con origen F05 que nadie escribe.

## 2. Decisión

### 2.1 *"Influyeron"* son los componentes que se ensamblaron

Una respuesta mostró los componentes que estaban en su prompt, **en la versión que se usó**, en el orden en que entraron. Un componente editado o dado de baja después se sigue mostrando como era: la versión es la que se ensambló (C09, [ADR-0039](0039-componentes-del-prompt-y-su-versionado.md)).

### 2.2 El texto de los componentes es sólo del administrador

| Rol | Ve |
|---|---|
| Operator | Tipo, posición y número de versión de cada componente |
| Supervisor | Lo mismo que el Operator |
| Admin | Lo mismo, **más el texto** de cada versión |

El administrador es quien tiene acceso a la configuración del AIAgent —el Anexo B le da, y sólo a él, *"gestiona agentes IA"*—, y los componentes son esa configuración. En la respuesta de los otros dos roles **la clave del texto no aparece**, ni siquiera como `null`.

**Los tres roles leen con el alcance del hilo** ([ADR-0035](0035-reglas-de-la-bandeja-manual.md), regla 2): lo que no pueden leer en la bandeja, tampoco pueden consultarlo acá, y se les responde que la conversación no existe.

### 2.3 Se pide por generación, y el hilo dice cuál

- **`GET /conversations/:id/generations/:generation_id/components`**, para los tres roles. Una generación que no es de esa conversación responde 404 `generation_not_found`.
- **Por generación y no por mensaje**, porque una sugerencia en `pending_review` tiene generación y no tiene mensaje ([ADR-0046](0046-modo-sugerencia.md)).
- **El hilo agrega `generation_id` a cada mensaje** que tiene una generación detrás: la respuesta del AIAgent, y la del Operator que mandó una sugerencia. Los demás lo llevan en `null`. Es lo que le dice al frontend qué puede consultar.
- ~~**Vive en `inbox/`**~~ *(enmendado por [ADR-0063](0063-paquetes-por-razon-de-cambio.md): vive en `reasoning/`, con el alcance del hilo que exporta `inbox/`)*, como anticipaba el reparto de [ADR-0017](0017-decomposicion-en-despliegues-y-lenguajes.md): el cerebro escribe y el núcleo sirve.

### 2.4 `reasoning_json` queda sin uso

La evidencia de F05 es `generation_components`, que ya se escribe. **La columna queda en el esquema y siempre en `NULL`**: sacarla es una migración que no cambia nada de lo que F05 muestra.

## 3. Justificación

**Por qué leer *"influyeron"* como *"se ensamblaron"*.** Es lo único que el sistema sabe con certeza. Decir cuál componente pesó más exigiría una atribución que ningún proveedor da, y que el sistema tendría que inventar. Además, es lo que el DER ya había decidido al darle a `generation_components` origen F05.

**Por qué el texto es del admin.** Es la decisión del grupo, y tiene un fundamento en el anteproyecto: el Anexo B le da la gestión de los agentes IA al administrador y a nadie más. Mostrarle el texto al Operator por esta vía abriría una segunda puerta a una configuración que en el panel tiene cerrada.

**Por qué el supervisor va con el Operator.** La decisión nombró sólo al admin. El supervisor ve toda la conversación del equipo, pero no la configuración del agente.

**Por qué la clave falta en lugar de ir en `null`.** Un `null` dice *"este componente no tiene texto"*, y eso es falso. La ausencia no dice nada.

## 4. Consecuencias

### Positivas

- **F05 queda construido** sobre evidencia que ya se escribía, sin tocar el cerebro ni el esquema.
- **El admin puede auditar respuesta por respuesta** con qué configuración se produjo, incluso después de editarla.
- **Una sola ruta sirve a los tres roles**: lo que cambia por rol lo decide el servicio, que es donde viven las otras reglas de alcance.

### Negativas / costos asumidos

- **F05 le explica poco al Operator.** Ve que se usaron el objetivo, el tono y las reglas, pero no qué dicen. Para entender una respuesta rara tiene que preguntarle al admin. **Es una lectura restrictiva de F05**, que habla del *"operador"*, y hay que declararla en el informe.
- **"Se ensambló" no es "influyó".** Un componente que el modelo ignoró aparece igual que uno que decidió la respuesta. F05 muestra qué se le dio al modelo, no por qué respondió así.
- **`reasoning_json` queda en el esquema sin uso**: es una columna que el DER atribuye a F05 y que nunca se llena.
- **El contexto dinámico, las intenciones y los fragmentos de RAG no forman parte de esta lectura**, aunque también estaban en el prompt. F05 habla de *"componentes"*, y los componentes son los seis de F01.
- **El hilo hace una subconsulta más por mensaje** para encontrar la generación. Es una lectura por índice sobre una página de a lo sumo cien mensajes.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **El Operator ve el texto** | Rechazada por el grupo: la configuración del agente es del admin |
| **Atribuir cuál componente pesó más** | Ningún proveedor lo informa: sería una cifra inventada |
| **Escribir `reasoning_json`** | Duplica `generation_components` en un JSON, que es exactamente lo que H-01 sacó del modelo |
| **Pedirlo por mensaje** | Deja afuera las sugerencias pendientes, que no tienen mensaje |
| **Borrar `reasoning_json`** | Una migración que no cambia nada de F05 |
