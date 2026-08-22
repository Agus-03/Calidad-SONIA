# Decisiones de diseño — SONIA

Registro de las decisiones de diseño en formato **ADR** (*Architecture Decision Record*): un archivo corto por decisión, con el contexto que la motivó, la decisión tomada, su justificación, sus consecuencias y las alternativas descartadas.

## Por qué se registran

En la defensa no alcanza con mostrar *qué* se hizo: hay que poder explicar *por qué* se hizo así y qué otras opciones se evaluaron. Una decisión sin registro se vuelve, seis meses después, indistinguible de un accidente.

Tiene además un efecto práctico inmediato: cuando dentro de tres meses alguien pregunte "¿por qué los recordatorios cuelgan del contacto y no de la conversación?", la respuesta está escrita y no hay que reconstruirla de memoria ni volver a discutirla.

## Convención

- **Archivo:** `NNNN-titulo-corto.md`, numeración correlativa de cuatro dígitos.
- **Título:** `# ADR-NNNN — Título`.
- **Metadatos:** tabla con estado, fecha, etapa, hallazgo que resuelve y requisitos afectados.
- **Secciones:** Contexto → Decisión → Justificación → Consecuencias → Alternativas descartadas.

Dos reglas que conviene sostener:

1. **Las consecuencias negativas se escriben.** Un ADR que sólo lista ventajas es publicidad, no una decisión — y es además el material que responde la pregunta "¿qué costo asumieron?" en la defensa.
2. **Un ADR no se edita cuando se cambia de opinión.** Se escribe uno nuevo que lo reemplace, y el viejo se marca como `Reemplazada por ADR-NNNN`. El historial de las decisiones que se revirtieron también es parte del trabajo.

---

## Decisiones registradas

| ADR | Título | Resuelve | Estado |
|---|---|---|---|
| [0001](0001-origen-del-contacto.md) | Origen del contacto en `contacts`, no en `sales` | H-13 | Aceptada |
| [0002](0002-ciclo-de-vida-conversacion-y-recordatorios.md) | Ciclo de vida de la conversación y vínculo de los recordatorios | H-14 | Aceptada |
| [0003](0003-etiquetas-catalogo-y-relaciones.md) | Etiquetas: catálogo único con relaciones a contactos y conversaciones | H-07, H-16 | Aceptada |
| [0004](0004-asignacion-de-conversaciones.md) | Asignación de conversaciones: pool con autoasignación | H-15 | Aceptada |
| [0005](0005-base-de-conocimiento.md) | Base de conocimiento: una por agente, con múltiples documentos | H-06 | Aceptada |
| [0006](0006-anonimizacion-de-pii.md) | Anonimización de PII antes del envío al LLM externo | — | Aceptada |
| [0007](0007-alcance-del-registro-de-auditoria.md) | Alcance del registro de auditoría | — | Aceptada |
| [0008](0008-ventana-de-24-horas.md) | Ventana de 24 horas de WhatsApp: incorporación al anteproyecto | — | Aceptada |
| [0009](0009-horario-de-atencion.md) | Horario de atención del agente: fuera del alcance | — | Aceptada |
| [0010](0010-qdrant-como-base-vectorial.md) | Qdrant como base vectorial, una colección por agente | — | Aceptada |
| [0011](0011-acceso-a-datos-sensibles.md) | Acceso a datos sensibles: acotado por asignación, no por rol | — | Aceptada |
| [0012](0012-cuenta-y-bandejas-de-entrada.md) | Cuenta y bandejas de entrada: previsión estructural para el trabajo futuro declarado | Auditoría del DER | Aceptada |
| [0013](0013-fuente-de-verdad-del-modelo.md) | La fuente de verdad del modelo es el catálogo, no el tablero | Enmienda C10 | Aceptada |
| [0014](0014-medicion-de-oe3-y-oe4.md) | Cómo se miden OE3 y OE4 | H-18 | Aceptada |
| [0015](0015-medicion-de-oe1.md) | Cómo se mide OE1 | H-20, H-21 | Aceptada |
| [0016](0016-rastro-del-pipeline-de-respuesta.md) | Rastro del pipeline de respuesta | H-23 | Aceptada |
| [0017](0017-decomposicion-en-despliegues-y-lenguajes.md) | Decomposición en despliegues y reparto de lenguajes | 8.4 y 8.5 sin decidir | **Propuesta** |

