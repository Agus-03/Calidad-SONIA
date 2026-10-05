# ADR-0076 — El texto de los PDF se extrae en modo *layout*

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · el modo *layout* de `pypdf`, adoptado antes de indexar documentos reales, decidido por el grupo el 2026-10-04 · las reglas 2 a 5, de construcción |
| **Fecha** | 2026-10-04 |
| **Etapa** | Construcción — Fase 12 (conocimiento) |
| **Resuelve** | La sospecha que quedó abierta en la revisión del `worker` ([ADR-0075](0075-medicion-de-oe6-y-evaluacion-de-recuperacion.md) §3): qué hace `pypdf` con los saltos de línea, y con eso dónde caen los cortes de [ADR-0065](0065-fragmentacion-de-documentos.md) |
| **Requisitos afectados** | F06 · OE6 |
| **Relacionada con** | [ADR-0064](0064-ingesta-de-documentos.md), [ADR-0065](0065-fragmentacion-de-documentos.md), [ADR-0071](0071-worker-de-ingesta.md), [ADR-0075](0075-medicion-de-oe6-y-evaluacion-de-recuperacion.md) |

---

## 1. Contexto

[ADR-0071](0071-worker-de-ingesta.md), regla 1, eligió `pypdf` para leer los PDF, y no dijo con cuál de sus dos modos de extracción:

- **El modo normal** (`plain`) devuelve el texto en el orden en que el PDF lo dibuja. Es el que usaba el `worker`.
- **El modo *layout*** ubica cada renglón en la columna en que está en la página, rellena con espacios para ubicarlo y deja una línea en blanco donde la página tiene un hueco vertical.

La fragmentación recursiva de [ADR-0065](0065-fragmentacion-de-documentos.md) corta primero por línea en blanco, después por salto de línea y después por `". "`. Así que dónde caen los cortes depende de los saltos de línea que deja la extracción.

Con el anteproyecto ([ADR-0075](0075-medicion-de-oe6-y-evaluacion-de-recuperacion.md), regla 4), el modo normal **pone un salto de línea entre cada dos palabras**:

| Extracción | Mediana del largo de un renglón | Espacios | Trozos que terminan a mitad de una oración |
|---|---|---|---|
| Normal (`chunking.yaml` v1) | 6 caracteres | 10 % | 90 % |
| *Layout*, tal como sale | 92 caracteres | 35 % | 29 % |
| *Layout* sin el relleno (regla 2) | 74 caracteres | 11 % | 40 % |

Con el modo normal, el separador `"\n"` está en todas partes. El recursivo corta en cualquier palabra, y la línea en blanco que separa dos párrafos no se distingue de la que separa dos palabras.

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **El `worker` extrae el texto en modo *layout*.** Se adopta antes de indexar documentos reales |
| 2 | **Del texto en modo *layout* se quita el relleno**: cada serie de espacios o tabuladores pasa a ser un espacio, y cada renglón pierde los márgenes. Los saltos de línea y las líneas en blanco quedan como están |
| 3 | **El modo es un parámetro de `rules/chunking.yaml`** (`pdf_text`, con los valores `plain` y `layout`), y su `version` pasa a 2. Cada trozo lo registra a través de `chunking_version` ([ADR-0065](0065-fragmentacion-de-documentos.md), regla 9) |
| 4 | **Una página que el modo *layout* no puede leer se lee en modo normal**, y queda en el log. No se pierde su texto |
| 5 | **El cargador rechaza un `pdf_text` desconocido o ausente**, y el `worker` no arranca, como con el resto de `chunking.yaml` |

## 3. Justificación

