# ADR-0016 — Rastro del pipeline de respuesta

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · la semántica de `pipeline_steps.version` enmendada por [ADR-0042](0042-pipeline-de-decision-del-turno.md) — en la corrida de salida de `blocklist` la columna nombra **dos** archivos versionados, `1+profanity1`— y la enmienda extendida por [ADR-0043](0043-prompt-injection-en-el-guardrail-de-entrada.md) a la corrida de entrada del `guardrail`, que va a nombrar `guardrails.yaml` y `injection.yaml` cuando se construya la detección de inyección |
| **Fecha** | 2026-08-10 · enmendada dos veces el 2026-09-25 (Fase 8) |
| **Etapa** | Diseño — Paso 6 (validación cruzada de las métricas) |
| **Resuelve** | **H-23** — la configuración de recuperación del RAG no se congela con la evidencia. Habilita además la definición no circular de OE6 (**H-24**) |
| **Requisitos afectados** | OE6 · F06, F12 |
| **Naturaleza** | Decisión de ingeniería del equipo. **El pipeline no está en el anteproyecto** |

---

## 1. Contexto

El equipo definió el pipeline de respuesta del agente:

```
Pregunta → Blocked-list → Guardrail → Triage → RAG → Grounding (Reranking si hace falta)
         → Blocked-list → Guardrail → Respuesta
```

**El catálogo modelaba uno de esos ocho pasos.** `retrieval_logs` guarda qué fragmentos volvió el RAG y `generations` guarda qué respondió el modelo. Todo lo demás —si una lista de bloqueo detuvo el mensaje, cómo clasificó el triage, si un guardrail modificó la salida, si había evidencia suficiente— no dejaba rastro.

Dos consecuencias, y la segunda es la que fuerza la decisión:

**Primera: H-23.** Ni el `top_k` ni el umbral de similitud existen en ninguna tabla. Sin ellos no se distingue *"volvió un solo fragmento porque sólo uno superó el corte"* de *"volvió uno porque se pidió uno"*, y mover cualquiera de los dos durante la Fase 4 vuelve incomparables las mediciones anteriores **sin dejar rastro**. Es la misma asimetría que H-21: `generations` congela los parámetros del LLM y no los del RAG, habiendo gobernado los dos exactamente el mismo turno.

**Segunda: OE6 no tiene denominador honesto sin esto.** OE6 mide *"el 90 % de las consultas **dentro del dominio del negocio** respondidas sin escalada"*. La definición obvia de "dentro del dominio" —el RAG recuperó algo por encima de un umbral— es **circular**: deja que el sistema decida qué preguntas le corresponde saber y después se evalúe únicamente sobre ésas, de modo que **mejora su métrica justo cuando falla en recuperar**.

El pipeline ofrece la salida, porque separa dos cosas que el `similarity_score` mezcla:

| Paso | Qué decide | De qué es propiedad |
|---|---|---|
| **Triage** | Si la pregunta pertenece al dominio | De la **pregunta**, y se decide **antes** de recuperar |
| **Grounding** | Si hay evidencia suficiente para responder | Del **sistema**, y se decide después |

El denominador de OE6 sale de `triage`; lo que la métrica detecta es *triage = `in_domain`* seguido de escalada. No es circular porque el veredicto del triage es previo e independiente del resultado de la recuperación.

## 2. Decisión

**Se incorpora `pipeline_steps`** (Bloque 3 — Ejecución por turno): una fila por cada paso del pipeline ejecutado en un turno, con `step`, `direction`, `position`, `outcome`, `score`, `version`, `latency_ms` y `detail_json`.

**Cuelga de `message_batches`, no de `generations`.** Los primeros pasos pueden bloquear el mensaje y hacer que nunca exista una generación: colgarla de la respuesta sería no poder registrar precisamente los casos en que no hubo respuesta.

**`retrieval` y `generation` figuran como pasos** aunque su detalle viva en `retrieval_logs` y `generations`, para que el rastro se pueda leer completo y en orden.

**El `outcome` es `varchar` con dominio declarado por `step`**, no una enumeración: C07 no admite una cuyo conjunto de valores dependa de otra columna. Misma solución que `conversation_annotations.gold_value`.

**Los parámetros de recuperación (`top_k`, umbral) viven en el `detail_json` del paso `retrieval`**, con lo que H-23 queda cubierto. Si el Paso 7 muestra que las consultas los necesitan de forma relacional, se promueven a columnas ahí.

