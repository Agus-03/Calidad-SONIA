# ADR-0076 — El texto de los PDF se extrae en modo *layout*

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · el modo *layout* de `pypdf`, adoptado antes de indexar documentos reales, decidido por el grupo el 2026-10-04 · quitar el relleno aunque corte algo peor, leer cada página en los dos modos y quedarse con el normal cuando el *layout* pierde texto, dejar las tablas como quedan y mantener el solapamiento en 0 aunque su premisa no se cumpla, decididos por el grupo el 2026-10-05 · las reglas 3, 5 y 6, de construcción |
| **Fecha** | 2026-10-04 |
| **Etapa** | Construcción — Fase 12 (conocimiento) |
| **Resuelve** | La sospecha que quedó abierta en la revisión del `worker` ([ADR-0075](0075-medicion-de-oe6-y-evaluacion-de-recuperacion.md) §3): qué hace `pypdf` con los saltos de línea, y con eso dónde caen los cortes de [ADR-0065](0065-fragmentacion-de-documentos.md) |
| **Requisitos afectados** | F06 · OE6 |
| **Enmienda** | [ADR-0065](0065-fragmentacion-de-documentos.md), **§3** — el argumento *"Con cortes entre párrafos, el solapamiento aporta poco […] la mayoría de los trozos terminan donde termina una idea"* deja de sostenerse: con el modo *layout*, el 40 % de los trozos del anteproyecto termina a mitad de una oración. La regla 1, solapamiento en 0, sigue en pie por [ADR-0075](0075-medicion-de-oe6-y-evaluacion-de-recuperacion.md), regla 5, y no por ese argumento |
| **Relacionada con** | [ADR-0064](0064-ingesta-de-documentos.md), [ADR-0065](0065-fragmentacion-de-documentos.md), [ADR-0071](0071-worker-de-ingesta.md), [ADR-0075](0075-medicion-de-oe6-y-evaluacion-de-recuperacion.md) |

---

## 1. Contexto

[ADR-0071](0071-worker-de-ingesta.md), regla 1, eligió `pypdf` para leer los PDF, y no dijo con cuál de sus dos modos de extracción:

- **El modo normal** (`plain`) devuelve el texto en el orden en que el PDF lo dibuja. Es el que usaba el `worker`.
- **El modo *layout*** ubica cada renglón en la columna en que está en la página, rellena con espacios para ubicarlo y deja líneas en blanco donde la página tiene un hueco vertical.

La fragmentación recursiva de [ADR-0065](0065-fragmentacion-de-documentos.md) prueba los separadores de `chunking.yaml` en orden: dos líneas en blanco seguidas (`"\n\n\n"`), una línea en blanco (`"\n\n"`), un salto de línea, `". "`, un espacio y, como último recurso, cualquier carácter. Así que dónde caen los cortes depende de los saltos de línea que deja la extracción.

Con el anteproyecto ([ADR-0075](0075-medicion-de-oe6-y-evaluacion-de-recuperacion.md), regla 4), el modo normal **pone un salto de línea entre cada dos palabras**:

| Extracción | Mediana del largo de un renglón | Espacios | Trozos que terminan a mitad de una oración |
|---|---|---|---|
| Normal (`chunking.yaml` v1) | 6 caracteres | 10 % | 90 % |
| *Layout*, tal como sale | 92 caracteres | 35 % | 29 % |
| *Layout* sin el relleno (regla 2) | 74 caracteres | 11 % | 40 % |

Con el modo normal, el separador `"\n"` está en todas partes. El recursivo corta en cualquier palabra, y la línea en blanco que separa dos párrafos no se distingue de la que separa dos palabras.

**Cómo se midió.** Con `pypdf` 6.19.0 y los parámetros de `chunking.yaml` v1: unos 500 tokens estimados a 4 caracteres por token, sin solapamiento y con los separadores de arriba. El texto se armó como lo arma el `worker`: cada página sin los espacios de los extremos, unidas por una línea en blanco, sin describir imágenes. *Renglón* es cada línea no vacía. *Espacios* es la proporción de caracteres que son un espacio. Un trozo termina *a mitad de una oración* si, sin los espacios del final, no termina en `.`, `:`, `?`, `!` ni `)`. El criterio es grueso: cuenta también como corte malo el trozo que termina en un título. Se midió con un script descartable que llama al lector y al fragmentador del `worker`, así que repetirlo es leer el anteproyecto con `worker.pdf.read` y cortar con `knowledge.chunking.split`.

