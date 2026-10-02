# ADR-0063 — Paquetes por razón de cambio: notes/, reasoning/ y realtime/ salen de inbox/

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · extraer los tres paquetes y enmendar las reglas 1 y 4 de `08-structure-rules` (opción A) lo eligió el grupo el 2026-10-02 |
| **Fecha** | 2026-10-02 |
| **Etapa** | Construcción — Fase 11 (resto del CRUD operativo) |
| **Resuelve** | Qué hacer con un paquete que acumula funcionalidades que cambian por razones distintas, y qué reglas de estructura lo permiten |
| **Requisitos afectados** | F05 · F21 · F26 |
| **Enmienda** | [08-structure-rules.md](../08-structure-rules.md), **reglas 1 y 4 y el árbol** — un paquete puede no tener tablas propias, y uno hermano se extrae cuando cambia por otra razón y la dependencia queda en un solo sentido · [ADR-0017](0017-decomposicion-en-despliegues-y-lenguajes.md), **§6, filas F05, F21 y F26** — F05 vive en `reasoning/`, el fan-out de F21 en `realtime/` y las notas de F26 en `notes/` · [ADR-0049](0049-visualizacion-de-los-componentes-de-cada-respuesta.md), **§2** — F05 deja de vivir en `inbox/` |
| **Relacionada con** | [ADR-0035](0035-reglas-de-la-bandeja-manual.md), [ADR-0051](0051-bandeja-en-tiempo-real.md), [ADR-0056](0056-transferencia-entre-operadores.md), [ADR-0058](0058-recordatorios.md) |

---

## 1. Contexto

Al cerrar la Fase 11, `inbox/` tenía 24 archivos de código y 19 de test, contra 5 a 9 de código en cada uno de los demás paquetes. Había ido absorbiendo todo lo que toca una conversación: la bandeja, el reparto, el traspaso, las etiquetas, las notas internas de F26, el razonamiento de F05 y el hub de F21. Las tres últimas cambian por razones que no son las de la bandeja: las notas tienen su propia tabla y su propio autor, F05 lee la evidencia del pipeline, y el hub también lo usa `reminders/` ([ADR-0058](0058-recordatorios.md)).

Dos reglas de [`08-structure-rules.md`](../08-structure-rules.md) impedían separarlas:

- **Regla 1:** *"Un paquete sin tablas propias es una bolsa y no se crea"*. Entró en `CLAUDE.md` el 2026-08-24 con la estructura base, y pasó a `08-structure-rules` el 2026-09-11. Ningún ADR la decidió, y el código ya la contradecía: `reporting/` y `brainclient/` no tienen tablas.
- **Regla 4:** un archivo que crece se parte *"en más archivos del mismo paquete, nunca en un paquete hermano: en Go eso invita al ciclo de imports y parte el contexto en dos"*. Entró en `CLAUDE.md` el 2026-09-03, tampoco por un ADR.

El criterio clásico de cohesión de paquetes es el *Common Closure Principle* de Robert Martin: lo que cambia junto va junto, y lo que cambia por razones distintas va separado. Su contrapeso es el *Acyclic Dependencies Principle*: entre paquetes no puede haber ciclos. La regla 4 protegía lo segundo prohibiendo por completo lo primero.

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **Salen de `inbox/` tres paquetes hermanos**: `notes/` (las notas internas de F26, dueño de `internal_notes`), `reasoning/` (F05, sólo lectura) y `realtime/` (el hub en memoria y el socket de F21) |
| 2 | **La dependencia va en un solo sentido**: los tres importan `inbox/`, y `inbox/` no importa a ninguno. `inbox/` sigue publicando por su interfaz `Events`, que `realtime/` implementa sin que `inbox/` lo conozca |
| 3 | **Lo que comparten se exporta desde su dueño y no se copia.** `inbox/` exporta tres cosas: `Service.Reachable`, el alcance del hilo ([ADR-0035](0035-reglas-de-la-bandeja-manual.md), regla 2), que usan `notes/` y `reasoning/`; `CanSee`, el alcance del listado, que usa `realtime/`; y `EventRow`, la fila de la bandeja que viaja en cada evento, para que un evento y una página no difieran en su forma |
| 4 | **Las rutas, los cuerpos y los códigos de error no cambian.** Cada paquete traduce los suyos con las mismas palabras que respondía `inbox/` |
| 5 | **La regla 1 se enmienda**: un paquete es un contexto con una sola razón para cambiar. Si escribe tablas, es su único dueño y nadie más las escribe. **Puede no tener tablas**, cuando lee las de otros (como `reporting/` y `reasoning/`) o es la infraestructura de un contexto (como `brainclient/` y `realtime/`) |
| 6 | **La regla 4 se enmienda**: un archivo que crece se sigue partiendo en archivos del mismo paquete. **Un paquete se extrae a uno hermano** sólo si se cumplen las tres condiciones: cambia por una razón distinta de la del paquete que lo contenía, la dependencia queda en un solo sentido, sin ciclos, y lo compartido se exporta desde el dueño en vez de copiarse |
| 7 | **`internal_notes` cambia de dueño**: de `inbox/` a `notes/` |