Con las primeras once queda cerrado el **Paso 3** de la etapa de diseño. Los hallazgos restantes de la [matriz de trazabilidad](../01-trazabilidad.md) no requieren decisión: se resuelven modelando, en los Pasos 4 y 5.

**ADR-0012 es posterior y de otra naturaleza:** no resuelve un hallazgo de la matriz, sino uno de la auditoría del catálogo de entidades (Paso 5). Es además la única decisión que **toca el alcance declarado** en la sección 7.3 del anteproyecto, y por eso conviene que el equipo la lea completa antes de darla por firmada.

**ADR-0014 y ADR-0015 son la serie del Paso 6.** Nacen de intentar escribir la consulta SQL de una métrica y descubrir que no se podía — que es exactamente para lo que existe ese paso. Las dos corrigen **el mismo error de fondo**: el modelo guardaba el resultado de una decisión y no la evidencia sobre la que se tomó, y la consecuencia era en ambos casos una métrica que sólo podía devolver el número deseado. Es previsible que aparezcan más: la validación de OE2, OE6 y OE7 todavía no se hizo.

**ADR-0015 enmienda un documento vigente**, la Regla 2 de [03-capa-prompts.md](../03-capa-prompts.md). Se dejó escrito el razonamiento original y por qué estaba incompleto, en vez de corregirlo en silencio.

**ADR-0017 es de otra clase y por eso está en estado Propuesta.** No resuelve un hallazgo del modelo de datos: decide la arquitectura de despliegue y el reparto de lenguajes, que la sección 8.4 dejó explícitamente sin definir. Toca el trabajo de los tres y conviene discutirla entre todos —y con los tutores— antes de darla por firmada. Es además la segunda decisión que **matiza el texto del anteproyecto**, después de ADR-0012: parte el Módulo 2 entre dos procesos, y señala que la justificación de Go por "concurrencia masiva" no está respaldada por el alcance declarado en 7.3.

---

## Decisiones pendientes

Ninguna de las primeras bloqueaba el avance a los Pasos 4 y 5. De las tres que abrió ADR-0014, **la regla de atribución sí toca el Paso 6**: sin ella no se puede escribir la consulta de precisión fina de F36. Las otras dos esperan al Paso 11.

### Derivadas de las decisiones tomadas

- [ ] **Vista de historial del operador.** Con F32 restringido a `admin` ([ADR-0011](0011-acceso-a-datos-sensibles.md)) y las conversaciones divididas por cada reapertura ([ADR-0002](0002-ciclo-de-vida-conversacion-y-recordatorios.md)), el operador atiende a un cliente recurrente sin ver sus conversaciones anteriores. ¿Ve al menos el listado de conversaciones previas desde el chat asignado? **Es el pendiente de mayor impacto.**
- [ ] **Registro de desencriptado de datos sensibles.** Mejora de trazabilidad, sin urgencia una vez acotado el acceso por asignación. Ver [ADR-0007](0007-alcance-del-registro-de-auditoria.md).
- [ ] **Marcador único o por tipo para CUIT/CUIL.** Comparten formato, así que la redacción sobre texto libre no puede distinguirlos. Se resuelve al modelar, en el Paso 5. Ver [ADR-0006](0006-anonimizacion-de-pii.md).
- [ ] **Método de asignación de agente cuando hay varios en una misma bandeja.** [ADR-0012](0012-cuenta-y-bandejas-de-entrada.md) define `inboxes.default_ai_agent_id`, que cubre toda la operación real: un agente atiende las conversaciones nuevas de su bandeja. Lo que queda abierto es el **único caso que necesita varios agentes sobre la misma bandeja**: la comparación de proveedores de LLM que contempla la sección 8.4 (GPT-4o / Claude / Cohere), de la que depende OE2.

  **Se decide en el Paso 11 (protocolo de evaluación), no en el modelo de datos.** Es una decisión de diseño experimental, y el principio de orden del plan —*Decisión → Dato*— advierte que modelarla antes de decidirla es dejar que el modelo la tome en silencio.

  Dos salidas aparentemente obvias **no sirven metodológicamente**, y conviene tenerlo escrito antes de llegar al Paso 11: repartir por bandeja (un número por proveedor) es autoselección, no aleatorización — el cliente elige a qué número escribe; y repartir por período (una semana cada modelo) confunde el efecto del modelo con el del período. Ambas invalidarían la comparación que sostiene OE2.

  Si el protocolo termina exigiendo trazabilidad del criterio de asignación, se resuelve con **una columna** en `conversations` (`ai_agent_assignment_reason`), no con un motor de ruteo. Un motor de reglas se descartó explícitamente: ningún requisito de F01–F39 lo pide, y reabriría [ADR-0009](0009-horario-de-atencion.md), que ya declaró el ruteo por horario fuera de alcance con el argumento de que *"construir capacidad sin requisito es alcance no justificado"*.

