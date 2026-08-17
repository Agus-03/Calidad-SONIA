# ADR-0015 — Cómo se mide OE1

| Campo | Valor |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 2026-08-10 |
| **Etapa** | Diseño — Paso 6 (validación cruzada de las métricas) |
| **Resuelve** | **H-20** — `active` no se versiona · **H-21** — `generations` no guarda de qué agente es |
| **Requisitos afectados** | OE1 · F01, F03, F05 |
| **Enmienda** | [03-capa-prompts.md](../03-capa-prompts.md) — Regla 2 (política de versionado) |

---

## 1. Contexto

OE1 exige *"que el **100 % de los prompts generados incorporen todos los componentes activos** y que la previsualización del prompt ensamblado esté disponible en tiempo real durante la configuración"*. Son dos métricas de naturaleza distinta en una misma oración; este ADR trata la primera y declara qué se hace con la segunda.

La primera es una **comparación entre dos conjuntos**:

| | Qué es | Dónde vivía |
|---|---|---|
| **A** | Lo que **se debía** ensamblar: los componentes activos del agente **en ese instante** | — |
| **B** | Lo que **se ensambló** | `generation_components` |

**A no existía.** `prompt_components.active` es un booleano de estado actual sin historial, y la [Regla 2](../03-capa-prompts.md) había concluido explícitamente que no hacía falta versionarlo:

> *"`active` y `position` quedan congelados en el momento de cada generación a través de `generation_components` (que sólo enlaza componentes que participaron, con su posición real de ensamblado)."*

El razonamiento es correcto para `position` y para `content`. Para `active` **equipara haber participado con haber estado activo** — que es exactamente lo que OE1 verifica. Con esa equivalencia, el único registro de A es el registro de B, la consulta compara un conjunto contra sí mismo y **devuelve 100 % siempre, aunque el ensamblador falle**. La Regla 2 dio por cierto lo que la métrica debía comprobar.

**Y había un segundo hueco.** Aun con el historial de `active`, para construir A hace falta saber **de qué agente** es la generación, y `generations` no lo guardaba. Los dos caminos disponibles fallan: `conversations.ai_agent_id` es mutable —F27 permite cambiar o apagar el agente en un chat, y `relaciones.md` §9 lo dice textual—, y derivarlo desde `generation_components` es circular, porque deduce *qué agente* a partir de *qué se ensambló*. La entidad ya congelaba `llm_provider`, `llm_model` y `temperature`, que salen del agente, y no congelaba de quién eran.

**Nada de esto es hipotético.** Tres razones concretas:

- **La ablación está planificada.** El catálogo justifica `active` diciendo que *"habilita ablación"*, y el Paso 4 la propone como evidencia experimental barata. Apagar un componente y volver a encenderlo es la operación que vuelve ininterpretables todas las mediciones anteriores.
- **La Regla 5 se quedaba sin control.** Dice que `system` *"nunca se recorta... porque OE1 mide que el 100 % de los prompts incorpore todos los componentes activos"*: una regla justificada en una métrica incapaz de detectar si la regla se viola.
- **La población de OE1 es producción.** Dice *"el 100 % de los prompts **generados**"* — generaciones reales, no casos de prueba.

## 2. Decisión

**(a) `prompt_component_versions` incorpora `active boolean`:** el estado encendido/apagado congelado en cada versión.

**(b) Se enmienda la Regla 2:** se crea una versión nueva cuando cambia el `content` **o el `active`**. `position` sigue sin disparar versión.

**(c) `generations` incorpora `ai_agent_id`**, FK obligatoria a `ai_agents`: qué agente produjo esta generación. Es **distinta de `conversations.ai_agent_id`**, que es mutable y da el agente actual de la conversación.

**(d) La cláusula de previsualización de OE1 no se mide con SQL.** Es comportamiento de interfaz (F03) y se verifica por prueba funcional. **OE1 se verifica por dos vías de naturaleza distinta**, y así se declara en el entregable del Paso 6.

Con (a) y (c), A y B se construyen por caminos independientes y la consulta de OE1 pasa a significar algo.

## 3. Justificación

