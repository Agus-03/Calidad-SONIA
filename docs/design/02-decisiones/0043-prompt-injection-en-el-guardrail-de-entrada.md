# ADR-0043 — Prompt injection: la detecta el guardrail de entrada, y corta el turno

| Campo | Valor |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 2026-09-25 |
| **Etapa** | Construcción — Fase 8 (pipeline de decisión). **Se decide acá y se construye en la fase de seguridad** |
| **Resuelve** | La decisión O3 que [ADR-0042](0042-pipeline-de-decision-del-turno.md) dejó con ADR propio: quién mira una inyección de prompt, en qué paso del pipeline, qué hace al encontrarla y qué deja escrito |
| **Requisitos afectados** | F01 · F03 · OE1 · OE2 — **ninguno la pide**, los cuatro la necesitan (ver *Naturaleza*) |
| **Naturaleza** | **Decisión de ingeniería del equipo. El anteproyecto no nombra la inyección de prompt en ninguna parte**, igual que el pipeline de [ADR-0016](0016-rastro-del-pipeline-de-respuesta.md). Su único riesgo de seguridad declarado (§5.2) es el dato sensible que viaja al modelo externo, que es territorio de [ADR-0006](0006-anonimizacion-de-pii.md); la única otra mención de seguridad es *"pruebas de carga y seguridad"* como entregable de una fase. Queda dicho acá antes que preguntado en la defensa |
| **Enmienda** | [ADR-0016](0016-rastro-del-pipeline-de-respuesta.md) — **la `version` compuesta de `pipeline_steps` deja de ser de un solo paso.** [ADR-0042](0042-pipeline-de-decision-del-turno.md) la introdujo para la corrida de salida de `blocklist`; la regla 4 de este ADR la extiende a la corrida de entrada del `guardrail`, que va a decidir con `guardrails.yaml` y con `injection.yaml`. El argumento sigue siendo el mismo, y lo que cambia es que ahora son **dos** los pasos que nombran dos archivos · [ADR-0042](0042-pipeline-de-decision-del-turno.md), **decisión D3 y la tabla de §2.4** — D3 cierra diciendo que *"a la entrada sustituye el dato y deja seguir el turno (`modified`); a la salida rechaza la respuesta entera (`blocked`)"*, y **la primera mitad deja de ser cierta**. El `guardrail` de entrada pasa a poder cortar, y el único motivo por el que corta es una inyección: la redacción de ADR-0006 sigue sustituyendo y dejando seguir el turno (`modified`), exactamente como D3 lo decidió. **La segunda mitad de D3 no se toca**, y tampoco el argumento que la sostiene: a la salida no se reescribe porque ese texto ya pasó por el proveedor |
| **Relacionada con** | [ADR-0006](0006-anonimizacion-de-pii.md), [ADR-0016](0016-rastro-del-pipeline-de-respuesta.md), [ADR-0026](0026-conjuntos-de-evaluacion-simulados.md), [ADR-0028](0028-cobertura-de-pruebas-por-servicio.md), [ADR-0042](0042-pipeline-de-decision-del-turno.md), [03-capa-prompts.md](../03-capa-prompts.md), [07-secuencia-del-turno.md](../07-secuencia-del-turno.md) |

---

## 1. Contexto

El `guardrail` de entrada que construyó la Fase 8 es **sólo una red de PII**: aplica los patrones de [ADR-0006](0006-anonimizacion-de-pii.md), sustituye lo que encuentra y deja seguir el turno. Nunca corta, por decisión D3.

Eso deja una anomalía escrita en el catálogo: **`guardrail` + `inbound` + `blocked` es un valor declarado del dominio de `outcome` que nada puede producir.** No es un descuido — ese valor existe porque el dominio se escribió contando con que algo, alguna vez, iba a rechazar un mensaje entrante entero. Hoy nada lo hace, y nada mira un mensaje que trae instrucciones para el modelo.

**Hay que decir de entrada de dónde sale esto, porque no sale del anteproyecto.** La inyección de prompt no aparece en ninguna parte del documento. Lo que sí aparece, en la sección 5.2, es un riesgo de seguridad distinto: *"los sistemas actuales no ofrecen mecanismos de anonimización o filtrado previo al envío al modelo externo"*, que es el problema que ADR-0006 resuelve. Y una mención suelta a *"pruebas de carga y seguridad"* como entregable. Nada más. Igual que con el pipeline de ADR-0016, esto es una decisión del equipo y se registra como tal en vez de presentarla como si derivara de un requisito.

**Por qué importa igual, y esto sí está anclado.** F01 arma el prompt con seis componentes que escribe el administrador, y F03 existe para que el administrador **vea exactamente** lo que se le va a decir al AIAgent antes de que llegue a una clienta. El mensaje de la clienta viaja en el mismo sobre, bajo `[MENSAJE DEL CLIENTE]`. Una clienta que escribe instrucciones está compitiendo con esa configuración dentro del mismo prompt.

