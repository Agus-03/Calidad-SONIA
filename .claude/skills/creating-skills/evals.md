# Evaluación de `creating-skills`

Las tres fallas que motivaron la skill ocurrieron en el repositorio antes de que existiera: las cinco piezas de `.claude/` tenían la `description` sin comillas y, con los finales de línea CRLF de un checkout en Windows, no cargaban; las dos skills de convenciones citaban una docena de identificadores que el código ya no tenía; y los agentes citaban las reglas de estructura por un número que se había corrido.

## Escenario 1 — descripción con dos puntos

**Pedido:** "Creá una skill para escribir ADRs: plantilla, registro en el README y consecuencias negativas."

**Se espera:**
- La `description` va entre comillas simples, aunque tenga `: ` adentro.
- `scripts/validate.sh` corre y pasa antes de entregar.
- Se decide y se justifica `disable-model-invocation` (abrir un ADR saca la decisión de las manos del grupo: manual).
- Remite a `docs/design/02-decisiones/README.md` en vez de copiar su convención.

**Sin la skill:** la descripción salió sin comillas; en el checkout de Windows, con CRLF, el YAML no parseó y la skill cargó con la metadata vacía, sin ningún aviso. En LF el mismo archivo pasaba, así que el error dependía de la máquina.

## Escenario 2 — datos que caducan

**Pedido:** "Creá una skill con el procedimiento para agregar un contexto nuevo en `src/core/internal`."

**Se espera:**
- Todo archivo, tipo o función que nombra existe, verificado con `Glob`/`Grep` en la misma corrida.
- Ningún "hoy", "todavía" ni cantidad que cambie; lo que depende del estado del repo se indica cómo verificarlo.
- Las reglas de estructura se citan por título, no por número.
- Toma un paquete existente como ejemplo en vez de inventar uno.

**Sin la skill:** `go-conventions` citaba `PromptPreviewer`, `generationSelect` y `needsNewVersion`, que no existían, y "las diez reglas" cuando ya eran doce.

## Escenario 3 — no corresponde una skill

**Pedido:** "Creá una skill que le enseñe a Claude buenas prácticas generales de Go."

**Se espera:**
- Responde que no corresponde: es conocimiento que Claude ya tiene, y el idioma del repo ya está en `go-conventions`.
- No crea ningún archivo.

**Sin la skill:** no hay precedente observado. Es el caso que evita sumar skills que sólo cuestan contexto; si falla en la práctica, se reemplaza por uno observado.
