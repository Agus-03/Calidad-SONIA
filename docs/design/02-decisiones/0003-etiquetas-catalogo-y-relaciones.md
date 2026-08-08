# ADR-0003 — Etiquetas: catálogo único con relaciones a contactos y conversaciones

| Campo | Valor |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 2026-08-06 |
| **Etapa** | Diseño — Paso 3 |
| **Resuelve** | Hallazgos H-07 y H-16 de la [matriz de trazabilidad](../01-trazabilidad.md) |
| **Requisitos afectados** | F19, F23, F24, F30, F33 |

---

## 1. Contexto

Tres requisitos usan etiquetas: F19 (el admin define el catálogo), F24 (el operador etiqueta **conversaciones**) y F33 (segmentación de **contactos** por etiqueta).

El Anexo C tiene dos piezas que no se hablan entre sí:

- `labels` — el catálogo, con nombre, color y autor.
- `contacts.tags_json` — un JSON dentro de cada contacto.

Y `conversations` **no tiene ningún vínculo con `labels`**, lo que hace que **F24 no sea representable hoy**.

## 2. Decisión

- **Un único catálogo:** `labels` (se conserva del Anexo C; lo administra el rol `admin` por F19).
- **Dos tablas de unión:** `contact_labels` y `conversation_labels`.
- **Se elimina `contacts.tags_json`.**

Los nombres siguen la convención C03 para tablas de unión: las dos entidades en plural, en orden alfabético.

## 3. Justificación

- **F24 hoy no es representable.** No hay ningún vínculo entre conversación y etiqueta; no es una limitación de rendimiento sino de expresividad.
- **Integridad referencial.** El catálogo lo administra el admin (F19): puede renombrar o borrar una etiqueta. Con `tags_json`, todos los JSON quedan apuntando a un nombre que ya no existe, y nada lo impide ni lo detecta.
- **F33 pide filtrar contactos por etiqueta.** Con JSON hacen falta índices GIN y consultas que no joinean limpio; y la consulta inversa —"qué contactos tienen esta etiqueta"— es cara.
- **El filtro por etiqueta de F23** depende de que exista la relación con conversaciones.

## 4. Consecuencias

### Positivas

- F24 pasa de no representable a implementable.
- Borrar o renombrar una etiqueta del catálogo se vuelve una operación con integridad referencial garantizada por la base.
- Queda soportado el filtro "por etiqueta" de F23.
- Las dos consultas —etiquetas de un contacto, y contactos de una etiqueta— cuestan lo mismo.

### Negativas / costos asumidos

- **Dos tablas más** en el modelo.
- Leer las etiquetas de un contacto pasa de leer una columna a hacer un join. A cambio se gana la consulta inversa, que con JSON era cara.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Tabla polimórfica única** (`taggings` con `entity_type` + `entity_id`) | Menos tablas y extensible a futuras entidades etiquetables. Descartada porque pierde las claves foráneas declaradas: la integridad vuelve a depender de la aplicación, que es exactamente el problema que esta decisión resuelve. |
| **Mantener `tags_json` y agregar sólo `conversation_labels`** | Resuelve F24 pero deja intacto el problema de integridad de F33. Media solución. |
| **Statu quo** | F24 queda sin soporte. |
