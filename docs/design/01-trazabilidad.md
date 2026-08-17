# SONIA — Matriz de trazabilidad requisito → dato

| Campo | Valor |
|---|---|
| **Documento** | `docs/design/01-trazabilidad.md` |
| **Estado** | Vigente |
| **Fecha** | 2026-08-06 · ampliada 2026-08-10 con los hallazgos del recorrido del Paso 6 (§5.3) |
| **Etapa** | Diseño — Paso 2 (Fundaciones) |
| **Base documental** | [SONIA_Anteproyecto.pdf](../SONIA_Anteproyecto.pdf) — Anexo A (F01–F39), Anexo B (roles), Anexo C (modelo de datos), sección 7.2 (OE1–OE7) |
| **Depende de** | [00-convenciones.md](00-convenciones.md) |

---

## 0. Qué es este documento y cómo se usa

Una fila por cada requisito funcional (F01–F39) y por cada objetivo específico (OE1–OE7): **46 filas**. Para cada uno se responde una sola pregunta:

> ¿Qué entidad y qué atributos del modelo de datos lo sostienen?

**Por qué esto va antes del modelo.** Es el instrumento más barato de toda la etapa de diseño y el que produce, de forma sistemática, la lista de entidades faltantes. La alternativa —mirar el modelo y tratar de notar qué falta— depende de que alguien se dé cuenta. La matriz hace que los huecos salgan solos: si al recorrer un requisito no hay dónde escribir la entidad, el hueco ya está encontrado.

**Contra qué se compara.** El único modelo de datos que existe hoy es el **Anexo C** del anteproyecto, con nueve entidades: `agents`, `conversations`, `messages`, `contacts`, `labels`, `sales`, `reminders`, `knowledge_base` y `quick_replies`. Los nombres se citan según las convenciones ya fijadas (`ai_agents`, `operators`, `knowledge_bases` — ver [00-convenciones.md](00-convenciones.md) C01 y C03).

**Regla de cierre.** Ninguna fila puede quedar sin veredicto. Y cuando el veredicto es negativo hay exactamente dos lecturas posibles, **las dos valiosas**:

1. **Falta modelar algo** → se agrega al modelo de datos en el Paso 5.
2. **El requisito no es implementable tal como está escrito** → es un **hallazgo**, no un error del equipo, y se resuelve como decisión en el Paso 3. Un tribunal valora mucho más "detectamos que F24 no era representable y lo corregimos" que un diagrama que finge que no había problema.

### Vocabulario de estado

| Símbolo | Significa |
|---|---|
| ✅ **Cubierto** | El Anexo C tiene la entidad y los atributos necesarios |
| ⚠️ **Incompleto** | La entidad existe, pero le faltan atributos o el requisito sólo se cumple parcialmente |
| ❌ **Sin soporte** | No hay ninguna entidad que lo sostenga |
| 📄 **Documental** | Restricción declarada sin implementación (F29, F39) |

> **Sobre la columna "diagrama".** El plan de diseño preveía una columna que vinculara cada requisito con el diagrama que lo representa. Se omite deliberadamente en esta versión: los diagramas del modelo nuevo todavía no existen, y una columna con 46 celdas que dicen "pendiente" es ruido, no información. Se agrega en el Paso 14, cuando haya diagramas reales que citar — y ahí se convierte en el segundo control de cobertura.

---

## 1. Módulo 1 — Motor de composición y reescritura de prompts