## 3. Justificación

- **Separar por razón de cambio (reglas 1, 5 y 6)** porque la cantidad de archivos no mide la cohesión. Un cambio en las notas no tendría por qué abrir el paquete de la bandeja, y el hub no tendría por qué vivir donde vive el envío.
- **Exportar y no copiar (regla 3)** porque el alcance del hilo es una regla de negocio de [ADR-0035](0035-reglas-de-la-bandeja-manual.md). Copiada en tres paquetes, terminaría divergiendo.
- **Dependencia en un solo sentido (regla 2)** porque es lo que la regla 4 quería proteger, el ciclo de imports. Se protege sin prohibir la extracción.
- **Mantener rutas y códigos (regla 4)** porque el frontend no tiene por qué enterarse de cómo se organiza el núcleo por dentro.

## 4. Consecuencias

### Positivas

- `inbox/` baja a 16 archivos de código, todos sobre la bandeja: el listado, el hilo, el envío, las acciones, el reparto, el traspaso y las etiquetas.
- Cada paquete nuevo sigue el patrón de cinco archivos (`model`, `repository`, `service`, `controller`, `routes`), o lo que le toca si no tiene tablas.
- Las reglas 1 y 4 quedan alineadas con el código que ya existía (`reporting/`, `brainclient/`) y con un criterio que se puede citar.

### Negativas / costos asumidos

- **`inbox/` tiene una API pública de la que dependen tres paquetes.** Cambiar `Reachable`, `CanSee` o `EventRow` obliga a revisarlos.
- **El evento del hub depende de la forma de la fila de la bandeja** (`EventRow`). Es lo que hace que coincidan, pero ata `realtime/` al DTO de `inbox/`.
- **Los tests de `notes/` y `reasoning/` imitan la regla del alcance en un doble.** Si la regla cambia en `inbox/`, los dobles pueden quedar desactualizados sin que ningún test falle. Lo compensa el test directo de `Reachable` que se sumó en `inbox/`.
- **Las reglas 1 y 4 dejan de ser mecánicas**: decidir si algo cambia por otra razón es un juicio, y dos personas pueden verlo distinto.
- **El historial de git se corta**: los archivos movidos y reescritos pierden su `blame` de un archivo a otro, salvo lo que git detecte como renombre.
- **Los arneses de test quedaron repetidos** en `notes/` y `reasoning/` (`mount`, `errorCode`, la semilla de la integración): los archivos `_test.go` no se pueden importar entre paquetes.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Dejar `inbox/` como estaba** (opción C) | Siete razones de cambio en un solo paquete |
| **Extraer sólo `notes/`** (opción B) | Cumplía la regla 1 tal como estaba escrita, pero dejaba en `inbox/` dos contextos que cambian por otras razones |
| **Subcarpetas dentro de `inbox/`** | En Go cada carpeta es un paquete, así que equivale a extraer, con una ruta de import más larga y la ilusión de que siguen siendo uno |
| **Copiar la regla del alcance en cada paquete** | Tres copias de una regla de negocio terminan divergiendo |
| **Carpetas por capa** (`controllers/`, `services/`) | Es el árbol por capas que la regla 2 evita, con razón |
