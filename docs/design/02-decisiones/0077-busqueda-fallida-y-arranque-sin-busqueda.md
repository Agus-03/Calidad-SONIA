# ADR-0077 — La búsqueda que falla cuenta como no respondida, y el cerebro no arranca sin búsqueda fuera de desarrollo

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · el bloque cuya búsqueda falló como no respondido en OE6, el cerebro que no arranca sin búsqueda fuera de `development` y `test`, y la forma de contar la escalada de OE6 diferida a la etapa de medición, decididos por el grupo el 2026-10-05 · la regla 2, de construcción |
| **Fecha** | 2026-10-05 |
| **Etapa** | Construcción — Fase 12 (conocimiento) |
| **Resuelve** | Tres puntos que dejaron abiertos las revisiones del cierre de la Fase 12: qué hace OE6 con un bloque cuya búsqueda falló, qué hace el cerebro si arranca sin poder buscar, y cómo cuenta OE6 una escalada |
| **Requisitos afectados** | OE6 · F06 |
| **Enmienda** | [ADR-0074](0074-pasos-de-recuperacion-y-grounding.md), **regla 2** — *"conserva los pasos que sí decidieron"* deja de ser todo lo que queda: también se escribe la fila de `retrieval`, con `outcome = 'failed'` · [ADR-0074](0074-pasos-de-recuperacion-y-grounding.md), **regla 5** — *"un cerebro sin búsqueda configurada"* sólo existe en `development` y `test`: en cualquier otro `ENVIRONMENT` no arranca, y con eso la consecuencia de ADR-0074 que dice que contesta sin documentos · [DER/entidades.md](../DER/entidades.md), dominio de `outcome` para `retrieval` — se suma `failed` |
| **Relacionada con** | [ADR-0024](0024-unidad-y-alcance-de-oe6.md), [ADR-0068](0068-recuperacion-y-grounding.md), [ADR-0073](0073-busqueda-semantica.md), [ADR-0074](0074-pasos-de-recuperacion-y-grounding.md), [ADR-0075](0075-medicion-de-oe6-y-evaluacion-de-recuperacion.md) |

---

## 1. Contexto

OE6 pide *"que el 90% de las consultas dentro del dominio del negocio sean respondidas sin escalada al operador humano"*. [ADR-0024](0024-unidad-y-alcance-de-oe6.md) y [ADR-0075](0075-medicion-de-oe6-y-evaluacion-de-recuperacion.md) definieron la consulta, la escalada y el alcance. Las revisiones del cierre de la Fase 12 encontraron tres huecos:

- **La búsqueda que falla.** Si Voyage o Qdrant no responden, el turno falla y el cerebro conserva sólo los pasos que decidieron ([ADR-0074](0074-pasos-de-recuperacion-y-grounding.md), regla 2). El `triage` ya dejó su `in_domain`, así que el bloque entra en el denominador. No hay `grounding` ni transferencia, así que la consulta de OE6 lo contaba como **respondido**, aunque el agente no contestó nada.
- **El cerebro sin búsqueda.** Sin `QDRANT_URL` o sin `VOYAGE_API_KEY`, el cerebro arranca, avisa en el log y contesta a todos los agentes sin documentos (ADR-0074, regla 5). Un agente con base contesta sin respaldo, y su OE6 sale perfecto porque ningún bloque puede salir `not_grounded`.
- **Cómo se cuenta la escalada.** Una transferencia marca como escaladas todas las consultas de su conversación, también las que el agente respondió antes. Un `not_grounded` sin transferencia marca sólo su bloque. Por eso apagar la política `out_of_domain` mejora OE6. El pendiente ya estaba en el [README](README.md), desde el Paso 11.

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **Un bloque cuya búsqueda falló es una consulta no respondida** para OE6. Queda en el denominador y no en las respondidas. No es una escalada, porque nadie transfirió |
| 2 | **La búsqueda que falla escribe su fila de `retrieval` con `outcome = 'failed'`**, junto con los pasos que decidieron, y el turno sigue fallando como dice ADR-0074, regla 2. Su `detail_json` lleva `top_k`, el umbral, el largo del texto buscado, si se recortó y el nombre del error, pero nunca su mensaje, que puede traer lo que respondió Qdrant. Si un reintento del núcleo después responde, la fila se borra con el rastro viejo (ADR-0074, regla 7) |
| 3 | **Fuera de `development` y `test`, el cerebro no arranca sin `QDRANT_URL` y `VOYAGE_API_KEY`**, en ninguno de los dos modos. Es la misma lista de entornos permitidos que la de los dobles, así que un `ENVIRONMENT` mal escrito también queda obligado. En `development` y `test` sigue arrancando sin buscar, con el aviso |
| 4 | **Si la escalada se cuenta por bloque o por conversación se decide en la etapa de medición.** OE6 no se mide en esta etapa, y la consulta queda como está |