| ID | Requisito | Entidades | Atributos que exige | Estado |
|---|---|---|---|---|
| **F01** | Configuración modular del agente | `ai_agents` | Los seis componentes (objetivo, personalidad, comportamiento, tono, voz de marca, reglas) + umbral de transferencia | ⚠️ La entidad existe, pero los siete valores están comprimidos en `components_json`. Ver **H-01** y **H-04** |
| **F02** | Tres modos de operación | `ai_agents`, `conversations` | `default_mode` (por agente) + `mode` (por conversación) | ✅ El Anexo C separa correctamente el modo por defecto del modo efectivo |
| **F03** | Previsualización del prompt | `ai_agents` | Lectura de cada componente por separado, en tiempo real | ⚠️ Con un JSON hay que parsear en cada render y no se puede mostrar el aporte de cada componente. Ver **H-01** |
| **F04** | **Reescritura adaptativa** | — | Objeciones, intención detectada y datos del cliente, **actualizados por turno** | ❌ **No existe ninguna entidad** donde vivan. `messages.prompt_snapshot_json` guarda lo que se envió, no el estado del contexto. Ver **H-02** |
| **F05** | Visualización del razonamiento | `messages` | `reasoning_json` apuntando a **qué componentes** influyeron | ⚠️ El campo existe, pero no puede referenciar componentes que no son filas. Ver **H-01** |
| **F06** | Pipeline RAG | `knowledge_bases` | Documentos (plural), hasta 100 MB por agente, y fragmentos recuperables | ⚠️ La entidad modela **un archivo** (`filename`, `size_bytes`); `embeddings_count` es un entero suelto que no permite referenciar fragmentos. Ver **H-06** |
| **F07** | Acceso a respuestas rápidas (por la IA) | `quick_replies` | Lectura del catálogo | ✅ |
| **F08** | Visualización de plantillas Meta | — | Plantilla, estado, motivo de rechazo, sugerencia del LLM | ❌ **No existe entidad de plantilla.** Ver **H-05** |
| **F09** | Respuestas rápidas configurables | `quick_replies`, `operators` | `title`, `content`, autor | ⚠️ La entidad existe; `created_by_admin_id` apunta a una entidad de usuario que el Anexo C nunca declara. Ver **H-00** |

---

## 2. Módulo 2 — Agregador inteligente y clasificador de transferencia

| ID | Requisito | Entidades | Atributos que exige | Estado |
|---|---|---|---|---|
| **F10** | Timer adaptativo | — | El bloque agregado, con el timer usado y el motivo de cierre | ❌ No hay entidad que represente el bloque. Sin persistirlo, "adaptativo" es una afirmación sin evidencia. Ver **H-03** |
| **F11** | Chunking dinámico por intenciones | — | Intenciones detectadas dentro del bloque | ❌ Sin entidad de bloque ni de intención. Ver **H-03** |
| **F12** | **Clasificador de transferencia** | — | El evento de transferencia y **el tipo de señal** que lo disparó | ❌ `conversations.operator_id` dice quién atiende ahora, no por qué ni cuándo se transfirió. Ver **H-04** |
| **F13** | Puntaje de conversaciones | `conversations` | `score`, y quién calificó y cuándo | ⚠️ `score` existe; faltan autor y fecha de la calificación. Hueco menor, no bloqueante |
| **F14** | Registro de ventas | `sales` | `amount`, operador, agente, canal de origen | ⚠️ Completo salvo el tipo de `amount` y la moneda, ya resueltos en la convención C08 |
| **F15** | Trazabilidad de origen | `contacts` | Canal de ingreso **del contacto** | ⚠️ El requisito dice "de cada **contacto**"; el modelo pone `source_channel` en `sales`. Un contacto que nunca compra queda sin origen. Ver **H-13** |
| **F16** | Gráficas y métricas de vendedores | `conversations`, `sales`, `operators` | Volumen en el tiempo, rendimiento por operador | ⚠️ Depende de la entidad de operador (**H-00**) y de marcas de tiempo de resolución (**H-12**) |

---

## 3. Módulo 3 — Plataforma operativa