Además, el modo *layout* de `pypdf` **pierde texto sin dar error** en tres casos, verificados en su código:
- no recorre los Form XObjects, donde dibujan su contenido las plantillas y los PDF combinados;
- descarta el texto rotado;
- saltea las fuentes que no puede interpretar.

Una página dibujada entera dentro de un Form XObject sale vacía.

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **El `worker` extrae el texto en modo *layout*.** Se adopta antes de indexar documentos reales |
| 2 | **Del texto en modo *layout* se quita el relleno**: cada tabulador y cada serie de espacios pasa a ser un espacio, y cada renglón pierde los márgenes. Los saltos de línea y las líneas en blanco quedan como están. Corta algo peor que el texto con el relleno, y el grupo lo prefiere así |
| 3 | **El modo es un parámetro de `rules/chunking.yaml`** (`pdf_text`, con los valores `plain` y `layout`), y su `version` pasa a 2. Cada trozo lo registra a través de `chunking_version` ([ADR-0065](0065-fragmentacion-de-documentos.md), regla 9) |
| 4 | **Cada página se lee en los dos modos**, y se queda con el texto del *layout* sólo si conserva al menos el 90 % de los caracteres del normal, sin contar los espacios en blanco. Si no, o si el modo *layout* da error, la página se lee en modo normal. El 90 % es `layout_min_ratio`, en `chunking.yaml`. El log de cada documento indexado dice cuántas páginas se leyeron en modo normal (`pages_read_plain`) |
| 5 | **El cargador rechaza un `pdf_text` desconocido o ausente, y un `layout_min_ratio` que no esté entre 0 (excluido) y 1**, y el `worker` no arranca, como con el resto de `chunking.yaml` |
| 6 | **`pypdf` queda fijo en la versión 6.19.0.** El texto del modo *layout* depende de la versión, y `chunking_version` no la registra: subirla es subir también la `version` de `chunking.yaml` |
| 7 | **Las tablas y las columnas quedan como las deja la regla 2**, con un espacio entre celda y celda |

## 3. Justificación

- **El modo *layout* porque deja los saltos de línea donde los tiene la página.** Con renglones enteros y líneas en blanco entre párrafos, los separadores de ADR-0065 vuelven a significar lo que dicen.
- **Antes de indexar documentos reales** porque cambiarlo después obliga a reindexar todo lo que ya se cargó, y hoy la base está vacía.
- **Quitar el relleno aunque corte peor (40 % contra 29 %)** porque el relleno no es texto. Los espacios que ubican un renglón en su columna son un tercio de los caracteres. Inflan el conteo de tokens, que se estima por caracteres ([ADR-0071](0071-worker-de-ingesta.md), regla 2), y llegan a Voyage y al prompt. Con el relleno, un trozo de "500 tokens" tiene un tercio menos de texto, y puede que corte mejor en parte por eso.
- **En `chunking.yaml` y no en el código** porque cambia lo que se corta tanto como un separador. Versionado ahí, una medición de OE6 sabe con qué extracción se hicieron los trozos que recuperó.
- **Los dos modos por página** porque el modo *layout* pierde texto en silencio, y una página sin texto es peor que una con los saltos de línea mal puestos. Comparar los caracteres sin espacios es la medida en la que los dos modos deberían coincidir. El margen del 10 % deja pasar una marca de agua rotada, que el *layout* descarta a propósito.
- **Fijar `pypdf`** porque la reproducibilidad de `chunking_version` depende de que el mismo PDF dé el mismo texto. Hoy lo garantiza `poetry.lock`, pero un `poetry update` lo rompería sin aviso.
- **Las tablas como quedan** porque la alternativa, marcar las celdas, no se puede hacer con lo que da el modo *layout*: el texto justificado también deja de tres a seis espacios entre palabras, así que no hay forma de distinguir un límite de celda de un espacio de la prosa.

## 4. Consecuencias

### Positivas

- En el anteproyecto, los trozos que terminan a mitad de una oración bajan del 90 % al 40 %, y el trozo más chico pasa de 45 a 108 tokens estimados.
- El texto que ven Voyage y el modelo es el de la página, en renglones, y no una palabra por línea.
- Una página dibujada en un Form XObject, o con texto que el *layout* pierde, conserva su texto. Ninguna de las 20 páginas del anteproyecto cayó al modo normal.

### Negativas / costos asumidos

