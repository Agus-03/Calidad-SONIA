# ADR-0008 — Ventana de 24 horas de WhatsApp: incorporación al anteproyecto

| Campo | Valor |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 2026-08-06 |
| **Etapa** | Diseño — Paso 3 |
| **Requisitos afectados** | F08, F28, F37, F39 · secciones 7.3 y 8.5 |

---

## 1. Contexto

La API de WhatsApp Business sólo permite enviar mensajes de formato libre **dentro de las 24 horas posteriores al último mensaje del cliente**. Fuera de esa ventana únicamente se pueden enviar plantillas previamente aprobadas por Meta.

Es una regla dura de la plataforma —no una decisión de diseño— y **no aparece en ninguna parte del anteproyecto**: ni en el alcance, ni en los flujos, ni en el modelo de datos.

## 2. Decisión

Se **incorpora al anteproyecto**, junto con su consecuencia operativa.

- Se registran en `conversations` el instante del último mensaje entrante y el vencimiento de la ventana.
- Se documenta explícitamente que **fuera de la ventana SONIA no puede iniciar contacto**, porque el alcance (7.3) excluye la creación de plantillas y F08/F37 establecen *"no crea ni envía plantillas"*.
- Se documenta el impacto sobre **F28**: un recordatorio que vence más de 24 horas después del último mensaje del cliente **no se puede accionar desde el sistema**; el operador tiene que salir por otro canal.

## 3. Justificación

- **Va a aparecer en producción sí o sí.** La Fase 4 despliega en el entorno real del cooperante con la API oficial de Meta: la ventana deja de ser teórica el primer día.
- **Cambia los flujos conversacionales.** Existe un estado del sistema en el que se puede recibir pero no iniciar, y ni los flujos conversacionales ni la máquina de estados lo contemplan hoy.
- **Es preferible declararla que dejar que la pregunte el tribunal.** Una limitación documentada es una decisión de alcance; la misma limitación descubierta en la defensa es un descuido.
- La combinación con las exclusiones de alcance produce una consecuencia que **no es obvia leyendo cada parte por separado**, y por eso conviene escribirla junta.

## 4. Consecuencias

### Positivas

- El alcance queda descrito con precisión, incluyendo lo que el sistema deliberadamente no puede hacer.
- Los flujos conversacionales y la máquina de estados pueden representar el estado "fuera de ventana".
- El equipo llega a la Fase 4 sin sorpresas sobre por qué un mensaje saliente falla.

### Negativas / costos asumidos

- **Es una limitación real de producto** que el informe tiene que declarar, y que reduce el atractivo funcional de lo entregado.
- **F28 pierde utilidad** en el caso de recordatorios a más de 24 horas, que es probablemente el caso más frecuente para un negocio de alquileres. Se asume y se documenta.
- Hay que mantener actualizado el vencimiento de la ventana en cada mensaje entrante.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Incorporar el envío de plantillas al alcance** para poder reabrir la ventana | Resolvería la limitación de raíz, pero contradice exclusiones explícitas del alcance (7.3) y de F08, F37 y F39. Ampliar el alcance a esta altura del cronograma tiene más costo que la limitación que evita. |
| **No documentarla** y tratarla como un detalle de implementación | Aparece igual en producción, pero sin haberla previsto en los flujos ni en el modelo — y sin poder explicarla como decisión. |