| ID | Requisito | Entidades | Atributos que exige | Estado |
|---|---|---|---|---|
| **F17** | Gestión de agentes IA | `ai_agents` | ABM completo | ✅ |
| **F18** | **Gestión de usuarios y roles** | — | Usuario, rol (`operator`/`supervisor`/`admin`) | ❌ **El Anexo C no declara ninguna entidad de usuario**, pese a que cuatro entidades la referencian por FK. Ver **H-00** |
| **F19** | Gestión de etiquetas | `labels` | Catálogo con nombre y color | ✅ |
| **F20** | Gestión de webhooks | — | Endpoint, credenciales de canal, firma, reintentos, **log de eventos** | ❌ No hay entidad de configuración de canal ni de evento recibido. Ver **H-08** |
| **F21** | Backend / WebSockets | `messages`, `conversations` | Distribución en tiempo real | ✅ a nivel de datos — es transporte. Pero la idempotencia ante reintentos exige el identificador de mensaje de Meta, que no existe. Ver **H-08** |
| **F22** | Vista centralizada de chats | `conversations`, `messages` | Último mensaje, **tiempo de espera**, agente asignado, estado del bot | ⚠️ Faltan marcas de tiempo operativas; derivarlas de `messages` en cada render de bandeja es inviable. Ver **H-12** |
| **F23** | Filtros de bandeja | `conversations`, `labels`, `reminders` | No leídas · asignadas · en espera · resueltas · por etiqueta · con recordatorio · destacadas | ⚠️ Cuatro filtros salen (`operator_id`, `status`, `reminders`, `starred`); **"no leídas"** no tiene atributo (**H-12**) y **"por etiqueta"** no tiene relación (**H-07**) |
| **F24** | **Etiquetas por conversación** | — | Vínculo conversación ↔ etiqueta | ❌ `conversations` no tiene ningún vínculo con `labels`. **No es representable hoy.** Ver **H-07** |
| **F25** | Destacar con estrella | `conversations` | `starred` | ✅ |
| **F26** | Transferencia entre operadores | `conversations` | Historial de reasignaciones + **notas internas** | ⚠️ `operator_id` se sobrescribe: no queda historial. Y "notas internas" no tiene entidad (`contacts.notes` es del contacto). Ver **H-15** |
| **F27** | Control de modo por conversación | `conversations` | `mode` | ✅ |
| **F28** | Recordatorios | `reminders` | Alerta vinculada **a un contacto**, con fecha | ⚠️ El requisito los vincula a contactos; el modelo, a conversaciones. Ver **H-14** |
| **F29** | Restricción: sin borrado de mensajes | `messages` | — | 📄 Documental. Su consecuencia de modelo ya está resuelta en la convención C09 |
| **F30** | ABM de contactos | `contacts` | Nombre, apellido, teléfono, email, etiquetas, notas, tipo | ⚠️ Todos los campos existen; las etiquetas están como `tags_json`. Ver **H-16** |
| **F31** | Extracción automática de datos | `contacts`, `ai_agents` | **Toggle por agente** + destino de los datos extraídos | ⚠️ Los tres campos cifrados existen, pero no hay toggle y sólo DNI/CUIL/CBU tienen dónde ir. Ver **H-17** |
| **F32** | Historial unificado por contacto | `contacts`, `conversations`, `sales`, `reminders` | Conversaciones, operadores, calificaciones, ventas y recordatorios | ⚠️ Se arma por joins, salvo los recordatorios, que cuelgan de conversación. Ver **H-14** |
| **F33** | Segmentación por etiquetas | `contacts`, `labels` | Filtrado de contactos por etiqueta del catálogo | ⚠️ Funciona como JSON, pero sin integridad referencial contra `labels` y sin join limpio. Ver **H-16** |
| **F34** | Métricas de volumen | `conversations`, `contacts` | Estados · **tiempos de respuesta** · distribución por **canal de origen** | ⚠️ Los estados salen de `status`; los tiempos de respuesta (**H-12**) y el canal de origen (**H-13**) no |
| **F35** | Métricas por vendedor | `conversations`, `sales`, `operators` | Resueltas, ventas, monto atribuido, tasa de conversión | ⚠️ Derivable de `operator_id` y `sales`, condicionado a la entidad de operador (**H-00**) |
| **F36** | **Métricas del agente IA** | — | Tasa de resolución autónoma · tasa de transferencia **por tipo de señal** · consistencia de tono | ❌ Las tres partes sin soporte: el emisor no distingue IA de humano (**H-09**), no hay evento de transferencia (**H-04**), no hay dónde guardar el juicio de tono (**H-11**) |
| **F37** | Panel de plantillas Meta | — | Estado de plantillas + sugerencia del LLM | ❌ Mismo hueco que F08. Ver **H-05** |
| **F38** | Gráficas temporales | `conversations` | Volumen en el tiempo, distribución horaria/semanal, evolución del agente | ⚠️ El volumen sale de `created_at`; la evolución de métricas del agente depende de F36 |
| **F39** | Restricción: sin campañas Meta | — | — | 📄 Documental |

---

## 4. Objetivos específicos

Acá el criterio es más exigente que en los requisitos: un objetivo no está cubierto si su **métrica** no se puede calcular.

