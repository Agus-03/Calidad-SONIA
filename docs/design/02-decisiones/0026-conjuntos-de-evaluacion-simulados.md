# ADR-0026 — Los conjuntos de evaluación de OE3 y OE4 se simulan

| Campo | Valor |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 2026-09-04 |
| **Etapa** | Diseño — planificación de la evaluación |
| **Resuelve** | De dónde salen las 200 y las 500 conversaciones |
| **Requisitos afectados** | OE3, OE4 · `conversation_annotations` · F12 |

---

## 1. Contexto

OE3 exige un conjunto de prueba de **200 conversaciones** y OE4 uno de **500 conversaciones etiquetadas**. Los dos son conjuntos **acotados por calendario**: dependen de tráfico real acumulado y de trabajo humano de anotación.

Y las fechas no cierran. OE3 vence el **28/08/2026** y OE4 el **07/09/2026**; el despliegue en producción real —OE7, Fase 4— empieza en **marzo de 2027**. Exigir conversaciones reales volvería los dos objetivos inalcanzables por construcción, no por falta de trabajo.

## 2. Decisión

**Los dos conjuntos se simulan.** Se generan conversaciones sintéticas y se anotan contra las definiciones de F12, [ADR-0023](0023-definicion-de-cambio-de-intencion.md) y [ADR-0024](0024-unidad-y-alcance-de-oe6.md). No se espera tráfico real para OE3 ni para OE4.

Los conjuntos viven versionados en `test/datasets/`, como ya preveía la estructura del código.

## 3. Justificación

- **El cronograma no permite otra cosa.** Los dos objetivos vencen medio año antes del despliegue: cualquier plan que dependa de tráfico real los incumple por fecha.
- **La anotación es trabajo humano que escala con el tamaño del conjunto.** Con 700 conversaciones entre los dos, simular permite controlar la distribución de casos en vez de esperar a que aparezcan.
- **Permite cubrir a propósito los casos raros.** Las cuatro señales de F12 no aparecen con la misma frecuencia en tráfico real: `explicit_request` y `out_of_domain` podrían no juntar muestra suficiente para medir precisión con sentido.

## 4. Consecuencias

### Positivas

- OE3 y OE4 dejan de depender del calendario de producción.
- La distribución de clases se controla, así que cada señal tiene muestra suficiente.
- El conjunto es reproducible y versionable: es exactamente lo que H-10 pedía para que la precisión ≥ 85 % no dependa de una planilla suelta.

### Negativas / costos asumidos

- **La precisión medida es sobre datos sintéticos y no se traslada automáticamente a producción.** Va declarado como **limitación explícita del trabajo**, no como nota al pie: es la primera pregunta que va a hacer un tribunal.
- **Riesgo de circularidad.** Si las conversaciones las genera el mismo modelo que después las clasifica, se mide consistencia consigo mismo y no precisión. La generación y la anotación tienen que apoyarse en criterios independientes del clasificador.
- **La distribución sintética puede no parecerse a la real.** Una precisión alta sobre casos limpios no sobrevive al ruido de WhatsApp: audios, mensajes cortados, errores de tipeo.
- **Las conversaciones simuladas viven en las mismas tablas que las reales.** `conversation_annotations.conversation_id` es FK a `conversations`, así que **todas las métricas de F34–F38 y de OE7 las van a contar** salvo que se las excluya explícitamente. Hace falta una marca —una columna, una `account` aparte o un rango de ids reservado— y **esa decisión queda abierta**. Es el pendiente más urgente que deja este ADR.

> **Mitigación recomendada:** repetir la medición sobre una muestra real durante OE7 y reportar los dos números. No reemplaza al conjunto sintético, pero convierte la limitación en un dato en vez de en una excusa.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Esperar tráfico real** | Incompatible con las fechas de OE3 (28/08/2026) y OE4 (07/09/2026), medio año anteriores al despliegue en producción. |
| **Conjunto mixto (real + sintético)** | Mejor calidad, pero depende igual del tráfico y no resuelve el problema de fecha. Se recupera como mitigación durante OE7. |
| **Reutilizar un corpus público** | No existe uno del dominio del cooperante, en español rioplatense y con estas etiquetas. |
