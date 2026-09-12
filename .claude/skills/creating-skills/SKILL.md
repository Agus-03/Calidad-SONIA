---
name: creating-skills
description: 'Crea una skill nueva de Claude Code en `.claude/skills/` con las reglas de este repositorio: decide primero si corresponde una skill, escribe la evaluación antes que la skill, arma el SKILL.md sin datos que caduquen y lo valida con el parser de Claude Code. Usar cuando se pida crear, escribir, incorporar o corregir una skill del proyecto.'
argument-hint: <nombre-de-la-skill> <para qué sirve>
---

# Crear una skill — SONIA

Esto no enseña el formato de una skill: Claude ya lo conoce. Enseña las reglas de este repositorio, cada una salida de una falla que ya ocurrió acá. La skill a crear es `$ARGUMENTS`; si viene vacío, preguntá nombre y propósito antes de seguir.

Copiá este checklist en tu respuesta y marcá cada paso al cumplirlo:

```
- [ ] 1. ¿Corresponde una skill?
- [ ] 2. Evaluación escrita (evals.md)
- [ ] 3. Sin duplicar lo que ya existe
- [ ] 4. Frontmatter
- [ ] 5. Cuerpo
- [ ] 6. Validación con el parser
- [ ] 7. Verificación de identificadores
- [ ] 8. Entrega
```

## 1. ¿Corresponde una skill?

Una skill es para un **procedimiento que se repite** o conocimiento del repositorio que se usa a veces. Si el pedido es otra cosa, decilo, nombrá el mecanismo que corresponde y terminá sin crear nada:

| Si lo que se quiere es… | Va en |
|---|---|
| Un hecho que vale en toda sesión | `CLAUDE.md` |
| Revisar con contexto limpio, o restringir herramientas | un subagente en `.claude/agents/` |
| Algo que tiene que cumplirse sí o sí | un hook o `permissions` en `.claude/settings.json` |
| Acceso a un sistema externo | un servidor en `.mcp.json` |
| Conocimiento general que Claude ya tiene (Go idiomático, qué es un ADR) | nada: una skill así sólo cuesta contexto |

## 2. Evaluación antes que la skill

Una skill escrita sin fallas observadas documenta problemas imaginados. Antes de escribir una línea del SKILL.md:

1. Pedí al usuario, o reconstruí de la conversación, **qué salió mal sin la skill**: qué hizo Claude mal o qué hubo que repetirle. Si nadie puede nombrar una falla concreta, decilo y no crees la skill.
2. Escribí `.claude/skills/<nombre>/evals.md` con **tres escenarios**. Cada uno lleva el pedido tal como lo haría una persona, lo que se espera como lista verificable, y lo que pasó sin la skill.
3. `evals.md` no se referencia desde el SKILL.md: así nunca se carga durante el uso, y queda versionado para volver a probar cada vez que la skill cambie.

## 3. Sin duplicar

- Leé las skills y los agentes que ya existen (`Glob` sobre `.claude/skills/*/SKILL.md` y `.claude/agents/*.md`). Si uno ya cubre el pedido, el cambio es sobre ése, no una skill nueva.
- Las reglas del proyecto tienen dueño: `docs/design/08-structure-rules.md` (estructura), `docs/design/00-convenciones.md` (C01–C10), `go-conventions` y `python-conventions` (idioma del código). **La skill remite a ellos, nunca los copia:** una regla copiada es una regla que después se contradice con su original.

## 4. Frontmatter

```yaml
---
name: <nombre>
description: '<qué hace, en tercera persona>. Usar cuando <disparadores concretos>.'
---
```