| ID | Objetivo y métrica | Entidades | Estado |
|---|---|---|---|
| **OE1** | Motor de composición (F01–F05): 100 % de los prompts con todos los componentes **activos**, previsualización en tiempo real | `ai_agents` | ⚠️ "Componente activo" presupone un estado por componente que un JSON monolítico no tiene. Depende de **H-01** |
| **OE2** | Reescritura adaptativa (F04) + consistencia de tono **≥ 80 %** por LLM-as-judge | — | ❌ Dos huecos: la capa de contexto dinámico (**H-02**) y la entidad donde vive el juicio de tono (**H-11**). Además, sin versionado de componentes no se puede decir **contra qué configuración** se midió |
| **OE3** | Agregador (F10–F11) + clasificador de transferencia: precisión **≥ 85 %** sobre 200 conversaciones | — | ❌ Faltan el bloque (**H-03**), el evento de transferencia (**H-04**) y —sobre todo— **el ground truth**: no hay dónde guardar la etiqueta correcta contra la cual medir precisión (**H-10**) |
| **OE4** | Clasificador de frustración e intención: precisión **≥ 85 %** sobre 500 conversaciones **etiquetadas** | — | ❌ Mismo ground truth, ampliado a 500. Ojo: el etiquetado de evaluación **no es** `labels`, que es el catálogo operativo del negocio. Ver **H-10** |
| **OE5** | Plataforma operativa (F17–F39) con cobertura de pruebas **≥ 80 %** | — | ⚠️ No exige entidades propias: hereda los huecos de F17–F39. La cobertura es una métrica de proceso, no de modelo |
| **OE6** | RAG: **90 %** de las consultas del dominio resueltas sin escalada | `knowledge_bases` | ❌ Depende de **H-06**. Y "consulta dentro del dominio" exige registrar qué fragmentos se recuperaron y con qué score — sin eso no se puede distinguir "falló el RAG" de "falló el LLM" |
| **OE7** | Producción: resolución autónoma **≥ 75 %** y consistencia de tono **≥ 85 %** | — | ❌ "Autónoma" exige saber si respondió la IA o un humano (**H-09**); la consistencia de tono exige **H-11** |

---

## 5. Hallazgos

Dieciocho, ordenados por impacto. Cada uno nace de comparar el texto de un requisito con el Anexo C — ninguno es una opinión sobre el diseño.

### 5.1. Faltan entidades o atributos → se resuelven modelando (Paso 5)

| # | Hallazgo | Afecta a |
|---|---|---|
| **H-00** | **No existe entidad de usuario/operador.** El Anexo C define nueve entidades y ninguna es la persona que atiende — pese a que `conversations.operator_id`, `sales.operator_id`, `reminders.operator_id` y `quick_replies.created_by_admin_id` la referencian, y a que el Anexo B define tres roles sobre ella. Es el hueco más elemental del modelo | F09, F13, F14, F16, F18, F19, F26, F28, F35 |
| **H-01** | **Los componentes del prompt viven en un JSON.** `agents.components_json` impide previsualizar por componente (F03), señalar cuál influyó (F05) y saber cuáles están activos (OE1) | F01, F03, F05, OE1, OE2 |
| **H-02** | **No existe la capa de contexto dinámico.** F04 exige actualizar objeciones, intención y datos detectados **en cada turno**, y ninguna entidad los aloja. Es el aporte original declarado en la sección 6.3, y es la parte del modelo que no está modelada | F04, OE2 |
| **H-03** | **No existe el bloque agregado ni la intención detectada.** Sin persistirlos, F10 y F11 no son medibles y OE3 no es defendible | F10, F11, OE3 |
| **H-04** | **No existe el evento de transferencia.** El modelo sabe quién atiende, no por qué ni cuándo cambió. Bloquea directamente la métrica "tasa de transferencia **por tipo de señal**" de F36 | F12, F36, OE3 |
| **H-05** | **No existe entidad de plantilla de Meta.** Dos requisitos completos sin ninguna entidad detrás | F08, F37 |
| **H-07** | **No hay vínculo conversación ↔ etiqueta.** F24 no es representable, y arrastra el filtro por etiqueta de F23 | F23, F24 |
| **H-08** | **No hay configuración de canal ni log de eventos de webhook.** F20 pide "reintentos y log de eventos" textualmente. Faltan también las credenciales del canal y el identificador de mensaje de Meta, sin el cual no hay idempotencia ante reintentos | F20, F21 |
| **H-09** | **El emisor del mensaje no distingue IA de humano.** `messages.direction` sólo dice entrante/saliente. Sin esa distinción no existe la "tasa de resolución autónoma" — el objetivo central de OE7 | F36, OE7 |
| **H-10** | **No hay ground truth de evaluación.** OE3 y OE4 miden precisión contra 200 y 500 conversaciones etiquetadas, y no hay entidad donde viva esa etiqueta correcta. Si se etiqueta en una planilla suelta, la métrica no es reproducible | OE3, OE4 |
| **H-11** | **No hay dónde guardar la evaluación de tono.** La consistencia de tono por LLM-as-judge aparece en tres lugares del anteproyecto y no tiene entidad en ninguno | F36, OE2, OE7 |
| **H-12** | **Faltan marcas de tiempo operativas en `conversations`.** "Tiempo de espera" (F22), "no leídas" (F23) y "tiempos de respuesta" (F34) no son derivables sin recorrer `messages` en cada render de bandeja | F22, F23, F34 |
| **H-15** | **Sin historial de reasignaciones ni notas internas.** F26 pide preservar ambos; `operator_id` se sobrescribe y las notas internas no existen | F26 |
| **H-17** | **Sin toggle de extracción ni destino para datos arbitrarios.** F31 dice "toggle por agente" textualmente, y "extrae datos del usuario" es abierto: sólo DNI, CUIL y CBU tienen dónde ir | F31 |