## 3. Justificación

- **No respondido porque el agente no contestó**, que es lo que OE6 mide. Excluir esos bloques haría que OE6 mejore justo cuando falla Voyage. Contarlos como escalada diría que fueron a una persona, y no fueron.
- **Una fila y no una deducción** porque la consulta de OE6 no tiene otra forma de distinguir un bloque cuya búsqueda falló de uno que nunca buscó, como el de un saludo. `generation` ya tiene `failed` en su dominio, y es el mismo precedente.
- **El nombre del error y no su mensaje** por lo mismo que la regla 8 de ADR-0074 no guarda el texto buscado: la fila es evidencia permanente, y el mensaje de Qdrant puede traer cualquier cosa. El mensaje va al log.
- **No arrancar** porque un cerebro sin búsqueda en producción contesta sin documentos a agentes que los tienen, y nadie se entera salvo quien lea el log de arranque. Sigue la regla 10 de [08-structure-rules](../08-structure-rules.md), que valida la configuración al arrancar en lugar de dejar que falle el primer turno.
- **Diferir la forma de contar la escalada** porque OE6 no se mide en esta etapa: se mide en la Fase 4 del anteproyecto (Validación y Redacción, desde el 1 de marzo de 2027), y esa decisión se toma con la consulta a la vista.

## 4. Consecuencias

### Positivas

- OE6 deja de contar como respondido un bloque que nadie contestó.
- El rastro dice por qué falló un turno que recuperaba, y la fila `failed` es distinguible de un bloque que no buscó.
- En producción, un cerebro que arranca siempre busca.

### Negativas / costos asumidos

- **Una caída de Voyage o de Qdrant baja OE6.** Es una falla de infraestructura y no de la base de conocimiento, y la métrica no las separa. El diagnóstico de OE6 puede separarlas filtrando por `outcome = 'failed'`.
- **Un bloque que falló en todos sus intentos queda con una fila `retrieval` sin `grounding`.** Quien lea el rastro tiene que saber que un `failed` cierra el turno.
- **Una fila más por turno fallido** en `pipeline_steps`.
- **El cerebro de producción depende de Voyage y de Qdrant para arrancar**, aunque ningún agente tenga documentos todavía. Se puede desplegar sin documentos, pero no sin las dos variables.
- **La validación sólo mira que las variables estén**, no que Voyage acepte la clave ni que Qdrant responda. Una clave inválida arranca y falla en el primer turno que busca.
- **Un `ENVIRONMENT` mal escrito en un entorno de pruebas** ahora pide las dos variables para arrancar, igual que ya pasaba con los dobles.
- **La asimetría de la escalada sigue en la consulta** hasta la etapa de medición: mientras tanto, apagar la política `out_of_domain` mejora OE6.

### Lo que queda abierto

| Qué | Estado |
|---|---|
| **Si la escalada se cuenta por bloque o por conversación** | En la etapa de medición, con el pendiente del Paso 11 del [README](README.md) |
| **Si un turno cuya generación falló cuenta como no respondido** | No se decidió: la regla 1 habla de la búsqueda. La consulta de OE6 todavía no mira `generation` |

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Excluir de OE6 los bloques cuya búsqueda falló** | OE6 mejoraría cuando la infraestructura falla |
| **Contarlos como escalada** | Diría que el bloque fue a una persona, y nadie transfirió |
| **Reportarlos aparte, sin tocar OE6** | Quedaría un número que nadie suma, y OE6 los seguiría contando como respondidos |
| **Deducir la falla de que hay `triage` y no hay `grounding`** | No la distingue de un saludo ni de un agente sin base, que tampoco tienen `grounding` |
| **Guardar el mensaje del error en la fila** | Puede traer lo que respondió Qdrant, y la fila es evidencia permanente |
| **Arrancar igual en producción y avisar** | Es lo que había: un agente con base contesta sin respaldo y nadie se entera |
| **No arrancar sin búsqueda en ningún entorno** | Obliga a levantar Qdrant y a tener una forma de vectorizar para cualquier prueba del cerebro, aunque no toque F06 |
