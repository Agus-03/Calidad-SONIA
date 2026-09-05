# ADR-0023 — Qué es un "cambio de intención" (OE4)

| Campo | Valor |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 2026-09-04 |
| **Etapa** | Diseño — Paso 11 anticipado (definición de métrica) |
| **Resuelve** | La mitad sin definir de OE4 · el `gold_value` pendiente de `AnnotationType.intent` |
| **Requisitos afectados** | OE4 · F04, F11 · OE2 |

---

## 1. Contexto

OE4 promete detectar *"señales de frustración **y cambios de intención**"* con precisión ≥ 85 % sobre 500 conversaciones etiquetadas. La mitad de frustración tiene señal declarada en F12; **la otra mitad no está definida en ninguna parte**, y el catálogo dejó a propósito el `gold_value` de `annotation_type = 'intent'` en *pendiente* esperando esta decisión.

Dos lecturas aparecieron primero, y las dos se descartan por motivos distintos:

**Cualquier cambio de `intent_label` entre bloques consecutivos.** Bajo esa lectura casi toda conversación tiene cambios de intención —se pregunta el precio, después el envío—, la métrica se degrada a la tasa base y detectarla al 85 % es trivial.

**"El pedido pasó a necesitar un humano".** Es la lectura intuitiva, y es **exactamente el clasificador de transferencia de F12** (`out_of_domain`, `no_resolution`) que **OE3 ya mide**. Adoptarla dejaría dos objetivos midiendo el mismo clasificador sobre dos conjuntos distintos, y `AnnotationType` con `transfer` e `intent` anotando el mismo fenómeno.

## 2. Decisión

Un **cambio de intención** es un cambio en lo que el cliente quiere que **invalida el contexto acumulado**: obliga al reescritor de F04 a **descartar** el resumen vigente en lugar de **extenderlo**.

> *Ejemplo:* venía negociando el precio de un producto y pasa a reclamar por una entrega demorada. No hace falta un humano — hace falta que el bot deje de arrastrar el contexto de la negociación.

`conversation_annotations.gold_value` para `annotation_type = 'intent'` toma dos valores: **`intent_change` · `none`**.

## 3. Justificación

- **Le da a OE4 un territorio que OE3 no ocupa.** Sin esto, dos de los siete objetivos específicos miden el mismo clasificador, y es la primera pregunta que haría un tribunal.
- **Lo engancha con el aporte original de la tesina.** Bajo la otra lectura, el mecanismo de reescritura adaptativa (F04) se queda **sin ningún objetivo que verifique que reescribe *bien***: OE2 mide consistencia de tono y nada más. Acá el cambio de intención es justamente el caso donde reescribir mal se nota.
- **Es observable con lo que ya se persiste:** `detected_intents` por bloque y `conversation_context_versions` turno a turno.
- **No cambia el modelo.** Sólo llena el dominio de un `gold_value` que el catálogo dejó en blanco esperando esta decisión.

## 4. Consecuencias

### Positivas

- OE3 y OE4 miden cosas distintas, con ground truth distinto.
- El dominio de `gold_value` para `intent` queda cerrado y el catálogo deja de tener una celda pendiente.
- La anotación es decidible leyendo la conversación, sin necesidad de ejecutar el sistema.

### Negativas / costos asumidos

- **"Invalida el contexto acumulado" tiene borde difuso.** Entre extender y descartar hay casos intermedios, y dos anotadores pueden discrepar. Conviene medir el acuerdo entre etiquetadores — `conversation_annotations.annotated_by_operator_id` ya lo permite, y es la razón por la que ese atributo existe.
- **Se aparta de la lectura intuitiva del equipo**, así que hay que explicarlo por escrito en el informe: OE4 no mide "cuándo hace falta un humano", eso es OE3.
- **Hace falta una guía de anotación con ejemplos antes de etiquetar**, o la definición no se aplica de forma consistente entre las 500 conversaciones.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Cualquier cambio de `intent_label` entre bloques** | Casi toda conversación tiene uno: la métrica se degrada a la tasa base y el 85 % se alcanza sin detectar nada. |
| **"Lo que pide ya no lo puede cumplir el LLM"** | Es el clasificador de transferencia de F12 que OE3 ya mide. Deja dos objetivos midiendo lo mismo y `AnnotationType` con dos valores redundantes. |
| **Cambio entre categorías de una taxonomía declarada** | Razonable y medible, pero exige fijar y mantener una taxonomía de intenciones que el anteproyecto no tiene. La definición elegida no la necesita. |