Y las dos métricas que sostienen el aporte se miden sobre el supuesto de que el prompt efectivo **es** el configurado: OE1 cuenta que los seis componentes activos hayan llegado al prompt, y OE2 compara proveedores sobre la misma configuración. Una inyección hace que la configuración medida y la efectiva sean dos cosas distintas, **y hoy nada lo diría**. No es una preocupación de seguridad genérica: es un agujero en el instrumento de medición del propio trabajo.

---

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **La detección vive en el paso `guardrail` de entrada y no en un paso nuevo de `PipelineStep`.** Su `blocked`, hoy inalcanzable, pasa a ser el resultado de una inyección detectada |
| 2 | **La fila del rastro queda inequívoca sin agregar una columna:** a la entrada, `modified` es la redacción de ADR-0006 y `blocked` es una inyección. Se pueden distinguir porque D3 le sigue prohibiendo cortar a la redacción — y esa mitad de D3 es la que **no** se enmienda |
| 3 | **Los patrones y las instrucciones viven en `rules/injection.yaml`, con su propia `version`** (regla 9), y no dentro de `guardrails.yaml`. Son dos cosas con dos dueños: el contenido de `guardrails.yaml` lo declara ADR-0006 y éste es una guarda del equipo, como `profanity.yaml` frente a `blocklist.yaml` |
| 4 | **El `version` de la fila del `guardrail` de entrada pasa a ser compuesto** —`<guardrails>+injection<injection>`—, con la forma que la enmienda de [ADR-0042](0042-pipeline-de-decision-del-turno.md) a ADR-0016 ya admite para la corrida de salida de `blocklist` |
| 5 | **Corta el turno y no lo reescribe.** Una inyección detectada no se sanitiza |
| 6 | **Dos capas, y en este orden: heurísticas primero; el paso LLM sólo si la fase de seguridad demuestra que las heurísticas no alcanzan.** Las heurísticas son determinísticas y gratis; la llamada extra antes de generar es el costo que O3 declaró, y se paga contra evidencia y no por las dudas |
| 7 | **Necesita su entrada `inbound.guardrail` en `rules/notices.yaml`**, y la línea del encabezado de ese archivo que hoy dice que este paso *"redacta, nunca corta, así que un texto para él sería texto que nadie puede alcanzar"* deja de ser cierta. El texto **no dice qué se detectó**, por la misma regla que ya rige a los otros tres |
| 8 | **`detail_json` guarda el nombre de la regla que coincidió y nada más.** Nunca el texto de la inyección: es el texto de la clienta, y es el mismo argumento de ADR-0006 que ya se discutió para `generations.completion_text` |
| 9 | **El detector se evalúa con un conjunto propio, y hoy no existe.** [ADR-0026](0026-conjuntos-de-evaluacion-simulados.md) simula los de OE3 y OE4 y no éste. La fase de seguridad lo construye, o declara por escrito que el detector no tiene medida |
| 10 | **Se construye en la fase de seguridad, no en la Fase 8.** Esta fase deja la decisión escrita y ni una línea de código |

---

## 3. Justificación

**Por qué no un paso nuevo, que era la alternativa obvia.** Tres razones, y la primera es la que decide. **Un paso nuevo dejaría `guardrail` + `inbound` + `blocked` huérfano para siempre**: un valor declarado en el dominio de `outcome` que ningún camino puede producir, lo que es peor que el estado actual, porque hoy al menos hay un ADR pendiente que explica por qué está ahí. Segundo, el costo: un valor nuevo de `PipelineStep` toca `contracts/enums.yaml`, el `CHECK` de la migración `0001` y los dos lenguajes generados — exactamente el costo que la Fase 7 **ya rechazó pagar** para registrar el clasificador de intenciones, y revertir ese criterio para otro paso sin un argumento nuevo sería incoherente. Tercero, no hace falta: la regla 2 muestra que la fila se lee sin ambigüedad con las columnas que ya están.

**Por qué corta en vez de sanitizar.** Quitarle las instrucciones a un mensaje deja un texto que nadie escribió, y el modelo lo responde igual — con la diferencia de que ahora nadie sabe qué se le preguntó. Es el mismo razonamiento por el que D3 decidió que a la salida no se reescribe: una sustitución sólo es honesta cuando lo que reemplaza es un dato, no una intención. Y hay una asimetría que conviene decir: la redacción de ADR-0006 sustituye **algo que la clienta quiso decir**; acá lo que hay que sacar es justamente lo que la clienta quiso que el sistema hiciera, y lo que queda después de sacarlo no es su mensaje.

**Por qué un archivo propio.** Es el mismo reparto de dueños que resolvió O6b con `profanity.yaml`: el contenido de `guardrails.yaml` no es nuestro —ADR-0006 declara los cuatro datos y sus marcadores— y los patrones de inyección sí lo son. Mezclarlos haría que un ajuste de nuestra guarda mueva la versión de los patrones que declara otro ADR, y al revés. El precio es un `version` compuesto, que es un precio que esta fase ya decidió pagar una vez y por el mismo motivo.