### 5.2. El requisito y el modelo se contradicen → requieren decisión (Paso 3)

Estos cuatro no se resuelven modelando: hay que elegir cuál de las dos versiones es la correcta y dejarlo escrito.

| # | Contradicción | Decisión pendiente |
|---|---|---|
| **H-06** | F06 habla de "documentos" en plural, hasta 100 MB por agente; `knowledge_base` modela **un solo archivo** | ¿Una base con muchos documentos, o una base por archivo? |
| **H-13** | F15 registra el canal de ingreso **del contacto**; el modelo lo guarda en `sales` | ¿`contacts` como fuente de verdad? Si `sales.source_channel` se mantiene, declararlo como *snapshot* del momento de la venta |
| **H-14** | F28 vincula los recordatorios **a contactos**; el modelo los vincula a conversaciones — y F32 exige llegar a ellos desde el contacto | ¿`contact_id` obligatorio con `conversation_id` opcional? |
| **H-16** | `contacts.tags_json` convive con el catálogo `labels` que administra el admin (F19): si renombra o borra una etiqueta, los JSON quedan huérfanos | ¿JSON o relación? |

### 5.3. Hallazgos del recorrido del Paso 6 → contra el modelo nuevo

Los dieciocho anteriores nacen de comparar el texto de un requisito con el **Anexo C**. Estos nacen de otra cosa: de **intentar escribir la consulta SQL de cada métrica** contra el catálogo ya construido en el Paso 5. Es el control que anticipa el punto 4 de §7, y encuentra dos clases de hueco que la matriz no podía ver:

- **Tipo `dato`** — la métrica está bien definida y el modelo no permite calcularla. **Se resuelve modelando ahora**: descubrirlo con datos productivos encima es irreparable, porque el dato nunca se guardó.
- **Tipo `definición`** — el anteproyecto no dice qué se mide, o dice varias cosas distintas. **Se registra y se difiere**: modelar antes de decidir es dejar que el modelo decida en silencio.