- **El modo *layout* porque deja los saltos de línea donde los tiene la página.** Con renglones enteros y líneas en blanco entre párrafos, los separadores de ADR-0065 vuelven a significar lo que dicen: sección, párrafo, renglón, oración.
- **Antes de indexar documentos reales** porque cambiarlo después obliga a reindexar todo lo que ya se cargó, y hoy la base está vacía.
- **Quitar el relleno porque no es texto.** Los espacios que ubican un renglón en su columna inflarían el conteo de tokens, que se estima por caracteres ([ADR-0071](0071-worker-de-ingesta.md), regla 2): con el relleno, un trozo de "500 tokens" tendría un tercio de espacios. También llegarían a Voyage y al prompt.
- **En `chunking.yaml` y no en el código** porque cambia lo que se corta tanto como un separador. Versionado ahí, una medición de OE6 sabe con qué extracción se hicieron los trozos que recuperó. Y la evaluación diferida de ADR-0075 puede comparar los dos modos cambiando un dato.
- **Leer en modo normal la página que falla** porque el modo *layout* calcula la posición de cada carácter en la página, y una fuente mal declarada puede hacerlo fallar donde el modo normal sólo lee el texto. Una página sin texto es peor que una página con los saltos de línea mal puestos.

## 4. Consecuencias

### Positivas

- En el anteproyecto, los trozos que terminan a mitad de una oración bajan del 90 % al 40 %, y el trozo más chico pasa de 45 a 108 tokens estimados.
- El texto que ven Voyage y el modelo es el de la página, en renglones, y no una palabra por línea.
- La evaluación diferida de ADR-0075 puede comparar los dos modos sin tocar código.

### Negativas / costos asumidos

- **Está medido sobre un solo documento**, el anteproyecto. Que el modo normal separe cada palabra puede ser propio de cómo se generó ese PDF. En otros documentos, la diferencia puede ser menor, o puede ser en contra.
- **El 40 % sigue siendo alto.** El renglón de un PDF termina donde termina el ancho de la página, no donde termina la oración. Un párrafo más largo que un trozo se corta en un renglón cualquiera.
- **Las tablas y las columnas pierden su forma.** Al quitar el relleno, las celdas de un renglón quedan separadas por un solo espacio, igual que las palabras. Con el modo normal tampoco la tenían.
- **Dos columnas de texto pueden quedar mezcladas por renglón.** El modo *layout* lee la página de izquierda a derecha, así que un renglón junta el de la columna izquierda con el de la derecha.
- **Un documento puede tener páginas leídas con cada modo**, por la regla 4, y `chunking_version` dice 2 en todos sus trozos. Sólo el log sabe qué páginas se leyeron en modo normal.
- **El modo *layout* es algo más lento** que el normal: en el anteproyecto, 3,8 s contra 3,6 s, con las imágenes incluidas. En el `worker` no hay un usuario esperando, pero una base grande tarda más en indexarse.
- **Las líneas en blanco que separan párrafos salen del espacio vertical de la página.** Un título con poco aire antes no se separa del párrafo anterior, y un interlineado amplio puede partir un párrafo en dos.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Seguir con el modo normal** | En el anteproyecto, nueve de cada diez trozos se cortan a mitad de una oración |
| **El modo normal, juntando los renglones cortos** | Es una heurística propia para adivinar qué salto de línea es real, cuando el modo *layout* ya lo sabe por la posición en la página |
| **El modo *layout* sin las líneas en blanco del espacio vertical** | Probado sobre el anteproyecto: los trozos a mitad de oración vuelven al 84 %, porque sin líneas en blanco no hay párrafos que respetar |
| **El modo *layout* con el relleno** | Un tercio de los caracteres son espacios, que inflan la estimación de tokens y llegan a Voyage y al prompt |
| **Juntar las líneas en blanco seguidas en una sola** | Probado sobre el anteproyecto: no mejora los cortes (41 % contra 40 %), y borra la diferencia entre un párrafo y una sección que usa el separador `"\n\n\n"` |
| **Cambiar de biblioteca, a PyMuPDF** | Extrae mejor, pero es AGPL ([ADR-0071](0071-worker-de-ingesta.md)) |