### Abiertas por ADR-0014 (Paso 6)

- [ ] **Regla de atribución entre señales simultáneas.** Si dos señales de F12 superan el umbral en el mismo turno hay una sola transferencia, y `transfer_events.signal_evaluation_id` apunta a una sola fila. F12 no dice cuál gana. Candidatos: mayor `score / threshold_used`, u orden de prioridad fijo. **Se resuelve en el modelo** — es la única de las tres que no puede esperar al Paso 11.
- [ ] **Definición de "cambio de intención"** (la otra mitad de OE4). Los datos existen: `detected_intents` permite derivar el cambio comparando bloques consecutivos. Pero bajo la lectura ingenua *toda* conversación tiene cambios de intención y la métrica se degrada a la tasa base. Hay que definir qué cuenta como cambio **disruptivo**. Paso 11.
- [ ] **`message_batches` no tiene `turn_number`** (**H-19**), pese a que `03-capa-prompts.md` define el turno como el bloque cerrado y procesado. Hoy `conversation_contexts` y `transfer_events` llevan cada uno el suyo por su cuenta. Lo natural sería que el turno viva en el bloque y los demás lo deriven.

### Diferidas al Paso 11 — métrica de consistencia de tono (H-22)

El recorrido del Paso 6 llegó a esta métrica y **se detuvo a propósito**. No falta un dato: falta la definición, y el instrumento de medición depende de una etapa muy posterior. Modelar antes de decidir sería dejar que el modelo tome la decisión en silencio.

- [ ] **¿Quién juzga el tono?** Tres candidatos: **evaluación humana** (sección 5.3), **LLM-as-judge** (OE2, 8.1, F36) o un **clasificador entrenado**. Anotar para entonces: el clasificador entrenado **no evita la evaluación humana, la consume** — necesita etiquetas, y esas etiquetas son juicios humanos de tono.
- [ ] **Reconciliar 5.3 con OE2 y OE7** en el informe final. La sección 5.3 promete evaluación humana al 85 %, OE2 promete LLM-as-judge al 80 % y OE7 dice 85 % sin decir quién juzga. Decidan lo que decidan, esto hay que explicarlo por escrito: **es una contradicción del propio anteproyecto, no un error de implementación**.
- [ ] **Unidad de medida: respuesta o conversación.** El modelo ya soporta ambas (`tone_evaluations.generation_id` es nullable), así que **no es una decisión de modelado**. Depende del instrumento: un juez humano puntúa conversaciones con mucho menos esfuerzo; un clasificador entrenado clasifica a la granularidad de sus etiquetas.

**Lo que ya está razonado y no hay que volver a discutir:**

1. **El número es una proporción, no un promedio.** Único anclaje textual sobre la forma del cálculo: 5.3 dice *"en más del 85 % de **las interacciones**"*. Y para una métrica llamada "consistencia", un promedio esconde justo lo que hay que detectar — un agente perfecto el 80 % del tiempo y pésimo el 20 % promedia igual que uno mediocre siempre.
2. **Si el juez devuelve un puntaje continuo, el punto de corte se congela en la fila** (`threshold_used`), por el mismo motivo que en `signal_evaluations`: si vive sólo en la rúbrica y la rúbrica cambia, las mediciones anteriores dejan de ser comparables. **No se agrega la columna hasta decidir el instrumento**, porque un clasificador que devuelve una etiqueta no la necesitaría igual.
3. **OE2 y OE7 se separan por `rubric_version` y fecha.** OE2 vence el 17/08/2026, dentro de la Fase 2 y sobre corpus controlado; OE7 mide *"conversaciones reales"* en la Fase 4 (1/03 – 6/04/2027, Anexo D). **OE7 es el único objetivo sin fecha propia**, así que ese anclaje es derivado y va declarado como supuesto, no como lectura obvia.
4. **Una consulta de tono nunca mezcla alcances.** El alcance se lee de `generation_id IS NULL`; sumar filas por respuesta con filas por conversación no da ningún número interpretable. Esta regla vale cualquiera sea la unidad que se elija.

