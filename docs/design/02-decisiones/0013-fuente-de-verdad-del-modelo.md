# ADR-0013 — La fuente de verdad del modelo es el catálogo, no el tablero

| Campo | Valor |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 2026-08-09 |
| **Etapa** | Diseño — Paso 5 (modelo de datos lógico) |
| **Resuelve** | Contradicción entre C10 y la práctica: qué artefacto manda cuando el tablero y el catálogo difieren |
| **Enmienda** | [00-convenciones.md](../00-convenciones.md) — C10 |

---

## 1. Contexto

C10 decidió **Miro** para el DER y el diagrama de clases, con un argumento que sigue siendo válido: más de treinta entidades, y el layout automático a esa escala produce diagramas *"que se pueden generar pero no leer, sin forma práctica de corregirlos"*. El modelo terminó con 35 entidades y 69 relaciones: la previsión era correcta.

Pero C10 agregó una frase que la práctica desmintió:

> *"El tablero es la fuente de verdad del DER y del diagrama de clases."*

Cuando se escribió, era razonable: no existía ningún otro artefacto que describiera el modelo. Hoy sí existe, y es más completo que cualquier tablero:

- `DER/entidades.md` — 35 entidades con sus atributos, tipos, **una línea `Origen` por entidad** que la traza a un requisito, un hallazgo o una decisión, y las notas ⚙ que justifican cada atributo sin anclaje textual.
- `DER/relaciones.md` — cada relación con su cardinalidad, **de qué lado vive la FK y por qué**, la justificación de cada FK nullable, y las reglas que no son expresables como cardinalidad.

Un tablero no puede contener nada de eso. Y C10 ya reconocía el otro lado del problema: *"Miro no se versiona: no hay historial de cambios ni revisión por diferencias sobre el DER ni sobre el diagrama de clases, que son justamente los artefactos que más van a cambiar."*

La consecuencia práctica de dejar C10 como está: el catálogo —con toda la trazabilidad que sostiene el trabajo— pasaría a ser documentación que envejece desde el día en que se dibuje el tablero.

## 2. Decisión

**La fuente de verdad del modelo de datos es el catálogo:** `DER/entidades.md` + `DER/relaciones.md`.

**El tablero de Miro es una representación legible del catálogo**, no su origen.

El orden de propagación es: **catálogo primero, tablero después.** Un cambio que aparezca sólo en el tablero no está tomado.

Lo que **no** cambia de C10: Miro sigue siendo la herramienta para dibujar el DER y el diagrama de clases, por el argumento de layout manual, que sigue en pie. Mermaid sigue siendo la herramienta por defecto para todo lo demás.

## 3. Justificación

- **La verdad queda versionada.** Un `git diff` muestra qué cambió del modelo y cuándo. Es exactamente el costo que C10 aceptaba y que ahora no hace falta pagar.
- **La trazabilidad vive en el artefacto que manda.** Las líneas `Origen` son lo que permite responder *"¿de qué requisito sale esta entidad?"* en la defensa. Si el artefacto que manda no las contiene, la pregunta se responde desde un documento secundario — y eso invita a que se desincronicen.
- **Encaja con el Paso 14.** El plan establece que *"el diagrama de clases no se rehace: se sincroniza desde el DER"*. Derivar un diagrama desde un texto estructurado es trabajo mecánico; derivarlo desde un canvas es trabajo manual y propenso a omisiones.
- **Encaja con el Paso 15.** El checklist de revisión cruzada pide verificar que ninguna entidad se llame distinto en dos diagramas y que las cardinalidades coincidan. Eso requiere un artefacto contra el cual verificar, y sólo el catálogo puede serlo.
- **Es lo que ya venía pasando de hecho.** Las decisiones del Paso 5 —la auditoría, ADR-0012, los renombres— se tomaron y se escribieron en el catálogo. El tablero todavía no existe. Declarar fuente de verdad a un artefacto que no existe habría sido una ficción.

## 4. Consecuencias

### Positivas

- El modelo queda revisable por diferencias, con historial y con autoría.
- La trazabilidad requisito → entidad → atributo vive en el mismo lugar que el modelo.
- Elimina la ambigüedad sobre qué artefacto gana ante una discrepancia, que es la pregunta que aparece justo cuando hay poco tiempo.

### Negativas / costos asumidos

- **Exige disciplina sostenida:** tocar el catálogo primero, siempre, incluso cuando el cambio se descubre dibujando. Es más lento que corregir la caja en el tablero y seguir. Si esa disciplina se rompe, el problema no desaparece — se invierte.
- **El tablero puede quedar atrasado respecto del catálogo** sin que nada lo señale. No hay validación automática entre ambos: la sincronización es un acto manual y voluntario.
- Mientras el tablero sea el artefacto que se muestra en la defensa, hay que asegurarse de que esté exportado **después** del último cambio del catálogo. La regla de exportación fechada de C10 sigue siendo la mitigación.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Mantener C10 tal cual (el tablero manda)** | Convierte al catálogo en documentación que envejece, y con él a las 35 líneas `Origen` y a todas las justificaciones de FK nullable — que son el material que responde las preguntas de la defensa. Además deja el modelo sin historial de cambios, que es el costo que C10 ya había marcado como su punto débil. |
| **Abandonar Miro y dibujar el DER en Mermaid** | Resolvería el versionado de un plumazo, pero reabre C10 en su parte más sólida: con 35 entidades el layout automático produce un diagrama ilegible. El problema del versionado se resuelve mejor moviendo la fuente de verdad que cambiando de herramienta. |
| **Generar el tablero automáticamente desde el catálogo** | Elimina la desincronización de raíz, pero requiere construir y mantener un generador contra la API de Miro: es trabajo de infraestructura que no aporta al trabajo de tesina y que nadie planificó. |
