# ADR-0021 — Atribución entre señales de transferencia simultáneas

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · **§1 y §2 enmendadas por [ADR-0044](0044-senal-de-frustracion-diferida-a-la-fase-16.md)**: hasta la Fase 16 se evalúan tres señales, no cuatro, porque `frustration` queda salteada. El orden no cambia, pero ADR-0044 acota `frustration` a la frustración con el agente, y eso debilita el argumento de §3 que la separa de la *"capacidad del sistema"* |
| **Fecha** | 2026-09-04 |
| **Etapa** | Diseño — Paso 6 (pendiente abierto por ADR-0014) |
| **Resuelve** | La regla de atribución que F12 no declara |
| **Requisitos afectados** | F12, F36 · OE3 |

---

## 1. Contexto

El clasificador **evalúa las cuatro señales de F12 en cada bloque procesado** y no corta al primer disparo — es la regla que H-18 impuso para que OE4 siga siendo medible.

Consecuencia directa: dos o más señales pueden superar su umbral en el mismo turno. Pero **la transferencia es una sola** y `transfer_events.signal_evaluation_id` apunta a **una única fila**. F12 no dice cuál gana.

No es un detalle de implementación: **F36 pide la *"tasa de transferencia por tipo de señal"***. Si la atribución es arbitraria, esa métrica es ruido con forma de número.

## 2. Decisión

**Orden de prioridad fijo.** Gana la primera que haya disparado, en este orden:

```
explicit_request  →  frustration  →  out_of_domain  →  no_resolution
```

`signal_evaluations` conserva las cuatro filas con su `score`, su `threshold_used` y su `fired`, así que **la atribución no destruye evidencia**: el conteo sin atribuir sigue disponible.

## 3. Justificación

- **`explicit_request` es la única de las cuatro que no es una inferencia nuestra.** El cliente pidió un humano con todas las letras; ninguna señal inferida debería taparlo, ni al transferir ni al reportar por qué se transfirió.
- **El resto va del estado del cliente a la capacidad del sistema.** Si el cliente está frustrado, ése es el motivo que hay que reportar aunque además la consulta cayera fuera del dominio: lo primero se corrige con la conversación, lo segundo con la base de conocimiento, y confundirlos manda al equipo a arreglar lo que no falla.
- **Es explicable en una oración.** El margen normalizado (`score / threshold_used`) se mueve cada vez que se ajusta un umbral: la atribución histórica cambiaría de sentido sin que el clasificador haya cambiado, que es exactamente el defecto que `threshold_used` existe para evitar.

## 4. Consecuencias

### Positivas

- F36 tiene una atribución determinista y reproducible entre corridas.
- La evidencia completa sobrevive en `signal_evaluations`: la atribución es una lectura, no una pérdida.
- Es una constante ordenada en el código, no configuración: no hay que congelarla por fila.

### Negativas / costos asumidos

- **La tasa por señal queda sesgada hacia el principio del orden.** `no_resolution` va a aparecer subrepresentada cada vez que coincida con otra señal. Hay que decirlo al leer F36, y ofrecer al lado el conteo sin atribuir, que sale de `signal_evaluations`.
- **El orden es un juicio del equipo y no está en el anteproyecto.** Queda declarado acá, igual que `accounts` y `pipeline_steps`.
- Si alguna vez se lo hiciera configurable por agente, **habría que congelar el orden usado junto a la evaluación** — mismo argumento que `threshold_used`. Hoy no se hace configurable, precisamente para no pagar eso.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Mayor margen normalizado (`score / threshold_used`)** | La atribución cambia cada vez que se ajusta un umbral, así que las mediciones históricas dejan de ser comparables sin que el clasificador haya cambiado. |
| **Registrar todas las señales que dispararon** | Obligaría a una relación N:N desde `transfer_events` y contradice que la transferencia es **una** acción. La evidencia múltiple ya vive en `signal_evaluations`. |
| **La primera en evaluarse** | Hace que el orden de ejecución —un detalle de implementación— decida una métrica del anteproyecto. |