### Diferidas al Paso 11 — qué es una "consulta dentro del dominio" (H-24)

- [x] **Definir *"dentro del dominio"* sin circularidad** → **resuelto por [ADR-0016](0016-rastro-del-pipeline-de-respuesta.md).** El veredicto del paso `triage` se emite **antes** de recuperar y es independiente de si la recuperación funcionó, así que sirve de denominador sin la circularidad de medirlo por el `similarity_score`. Queda pendiente **validarlo contra ground truth en una muestra** —agregar un valor a `AnnotationType`, hoy `transfer` · `frustration` · `intent`— para poder afirmar que el triage clasifica bien: sigue siendo el sistema juzgándose a sí mismo.
- [ ] **Fijar la unidad de "consulta".** Bloque (`message_batches`), generación o intención detectada dan números distintos, y F11 admite varias intenciones por bloque. El bloque es la unidad que `03-capa-prompts.md` ya llama "turno".
- [ ] **Definir "sin escalada".** ¿Cualquier transferencia, o sólo la de tipo `out_of_domain`? Si un cliente pregunta algo del dominio y escala por *frustración*, literalmente no fue *"respondida sin escalada"* — pero OE6 mide cobertura del RAG, no paciencia del cliente. Y como la escalada mueve **la conversación entera**, hay que decidir además qué pasa con las consultas posteriores del mismo chat.

> **Ojo:** la cadena 5 de [`DER/relaciones.md` §11](../DER/relaciones.md) ya está redactada como *"contra la ausencia de transferencia por `signal_type = 'out_of_domain'`"*. **Eso presupone la tercera decisión sin haberla tomado.** Se deja marcado para no leerlo después como si estuviera resuelto.

### Diferidas al Paso 11 — atribución por operador (H-25)

Ningún dato falta: las cuatro reglas son calculables hoy. Falta elegir **una** y sostenerla en todas las métricas de F35 y F16.

| Regla | Qué mide | Defecto |
|---|---|---|
| `conversations.resolved_by_operator_id` | Quién cerró | Como el cierre es manual ([ADR-0002](0002-ciclo-de-vida-conversacion-y-recordatorios.md)), **acredita al que sólo apretó "resolver"** en un chat que atendió la IA |
| `conversations.operator_id` | Último asignado | Se sobrescribe; F26 lo vuelve arbitrario, y en modo autónomo es nulo |
| `conversation_assignments` | Todos los que intervinieron | Los porcentajes suman más de 100 % |
| `messages.sender_operator_id` | **Quién efectivamente respondió** | El más fiel al trabajo hecho, y el más caro de calcular. No estaba en la lista original |

- [ ] **Elegir la regla de atribución** y aplicarla al denominador de la tasa de conversión. **El numerador ya está fijo** (`sales.operator_id`): si el denominador usa otra regla, el cociente mezcla dos atribuciones distintas y no significa nada.
- [ ] **Definir "rendimiento por operador"** (F16). ¿Conversaciones resueltas? ¿Tasa de conversión? ¿Promedio de `conversations.quality_score` (F13)? El requisito pide una comparativa y no dice de qué.

> **Lo que no es una decisión:** `sales.amount` se agrupa **siempre por `currency`** (C08). No es una elección, es la convención ya fijada — sumar ARS con USD invalidaría el monto atribuido.

### Heredadas de la sección 8.4 del anteproyecto

- [ ] **Frontend.** Sigue "A DEFINIR" en 8.4. Debe consumir WebSockets para el inbox en tiempo real (F21).
- [ ] **Proveedor de LLM primario y de respaldo.** Conviene fijarlo antes de medir OE2: la consistencia de tono depende del modelo.
- [ ] **Modelo de embeddings.** Condiciona la dimensionalidad de las colecciones del vector store; cambiarlo después obliga a reindexar toda la base de conocimiento.
