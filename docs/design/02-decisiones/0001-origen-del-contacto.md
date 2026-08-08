# ADR-0001 — Origen del contacto en `contacts`, no en `sales`

| Campo | Valor |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 2026-08-06 |
| **Etapa** | Diseño — Paso 3 |
| **Resuelve** | Hallazgo H-13 de la [matriz de trazabilidad](../01-trazabilidad.md) |
| **Requisitos afectados** | F14, F15, F34 |

---

## 1. Contexto

F15 dice textualmente: *"Registra el canal de ingreso de cada **contacto** (Meta Ads, QR, referido, directo)"*.

El Anexo C no pone ese dato en `contacts`: lo pone en `sales.source_channel`. Es un desalineamiento directo entre el requisito y el modelo — el requisito habla del contacto, el modelo lo guarda en la venta.

## 2. Decisión

**`contacts.source_channel` es la fuente de verdad del canal de ingreso.** Obligatorio, fijado en el alta del contacto, inmutable.

**Se elimina `sales.source_channel`.** El requisito de F14 —*"la vincula al canal de origen"*— se satisface con un join a través del contacto.

Los valores son los cuatro que enumera F15, declarados como enumeración según la convención C07:

```
SourceChannel = meta_ads | qr | referral | direct
```

## 3. Justificación

- **F15 dice "contacto", textual.** El modelo debe reflejar el requisito, no al revés.
- **Un contacto que nunca compra igual tiene un origen.** Es justamente el dato que necesita F34 para su *"distribución por canal de origen"*. Con el campo sólo en `sales`, los contactos que no convierten desaparecen de esa métrica: se estaría midiendo la efectividad de un canal contando únicamente a los que ya compraron. Es sesgo de supervivencia, y hace la métrica inútil para decidir dónde invertir.
- **Una sola fuente de verdad**, según la convención C05: un dato se modela en un solo lugar.

## 4. Consecuencias

### Positivas

- F34 se puede calcular sobre el universo completo de contactos, no sólo sobre los que compraron.
- El origen queda disponible desde el alta, sin esperar a que haya una venta.
- Desaparece la posibilidad de que dos columnas con el mismo nombre digan cosas distintas.

### Negativas / costos asumidos

- **Si el mismo contacto vuelve por otro canal, no queda registrado.** El origen es el de adquisición, no el del último contacto. Se asume: la atribución por *touchpoint* pertenece a la gestión de campañas, explícitamente fuera del alcance (7.3).
- F14 requiere un join en lugar de leer una columna. El costo es despreciable.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Mantener ambos** — origen en `contacts` y *snapshot* en `sales` | Defendible si se quisiera atribución por momento de venta. Descartada porque duplica el dato sin un caso de uso dentro del alcance, y dos columnas con el mismo nombre y semántica distinta es una fuente de error garantizada. |
| **Sólo en `sales`** (statu quo del Anexo C) | El sesgo de supervivencia descrito en §3, y el desalineamiento con el texto de F15. |

> Si más adelante se necesita atribución por touchpoint, debe ser un campo **con otro nombre** (`attribution_channel`), para que nadie confunda "por dónde llegó este cliente" con "a qué se atribuye esta venta".