**Por qué las heurísticas primero.** El paso LLM es una llamada más sobre el camino de entrada de **todos** los turnos, incluido el 99 % que no trae nada. O3 declaró ese costo y lo aceptó en principio; lo que este ADR agrega es el orden: primero lo que no cuesta, y la llamada cuando haya un conjunto de evaluación que muestre qué se está escapando. Comprar la latencia antes de poder medir la mejora es comprarla a ciegas.

---

## 4. Consecuencias

### Positivas

- **Un valor declarado deja de ser inalcanzable.** `guardrail` + `inbound` + `blocked` pasa a tener un camino, que es la razón por la que se lo escribió.
- **Sin DDL, sin enumeración nueva, sin regenerar el contrato.** La decisión entra con un archivo de reglas y una entrada en `notices.yaml`.
- **La fila del rastro sigue diciendo qué pasó** con las columnas que ya tiene: `modified` es la redacción, `blocked` es la inyección.
- **Los patrones se ajustan editando datos y desplegando**, nunca editando Python (regla 9), que es lo que hace viable corregir un falso positivo el día que aparezca.
- **El pipeline queda con una guarda sobre lo único que el trabajo aporta de nuevo**: la composición del prompt.

### Negativas / costos asumidos

- **D3 pierde la mitad que lo hacía simple.** Hoy se puede afirmar *"un guardrail de entrada nunca corta"*, y deja de ser cierto. Quien lea el código de la Fase 8 y el de la fase de seguridad va a encontrar dos reglas distintas para el mismo paso, y lo único que las reconcilia es este ADR.
- **Un paso con dos trabajos y dos archivos**, y con él vuelve el `version` compuesto: dos números en una columna `varchar` que nadie puede ordenar ni comparar por rango. Es el costo que la decisión O6b ya aceptó, pagado una segunda vez.
- **`latency_ms` de esa fila pasa a medir dos cosas.** La redacción y la detección quedan sumadas, así que el costo de lo que se agregue **no se puede aislar** del que ya estaba. Es el precio directo de no haber hecho un paso aparte, y conviene tenerlo escrito antes de que alguien intente medirlo.
- **Un falso positivo amordaza un mensaje legítimo, y es peor que el de la lista de malas palabras.** Aquél corta después de haber pagado la generación; éste corta **antes**, así que la pregunta de la clienta no llega a hacerse nunca y lo único que recibe es el texto fijo.
- **Una inyección que pasa es invisible.** El paso escribe `pass` y nada dice que no la vio. El único rastro de que el prompt efectivo no fue el configurado sería la respuesta misma, que es justamente lo que OE1 y OE2 suponen limpio.
- **El detector no tiene medida.** Sin conjunto de evaluación, *"detecta inyección"* es una afirmación sin número, en un trabajo que defiende su aporte con números. Queda como pendiente explícito de la fase de seguridad y no como algo que se dará por hecho.
- **Sigue sin resolverse quién hace el *checkpoint* de la regla 4** (decisión O10): la red de PII de este paso es hoy lo único que sustituye algo, y darle un segundo trabajo lo vuelve más difícil de mover el día que ese *checkpoint* exista.
- **Se decide algo que no se construye**, con el riesgo que eso siempre tiene: entre esta fase y la de seguridad el código puede moverse lo suficiente como para que el paso elegido ya no sea el lugar cómodo. Se asumió porque la alternativa —decidirlo cuando se construya— deja el valor huérfano sin explicación durante cuatro fases.

---

## 5. Alternativas descartadas

| Alternativa | Por qué no |
|---|---|
| **Un valor nuevo `injection` en `PipelineStep`** | Deja `guardrail` + `inbound` + `blocked` huérfano para siempre, y cuesta cuatro lugares —`contracts/enums.yaml`, el `CHECK` de `0001` y los dos lenguajes generados— que la Fase 7 ya se negó a pagar por el clasificador de intenciones. Lo que sí ganaba, y se pierde: latencia y versión propias, y un `outcome` con su dominio limpio. |
| **Sanitizar el mensaje en vez de cortar** | Lo que queda después de sacarle las instrucciones no es el mensaje de la clienta, y el modelo lo responde igual sin que nadie sepa qué se le preguntó. |
| **Los patrones dentro de `guardrails.yaml`** | Mezcla una guarda nuestra con un contenido que declara ADR-0006, y hace que ajustar una mueva la versión de la otra. Es el mismo error que O6b evitó con `profanity.yaml`. |
| **El paso LLM desde el principio** | Una llamada más antes de generar en **todos** los turnos, comprada antes de tener con qué medir si mejora algo. Queda como segunda capa y contra evidencia, no descartada. |
| **Detectarlo en el núcleo, antes de llamar al cerebro** | El núcleo no compone el prompt y no sabe qué es una instrucción para el modelo. Además rompería la división de ADR-0017: el que decide es el cerebro. |
| **Declararlo fuera de alcance** | Era elegible: no hay requisito que lo pida y el alcance de 7.3 es acotado. Se descartó porque dejaría sin ninguna guarda a la composición de prompts, que es el aporte original que declara la sección 6.3, y porque el valor `blocked` del guardrail de entrada quedaría en el catálogo sin que nadie pueda explicar qué hace ahí. |