| # | Hallazgo | Tipo | Afecta a | Estado |
|---|---|---|---|---|
| **H-18** | **La salida del clasificador de frustración no se persiste.** `transfer_events` sólo tiene fila cuando la transferencia se dispara. Para el clasificador de transferencia alcanza, porque su salida positiva *es* la acción; para el detector de frustración no, porque su salida es una percepción que sólo deja rastro si además cruzó el umbral. **El conjunto que OE4 mide es un superconjunto del que la base guarda**, y medir precisión sobre lo guardado da un número que sube al subir el umbral | dato | OE3, OE4, F36 | ✅ **Resuelto** — entidad `signal_evaluations` ([ADR-0014](02-decisiones/0014-medicion-de-oe3-y-oe4.md)) |
| **H-19** | **`message_batches` no tiene `turn_number`**, pese a que [03-capa-prompts.md](03-capa-prompts.md) define *"turno = un `message_batch` cerrado y procesado"*. Hoy `conversation_contexts` y `transfer_events` llevan cada uno el suyo por su cuenta, sin un origen común. Lo natural sería que el turno viva en el bloque y los demás lo deriven | dato | F10, F11, OE3 | ⚠️ Abierto — ver `02-decisiones/README.md` |
| **H-20** | **`prompt_component_versions` no versiona `active`.** OE1 compara lo que *se debía* ensamblar contra lo que *se ensambló*, y el único registro de ambos lados era `generation_components`: la consulta se compara consigo misma y **sólo puede dar 100 %**, aunque el ensamblador falle. La Regla 2 de `03-capa-prompts.md` lo había considerado y concluido mal — equiparó *haber participado* con *haber estado activo*, que es lo que OE1 verifica | dato | OE1, F01 | ✅ **Resuelto** — `prompt_component_versions.active` + enmienda a la Regla 2 ([ADR-0015](02-decisiones/0015-medicion-de-oe1.md)) |
| **H-21** | **`generations` no guarda de qué agente es.** `conversations.ai_agent_id` es mutable (F27) y derivarlo de `generation_components` es circular. La entidad ya congelaba `llm_provider`, `llm_model` y `temperature` —que salen del agente— y no congelaba de quién eran | dato | OE1, OE2 | ✅ **Resuelto** — `generations.ai_agent_id` ([ADR-0015](02-decisiones/0015-medicion-de-oe1.md)) |
| **H-22** | **La consistencia de tono no tiene una definición única en el anteproyecto.** Tres formulaciones: 5.3 (*evaluación humana*, >85 % de las interacciones), OE2 (*LLM-as-a-judge*, ≥80 %) y OE7 (≥85 %, sin decir quién juzga); 8.1 y F36 dicen LLM-as-judge. Además `tone_evaluations` **no tiene dónde alojar a un evaluador humano** y `tone_score` no declara escala ni punto de corte. El instrumento —humano, LLM-as-judge o clasificador entrenado— se decide recién en la etapa de evaluación | definición | OE2, OE7, F36 | ⏸️ **Diferido al Paso 11** |
| **H-23** | **La configuración de recuperación del RAG no se congela con la evidencia.** `retrieval_logs` guarda qué fragmentos volvieron, con su `similarity_score` y su `rank`, pero **ni `top_k` ni el umbral de similitud existen en ningún lado** — ni en `knowledge_bases`, ni en `ai_agents`, ni en `generations`. Sin ellos no se distingue *"volvió un solo fragmento porque sólo uno superó el corte"* de *"volvió uno porque se pidió uno"*, y mover cualquiera de los dos parámetros durante la Fase 4 vuelve incomparables las mediciones anteriores **sin dejar rastro**. Es la misma asimetría que H-21: `generations` congela los parámetros del LLM y no los del RAG, habiendo gobernado los dos el mismo turno | dato | OE6 | ✅ **Resuelto** — `pipeline_steps`, con los parámetros en el `detail_json` del paso `retrieval` ([ADR-0016](02-decisiones/0016-rastro-del-pipeline-de-respuesta.md)) |
| **H-24** | **OE6 no define ninguno de sus tres términos.** (a) *"Consulta dentro del dominio"* no está definida, y la definición obvia —"el RAG recuperó algo con score alto"— es **circular**: deja que el sistema decida qué preguntas le corresponde saber y después se evalúa sobre esas, de modo que **mejora su métrica justo cuando falla en recuperar**. (b) La unidad de *"consulta"* no está fijada: bloque, generación o intención detectada dan números distintos con F11. (c) *"Sin escalada"* no dice si cuenta cualquier transferencia o sólo la de tipo `out_of_domain` — y como la escalada mueve la conversación entera, hay además un choque de granularidad con el denominador, que es por turno | definición | OE6 | ⏸️ **Diferido al Paso 11.** (a) tiene salida: el veredicto de `triage`, que `pipeline_steps` ya registra, se emite **antes** de recuperar y rompe la circularidad — ADR-0016 |
| **H-25** | **La atribución por operador admite cuatro reglas incompatibles.** F35 pide *"conversaciones resueltas, ventas generadas, monto atribuido y tasa de conversión por operador"*. Las ventas son inequívocas (`sales.operator_id`), pero *"conversaciones resueltas por un operador"* puede leerse de cuatro formas —quién cerró, último asignado, quien haya intervenido, o quien efectivamente respondió—, **y la tasa de conversión mezcla dos reglas distintas en un mismo cociente** si el numerador usa `sales.operator_id` y el denominador otra cosa. F16 agrega *"rendimiento por operador"* sin definir rendimiento. **Ningún dato falta:** las cuatro reglas son calculables hoy | definición | F35, F16, F13 | ⏸️ **Diferido al Paso 11** |