- **La `description` va siempre entre comillas simples.** En Windows git deja los archivos con finales de línea CRLF (`core.autocrlf`), y con CRLF un `: ` sin comillas rompe el YAML: Claude Code carga la skill con la metadata vacía —o saltea el agente entero— sin avisar. Con LF el mismo archivo pasa, así que en Mac o Linux el error no se ve: por eso las comillas van siempre, no sólo cuando falla. Ya pasó con las cinco piezas que había en `.claude/`. Un apóstrofo literal adentro se escribe doble: `''`.
- `name`: minúsculas, dígitos y guiones, hasta 64 caracteres, sin `claude` ni `anthropic`. Preferí gerundio (`writing-adrs`, `adding-bounded-context`).
- `description`: tercera persona, **qué hace y cuándo usarla**, con el disparador principal primero; hasta 1.024 caracteres. En español, como toda la documentación (C02).
- `disable-model-invocation: true` hace que la skill sólo se invoque a mano con `/<nombre>`: Claude nunca la dispara solo, y su descripción deja de ocupar contexto en cada turno. El precio es que Claude tampoco la ve si alguien la pide con palabras. Se decide por skill: **manual** cuando dispararla sin querer es caro —abrir un ADR saca una decisión de las manos del grupo, publicar no se deshace—; **automática** cuando lo caro es que no se aplique al pedirla con palabras. Esta skill es automática por eso.
- `paths:` en las skills de convenciones (`paths: ["src/core/**/*.go"]`), para que carguen solas al tocar esos archivos.
- `argument-hint` y `$ARGUMENTS` si la skill recibe un parámetro.
- `allowed-tools` sólo si hace falta, y acotado: pre-aprueba herramientas sin preguntar. Nunca un `Bash` amplio.

## 5. Cuerpo

- **Procedimiento, no enciclopedia:** pasos en orden, un checklist copiable si son más de tres, y un paso final de validación que se pueda correr (`go vet ./...`, `make test`, `make contracts-check`). Menos de 500 líneas.
- **Nada que caduque.** Sin "hoy", "todavía", "ya existe", ni cantidades que cambian ("las doce reglas"). Si un dato depende del estado del repo, la skill dice **cómo verificarlo** (`Glob`, `Grep`), no cuál es. Así caducaron las dos skills de convenciones: citaban una docena de identificadores que el código ya no tenía.
- **Las reglas se citan por su título** (*«Plano hasta que duela»*), nunca por número: los números ya se corrieron una vez.
- **Los ejemplos son del repositorio, reales y verificados:** un paquete concreto, un archivo concreto. Nunca uno inventado para ilustrar.
- No expliques lo que Claude ya sabe.
- Cada párrafo en una sola línea: el SKILL.md se lee como prompt, y un salto de línea dentro de un párrafo cambia la estructura que el modelo ve.
- Archivos de referencia a un solo nivel de profundidad desde el SKILL.md, con nombres que digan qué contienen.
- **Scripts**, si la skill hace algo determinista: en `scripts/` dentro de la skill, invocados con `bash "${CLAUDE_SKILL_DIR}/scripts/<script>.sh"` —con `bash` delante, porque git en Windows no conserva el bit de ejecución—, y sin depender de `python` ni `jq`, que no están en todas las máquinas del equipo. El precedente es `.claude/hooks/git-read-only.sh`.

## 6. Validación con el parser de Claude Code

Corré `bash "${CLAUDE_SKILL_DIR}/scripts/validate.sh"`. Valida el frontmatter de todas las skills y de todos los agentes con el mismo parser que usa Claude Code al cargarlos. Si falla, arreglá y volvé a correrlo: no se entrega una skill que no pasa.

## 7. Verificación de identificadores

Por cada ruta, archivo, función, tipo, comando o documento que el SKILL.md nombre, confirmá con `Glob` o `Grep` que existe **ahora**. El que no existe se corrige o se saca; nunca se deja "porque va a existir".

## 8. Entrega

- No commitees. Mostrá lo creado, contá qué evaluación la justifica y dá el commit como un comando de una sola oración, sin cuerpo (regla *Commits de una sola oración* de `CLAUDE.md`).
- Recordale al usuario la prueba que falta y que no podés hacer desde esta sesión: abrir una sesión nueva, confirmar que la skill aparece —salvo que tenga `disable-model-invocation`—, correr los tres escenarios de `evals.md` y comparar contra lo que pasó sin la skill.

## Skills de terceros

Si el pedido es instalar una skill ajena en vez de crearla: sólo de fuentes confiables —las del proyecto o las oficiales de Anthropic—, instalada en el proyecto y copiada, no global ni enlazada (`npx skills add <origen> --skill <nombre> -a claude-code --copy`, con `DISABLE_TELEMETRY=1`), y **leyendo todos sus archivos antes de commitear**, scripts incluidos. Una skill trae instrucciones y código que corren con los permisos de Claude.