- **Dos columnas, ninguna entidad.** El modelo se mantiene en 36 entidades. Es la intervención mínima que vuelve medible el objetivo.
- **`active` versionado es literalmente lo que OE1 mide.** No es un dato agregado por las dudas: es el sustantivo de la oración del objetivo.
- **`generations.ai_agent_id` corrige una asimetría, no agrega una capacidad.** La entidad ya snapshotea la configuración del agente y no su identidad; eso era un olvido.
- **La enmienda completa la Regla 2 en lugar de contradecirla.** Su justificación era evitar *"filas idénticas"* en el histórico. Una fila donde `active` cambió no es idéntica a la anterior: versionar cuando cambia algo significativo sigue siendo el criterio, y lo que estaba mal era considerar que `active` no lo era.
- **Detecta las tres fallas que importan:** el truncamiento por presupuesto de tokens (Regla 5), la selección equivocada del conjunto de componentes, y la pérdida de interpretación del histórico por ablación.

## 4. Consecuencias

### Positivas

- OE1 se vuelve verificable **sobre datos de producción**, que es la población que declara el objetivo.
- La Regla 5 pasa a tener un control que la vigila en vez de una afirmación que la respalda.
- La ablación se convierte en un experimento repetible: se puede apagar un componente, medir, volver a encenderlo y seguir interpretando lo medido antes.
- `generations.ai_agent_id` habilita, de paso, cualquier métrica por agente sin depender del estado mutable de la conversación.

### Negativas / costos asumidos

- **`prompt_component_versions` se vuelve más ruidosa.** Prender y apagar un componente genera filas de versión aunque el texto no cambie. Es ruido legítimo, pero el histórico de un componente deja de leerse como "la lista de sus redacciones".
- **La consulta de OE1 no es trivial.** Reconstruir "la versión vigente en el instante T" para cada componente exige una subconsulta lateral por componente y por generación. Es una consulta de reporte, no de bandeja, así que el costo es aceptable — pero hay que escribirla con cuidado.
- **Necesita desempate.** Si dos cambios de un mismo componente caen en el mismo instante, "la versión vigente" es ambigua: se resuelve ordenando por `version_number` además de `created_at`, y hay que acordarse de hacerlo.
- **Se admite que un documento vigente estaba mal.** La Regla 2 se publicó con un razonamiento incompleto. Preferimos enmendarla dejando escrito el error a corregirla en silencio.
- **La cláusula de previsualización queda fuera del entregable SQL.** Es honesto, pero significa que OE1 no se responde con un solo número.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **`generations.expected_components_count`** — el ensamblador escribe cuántos componentes creía activos, y se compara con `count(generation_components)` | Es una columna sola y detecta el truncamiento, pero **los dos números salen del mismo código**: si el ensamblador consultó mal el conjunto activo, ambos coinciden y la métrica da 100 %. Detecta errores de suma, no de comprensión. Tampoco dice cuál componente faltó ni recupera el histórico, así que la ablación seguiría rompiendo la interpretación de lo medido antes. |
| **Entidad `ai_agent_versions`** — congelar la configuración completa del agente y apuntarla desde `generations` | Resuelve lo mismo que la opción elegida y además da una clave de agrupación estable y legible para OE2 —hoy resoluble con un `array_agg` sobre las versiones, feo pero funcional—. Se descarta **por ahora, no en general**: esa comodidad es una necesidad del diseño experimental, que se define en el **Paso 11**. Y postergarla no cuesta nada, porque el Paso 11 ocurre antes de la Fase 2 y no habrá ni una fila de datos que migrar. Es la situación inversa a la de [ADR-0012](0012-cuenta-y-bandejas-de-entrada.md), donde postergar `accounts` habría obligado a decidir retroactivamente sobre datos productivos reales. |
| **Declarar OE1 no medible por SQL y verificarlo sólo con pruebas unitarias** | Defendible —es una propiedad de corrección de código, y OE5 ya compromete ≥80 % de cobertura—, pero **baja el objetivo sin decirlo**: el anteproyecto dice *"el 100 % de los prompts generados"*, y la población son las generaciones reales. Además deja la Regla 5 sin control en producción, y esa falla es justamente la que un test no reproduce: aparece con una conversación larga más un RAG grande. En la defensa, *"¿cómo saben que se cumplió en producción?"* se quedaría sin respuesta. |
| **Versionar también `position`** | Ninguna métrica del anteproyecto mide el orden de ensamblado, y `generation_components.position` ya registra el orden real usado. Agregarlo sería construir capacidad sin requisito — el mismo argumento con el que [ADR-0009](0009-horario-de-atencion.md) dejó el ruteo por horario fuera de alcance. |