> **Lo que revela el patrón.** Ninguno de estos hallazgos era detectable con la matriz: la fila de OE4 decía *"faltan el bloque, el evento de transferencia y el ground truth"*, y las tres cosas se agregaron en el Paso 5. La matriz encuentra **entidades faltantes**; sólo escribir la consulta encuentra **combinaciones imposibles**. Es la misma lección que ya había dejado el hueco de los campos de medios en `messages` (§12 de `DER/entidades.md`), ahora en su versión más cara.
>
> **Los cuatro de tipo `dato` fallaron por el mismo motivo:** el modelo guardaba **el resultado de una decisión y no la evidencia sobre la que se tomó** — la transferencia sin las señales evaluadas (H-18), el prompt ensamblado sin la configuración que lo debía producir (H-20, H-21), los fragmentos recuperados sin los parámetros con que se los pidió (H-23). En todos los casos la consecuencia es la misma: una métrica que sólo puede devolver el número deseado, o que deja de ser comparable en silencio.
>
> **Y los tres de tipo `definición` comparten otro:** los tres aparecen donde el anteproyecto **nombra una métrica sin definir su denominador** — qué cuenta como interacción (H-22), como consulta del dominio (H-24), como conversación de un operador (H-25). Es el lugar donde conviene mirar primero al revisar cualquier objetivo que falte.

---

## 6. Resumen

| Estado | Requisitos (F01–F39) | Objetivos (OE1–OE7) |
|---|---:|---:|
| ✅ Cubierto | 7 | 0 |
| ⚠️ Incompleto | 20 | 2 |
| ❌ Sin soporte | 10 | 5 |
| 📄 Documental | 2 | — |
| **Total** | **39** | **7** |

Dos lecturas que conviene tener presentes:

**Ningún objetivo específico es medible hoy.** Cinco de siete no tienen soporte y los otros dos están incompletos. No es un juicio sobre el trabajo hecho: el anteproyecto declara las métricas correctamente, y el Anexo C se presenta a sí mismo como "esquema simplificado". Pero significa que **el modelo de datos completo es precondición de la fase de medición**, no una tarea paralela.

**El patrón de los huecos no es aleatorio.** Lo que está cubierto es la operación (contactos, conversaciones, etiquetas, ventas, respuestas rápidas: el territorio que Chatwoot ya resolvió). Lo que falta es, casi exactamente, el aporte original —composición y reescritura de prompts, clasificación de transferencia, agregación de mensajes— más toda la capa de evaluación. Dicho de otro modo: **está modelado lo que SONIA comparte con el estado del arte, y no está modelado lo que la diferencia de él.**

---

## 7. Qué se hace con esto

1. **Los cuatro de §5.2 van al Paso 3** como decisiones: hay que elegir entre dos versiones que hoy se contradicen. No se pueden modelar sin decidir primero.
2. **Los catorce de §5.1 van al Paso 5** como entidades y atributos nuevos, distribuidos en los ocho bloques del modelo de datos lógico.
3. **H-01, H-02 y H-11 se diseñan aparte, en el Paso 4** (`03-capa-prompts.md`): son el núcleo conceptual de la tesina y merecen tratamiento propio, no un rincón del DER.
4. **Esta matriz se vuelve a recorrer en el Paso 6**, ya contra el modelo nuevo. En esa pasada ninguna fila puede quedar en ❌, y cada métrica de OE1–OE7 tiene que poder escribirse como consulta SQL.

> **Estado del recorrido del Paso 6 al 2026-08-10.** Hecho: **OE1, OE3 y OE4**, que produjeron H-18 a H-21 (§5.3), ADR-0014 y ADR-0015. Pendientes: OE2, OE5, OE6, OE7 y F34–F38. Los estados ❌ y ⚠️ de las tablas de §2 a §4 **no se corrigen retroactivamente**: describen el Anexo C, que es contra lo que se hizo esta matriz. La cobertura del modelo nuevo se registra en esta sección y en el entregable del Paso 6.

Los hallazgos de §5.2 —las contradicciones internas del propio anteproyecto— merecen un párrafo en el informe final. Detectarlas y resolverlas es trabajo de ingeniería que suma; que aparezcan sin resolver en la defensa, no.