- **Está medido sobre un solo documento**, el anteproyecto. Que el modo normal separe cada palabra puede ser propio de cómo se generó ese PDF. En otros documentos, la diferencia puede ser menor, o puede ser en contra.
- **Ese documento es también el único de la evaluación diferida** ([ADR-0075](0075-medicion-de-oe6-y-evaluacion-de-recuperacion.md), regla 4). La extracción, y las alternativas descartadas en §5, se eligieron mirando el mismo texto sobre el que después se va a medir la *recall@k*. Ese número va a salir más alto de lo que saldría sobre un documento que no se usó para ajustar nada.
- **El 40 % sigue siendo alto.** El renglón de un PDF termina donde termina el ancho de la página, no donde termina la oración. Un párrafo más largo que un trozo se corta en un renglón cualquiera. Contradice el argumento con el que ADR-0065 justificó no solapar, y el solapamiento queda en 0 igual: lo que se pierde en esos cortes no lo mide nada.
- **Quitar el relleno corta peor que dejarlo** (40 % contra 29 %). Se acepta para no inflar los tokens ni mandar espacios a Voyage y al prompt.
- **Las tablas y las columnas pierden su forma.** Las celdas de un renglón quedan separadas por un solo espacio, igual que las palabras. En una lista de precios, si falta una celda o el encabezado cae en otro trozo, el modelo puede atribuirle un precio a la propiedad equivocada.
- **Dos columnas de texto pueden quedar mezcladas por renglón.** El modo *layout* lee la página de izquierda a derecha, así que un renglón junta el de la columna izquierda con el de la derecha. Las fichas a dos columnas son comunes en una inmobiliaria.
- **Leer cada página dos veces casi duplica el costo de extraer.** En el anteproyecto, 6,1 s contra 3,6 s del modo normal, con las imágenes incluidas. En el `worker` no hay un usuario esperando, pero una base grande tarda más en indexarse.
- **La pérdida parcial que no llega al 10 % no se recupera.** Una tabla rotada en una página de mucho texto, o un recuadro dentro de un Form XObject, se pierde si el resto de la página alcanza el 90 %. El 90 % es una heurística, elegida sin datos.
- **Un documento puede tener páginas leídas con cada modo**, por la regla 4, y `chunking_version` dice 2 en todos sus trozos. Sólo el log sabe cuántas, y no cuáles.
- **Una actualización de seguridad de `pypdf` obliga a subir la versión de `chunking.yaml`** y a reindexar todos los documentos.
- **Las líneas en blanco salen del espacio vertical de la página.** Un título con poco aire antes no se separa del párrafo anterior, y un interlineado amplio puede partir un párrafo en dos.
- **Un hueco vertical grande dentro de una página corta antes que un cambio de página.** Sólo ese hueco produce dos líneas en blanco seguidas (`"\n\n\n"`), que es el primer separador. El `worker` une las páginas con una sola, el separador de párrafo.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Seguir con el modo normal** | En el anteproyecto, nueve de cada diez trozos se cortan a mitad de una oración |
| **El modo normal, juntando los renglones cortos** | Es una heurística propia para adivinar qué salto de línea es real, cuando el modo *layout* ya lo sabe por la posición en la página |
| **El modo *layout* con el relleno** | Corta mejor (29 %), pero un tercio de los caracteres son espacios, que inflan la estimación de tokens y llegan a Voyage y al prompt |
| **El modo *layout* sin las líneas en blanco del espacio vertical** | Probado sobre el anteproyecto: los trozos a mitad de oración vuelven al 84 %, porque sin líneas en blanco no hay párrafos que respetar |
| **Juntar las líneas en blanco seguidas en una sola** | Probado sobre el anteproyecto: no mejora los cortes (41 % contra 40 %), y el hueco vertical grande deja de cortar antes que un párrafo |
| **Caer al modo normal sólo cuando el *layout* da error** | Era la primera versión de la regla 4. No cubre las páginas que el *layout* devuelve vacías o cortas sin dar error |
| **Conservar el texto rotado** (`layout_mode_strip_rotated=False`) | Recupera las tablas rotadas, y mete en el texto las marcas de agua, como "BORRADOR", que suelen ir rotadas |
| **Marcar las celdas con `" \| "` donde haya tres espacios o más** | El texto justificado del anteproyecto deja de tres a seis espacios entre palabras: partiría la prosa en celdas |
| **Volver a comparar el solapamiento** | Obliga a implementarlo, y el grupo decidió no tenerlo ([ADR-0075](0075-medicion-de-oe6-y-evaluacion-de-recuperacion.md), regla 5) |
| **Cambiar de biblioteca, a PyMuPDF** | Extrae mejor, pero es AGPL ([ADR-0071](0071-worker-de-ingesta.md)) |