## 3. Justificación

- **Es el mismo error que ya corregimos cuatro veces.** H-18, H-20, H-21 y H-23 son todos *"se guardó el resultado de una decisión y no la evidencia sobre la que se tomó"*. El pipeline entero era la versión grande de ese error: ocho decisiones por turno, una registrada.
- **Destraba OE6 sin circularidad**, que es la única salida que no exige inventar un criterio ajeno al sistema.
- **Registrar un paso de ocho habría sido peor.** La alternativa acotada —dos columnas para congelar el `top_k`— arreglaba el quinto paso dejando invisibles el primero, el segundo, el tercero y el sexto. Un modelo que registra un tramo arbitrario de un proceso es más engañoso que uno que no registra nada, porque parece completo.
- **Sirve más allá de OE6:** latencia por paso (qué tramo es el lento), forense ante el cooperante (*"¿por qué el bot no me contestó eso?"*) e insumo directo para el Paso 12, donde los guardrails y las listas de bloqueo son justamente lo que se dibuja como control en las zonas de confianza.

## 4. Consecuencias

### Positivas

- El turno queda auditable de punta a punta, en orden y con tiempos.
- OE6 pasa a tener un denominador defendible.
- H-23 queda cubierto sin agregar columnas de configuración sueltas.
- `03-capa-prompts.md` §3 tenía el reranking y los guardrails anotados como *"extensiones identificadas, no modeladas todavía"*. Dejan de estarlo: su rastro ya tiene dónde vivir, aunque su diseño se profundice después.

### Negativas / costos asumidos

- **Volumen:** entre siete y nueve filas por turno. Con ~300 bloques diarios son ~2.500 filas/día, cerca de un millón al año. Irrelevante para PostgreSQL, pero **exige política de retención en el Paso 7** — es la tabla que más rápido crece del modelo después de `messages`.
- **`PipelineStep` va a cambiar.** El pipeline todavía no está dibujado: eso es el **Paso 8**. Se está fijando la enumeración antes de tener el diagrama, y es probable que aparezcan pasos, se dividan otros o cambien nombres. Se asume conscientemente: no hay datos, y ampliar una enumeración es barato frente a descubrir en producción que no se registró nada.
- **Se modela algo que el anteproyecto no pide.** Blocked-list, guardrail, triage y grounding son decisiones del equipo. Queda declarado en el catálogo y acá, igual que se hizo con `accounts`: es preferible a presentarlo como si derivara de un requisito.
- **`detail_json` es un cajón.** Guardar ahí el `top_k` y el umbral es correcto para el modelo lógico, pero un `jsonb` no tiene integridad ni tipos. Si esos parámetros terminan siendo insumo de consultas frecuentes, hay que promoverlos.
- **Dos filas señalan hacia otras tablas** (`retrieval`, `generation`). Es redundancia deliberada a cambio de que el rastro se lea entero.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **No modelar nada ahora; dibujar el pipeline en el Paso 8 y decidir después** | Era la recomendación inicial y sigue siendo razonable: elegir qué persistir sin el diagrama es adivinar. Se descarta por irreversibilidad — el Paso 8 llega antes de implementar, pero el costo de equivocarse en cada dirección es asimétrico: una enumeración de más se amplía, un rastro que no se registró durante la Fase 4 no se reconstruye. |
| **Sólo `generations.rag_top_k` y `rag_min_similarity`** | Cubre H-23 con dos columnas y deja invisibles los otros siete pasos. Un rastro parcial de un proceso se lee como completo y es más engañoso que la ausencia total. |
| **Sólo el veredicto del triage** (lo mínimo que OE6 necesita) | Más barato, pero decide a ciegas cuál es el único paso que vale la pena registrar, sin tener el diagrama del Paso 8 que permitiría afirmarlo. |
| **Colgar la entidad de `generations`** | Impide registrar precisamente los turnos en que el pipeline bloqueó el mensaje y no hubo generación — que son los casos más interesantes para auditar. |
| **Una tabla por paso** (`guardrail_events`, `triage_results`, …) | Da tipos fuertes por paso, pero multiplica entidades para un proceso todavía no dibujado, y obliga a reconstruir el orden del turno con uniones entre tablas heterogéneas. |
