# SONIA — Capa de prompts (Paso 4)

| Campo | Valor |
|---|---|
| **Documento** | `docs/design/03-capa-prompts.md` |
| **Estado** | Vigente |
| **Fecha** | 2026-08-08 · Regla 2 enmendada 2026-08-10 (Paso 6 — ADR-0015) |
| **Etapa** | Diseño — Paso 4 (Etapa B) |
| **Base documental** | [SONIA_Anteproyecto.pdf](../SONIA_Anteproyecto.pdf) — Anexo A (F01, F03, F04, F05, F10, F11, F31), sección 6.3, 7.2 (OE1, OE2) · [00-convenciones.md](00-convenciones.md) · [01-trazabilidad.md](01-trazabilidad.md) — H-01, H-02 · [02-decisiones/0006-anonimizacion-de-pii.md](02-decisiones/0006-anonimizacion-de-pii.md) |
| **Complemento** | [DER/entidades.md](DER/entidades.md) y [DER/relaciones.md](DER/relaciones.md) — las tablas de esta capa ya están catalogadas ahí; este documento no las repite, define las **reglas** que las tablas por sí solas no dicen |

---

## 0. Qué resuelve este documento

El catálogo del DER ya contiene las tres capas de tablas que este paso necesitaba: `prompt_components` + `prompt_component_versions` + `transfer_policies` (Bloque 2), y `message_batches` + `detected_intents` + `conversation_contexts` + `conversation_context_versions` + `generations` + `generation_components` (Bloque 3). Esa parte del Paso 4 se adelantó al construir el DER y no se repite acá.

Lo que faltaba — y es lo que fija este documento — son las **reglas de comportamiento** que el plan de diseño pide explícitamente: en qué orden se ensambla el prompt, cuándo se versiona un componente, qué hace el sistema si falla la reescritura del contexto, en qué punto exacto se redacta el dato personal, y qué se recorta si el prompt no entra en la ventana del modelo. Ninguna tabla responde estas preguntas por sí sola.

**Nota de terminología.** F04 habla de actualizar el contexto "en cada turno". En este documento, **turno = un `message_batch` cerrado y procesado** (F10/F11) — no un mensaje individual. En el caso más común (un mensaje, pausa, respuesta) son la misma cosa; difieren sólo cuando el cliente escribe varios mensajes fragmentados que el agregador junta en un solo bloque antes de procesarlos.

---

## 1. Las tres capas (resumen — ver el DER para el detalle)

| Capa | Dueño | Mutabilidad | Tablas |
|---|---|---|---|
| **1 — Diseño** | El agente IA | Versionada, cambia por edición del administrador | `prompt_components`, `prompt_component_versions` |
| **2 — Ejecución** | La conversación | Versionada, cambia por bloque procesado | `conversation_contexts`, `conversation_context_versions` |
| **3 — Auditoría** | El bloque/turno | Inmutable | `generations`, `generation_components`, `retrieval_logs` |

El porqué de esta separación por dueño ya está justificado en [DER/entidades.md §12](DER/entidades.md#12-por-qué-estas-entidades) y no se repite acá.

---

## 2. Las cinco reglas

### Regla 1 — Composición del prompt

**Decisión.** El prompt no es un texto plano: usa la estructura de roles de la API del LLM.

- **`system`** contiene **únicamente** los seis `prompt_components` activos del agente, concatenados según su `position`. No cambia entre bloques de la misma conversación salvo que el administrador edite un componente.
- **`user`** contiene, en este orden: **(1)** el contexto dinámico del bloque (`conversation_contexts`: `primary_intent`, objeciones, urgencia, señal de presupuesto, datos extraídos), **(2)** los fragmentos recuperados por RAG (`retrieval_logs`), **(3)** el mensaje del cliente ya redactado (Regla 4).
- **El historial** de bloques anteriores viaja como turnos reales `user`/`assistant` entre `system` y el `user` del bloque actual — no se aplana dentro de un solo mensaje.

**Justificación.**

- Mantener `system` estático y limitado a los seis componentes lo vuelve **cacheable** entre bloques (Anthropic, OpenAI y otros proveedores ofrecen *prompt caching* sobre el contenido que no cambia). Si el contexto dinámico viviera en `system`, se perdería ese ahorro en el 100% de los bloques, porque nunca habría dos `system` iguales. El costo y la latencia por bloque que ya registra `generations.cost` / `latency_ms` son insumo directo de la viabilidad económica ante el cooperante — este orden los protege.
- F04 usa el verbo "inyecta" para describir el contexto dinámico, y no impone en qué rol de la API ocurre esa inyección: no hay conflicto entre esta decisión y el texto del anteproyecto.
- El orden dentro de `user` — contexto, luego RAG, luego la pregunta — sigue el razonamiento con que un operador humano encararía el mismo caso: primero "qué sé de este cliente ahora", después "qué dice mi base de conocimiento", y al final la pregunta literal.

**Costo asumido.** El `system` deja de reflejar el estado de la conversación; cualquier lectura aislada del `system` sin su `user` correspondiente da una imagen incompleta del agente en ese momento. Es aceptable porque `generations` siempre guarda ambos juntos.

---

### Regla 2 — Política de versionado de `prompt_component_versions`

> **Enmendada el 2026-08-10** por el recorrido del Paso 6 ([ADR-0015](02-decisiones/0015-medicion-de-oe1.md), H-20). La redacción anterior versionaba **sólo por `content`**; el texto de abajo es el vigente.

**Decisión.** Se crea una versión nueva si cambió el **`content`** o el **`active`** respecto de la versión vigente. Un guardado sin cambios no inserta fila. El `position` **no** dispara versión.

**Justificación.** OE2 exige poder decir contra qué configuración se midió cada resultado, y OE1 exige poder decir **qué componentes debían estar** en cada prompt. Versionar en cada guardado —incluso sin cambios— llenaría el histórico de filas idénticas y obligaría a filtrar duplicados después, exactamente el ruido que la trazabilidad necesita evitar. Pero un cambio de `active` **no** es una fila idéntica: es el cambio más significativo que puede sufrir un componente.

**Por qué cambió esta regla.** La versión original sostenía que `active` y `position` quedaban congelados a través de `generation_components`, *"que sólo enlaza componentes que participaron"*. Para `position` es cierto —`generation_components.position` guarda el orden real de ensamblado— y para `content` también. Para `active` no: **equipara haber participado con haber estado activo, que es precisamente lo que OE1 verifica.** Con esa equivalencia, la consulta de OE1 compara `generation_components` contra sí mismo y devuelve 100 % aunque el ensamblador falle. Una métrica que no puede dar otro resultado no mide nada.

`position` sigue sin versionarse, y es deliberado: ninguna métrica del anteproyecto mide el orden de ensamblado.

---

### Regla 3 — Ciclo de vida de `conversation_contexts`

**Decisión.**

- **Creación:** al procesarse el primer bloque de la conversación. Antes de eso no existe fila (`conversations 1 — 0..1 conversation_contexts`, ya fijado en [relaciones.md:62](DER/relaciones.md#L62)).
- **Reescritura:** una vez por bloque cerrado y procesado (no por mensaje individual).
- **Ante falla de la reescritura** (el modelo que actualiza el contexto no responde, da error o hace timeout): se reintenta un número acotado de veces; si sigue fallando, la generación de ese bloque se produce igual, usando la última versión de contexto disponible. No se bloquea al cliente por una falla interna.

**Justificación.** F04 no describe qué hacer ante una falla — describe el mecanismo en funcionamiento normal. Bloquear el turno hasta lograr la reescritura contradice la expectativa de reactividad casi inmediata de WhatsApp. El riesgo asumido es responder un bloque con contexto desactualizado en un turno puntual; se corrige solo en el bloque siguiente que sí funcione.

---

### Regla 4 — Punto exacto de redacción de PII

**Decisión.** Un único checkpoint, inmediatamente después de que el `message_batch` cierra:

1. Se extrae el dato personal del texto crudo del bloque (F31) y se cifra y persiste en `contact_attributes` (con `source_message_id`).
2. Se sustituye el valor por su marcador de tipo (`[DNI]`, `[CUIT]`, `[CUIL]`, `[CBU]` — ADR-0006).
3. **Todo lo que sigue** — detección de intención, reescritura del contexto, consulta de RAG, armado de `system`/`user`, generación — trabaja exclusivamente sobre el texto ya redactado.

**Justificación.** `conversation_context_versions.rewritten_by_model` indica que la reescritura del contexto es en sí misma una llamada a un modelo — no lógica propia. Si ese modelo (o el de embeddings del RAG) fuera un proveedor externo, redactar sólo antes de la generación final dejaría una segunda vía de fuga que ADR-0006 no cubre. Un único checkpoint temprano es más simple de auditar y de defender: ninguna llamada a un modelo, propio o externo, ve el dato crudo después de este punto en el diagrama del Paso 8.

**Costo asumido.** Si algún clasificador interno se beneficiaría del dato crudo (por ejemplo, para reconocer mejor un patrón numérico en contexto), no lo tiene. Es el mismo costo que ADR-0006 ya aceptó como riesgo de falso negativo.

---

### Regla 5 — Presupuesto de tokens

**Decisión.** Si el prompt ensamblado no entra en la ventana de contexto del modelo, se recorta en este orden:

1. **Historial de la conversación**, empezando por los bloques más viejos — se conservan sólo los últimos N turnos textuales; lo anterior queda representado por `conversation_contexts.summary`.
2. Si con eso no alcanza, **fragmentos de RAG**, empezando por el de menor `similarity_score`.
3. **Nunca se recorta:** `system` (los seis componentes — lo exige OE1), el contexto dinámico estructurado, ni la pregunta actual del cliente.

**Justificación.** `system` no se puede tocar porque OE1 mide que el 100% de los prompts incorpore todos los componentes activos: recortarlo invalidaría el objetivo que se está midiendo. El contexto estructurado y la pregunta actual son pequeños y son la parte más específica del bloque — recortarlos es responder a ciegas. Queda el historial y el RAG como los dos componentes genuinamente compresibles, y `conversation_contexts.summary` — que ya existe en el catálogo — es evidencia de que el diseño ya prevé no depender del historial completo para mantener continuidad.

---

## 3. Extensiones identificadas, no modeladas todavía

> **Actualizado el 2026-08-10 ([ADR-0016](02-decisiones/0016-rastro-del-pipeline-de-respuesta.md)).** El equipo definió el pipeline completo de respuesta —*blocked-list → guardrail → triage → RAG → grounding (reranking si hace falta) → blocked-list → guardrail → respuesta*—, del que las dos extensiones de abajo son pasos. **Su rastro ya está modelado** en la entidad `pipeline_steps`: una fila por paso ejecutado, con su resultado, su versión y su latencia. Lo que sigue pendiente es el **diseño** de cada paso, no dónde registrarlo. El pipeline se dibuja en el **Paso 8** (secuencia del turno); las zonas de confianza, en el Paso 12.

Ninguna de las dos tiene anclaje textual en el anteproyecto — son mejoras de ingeniería que el equipo decidió incorporar, no un requisito de F01–F39. Se documentan como puntos de extensión en la secuencia del bloque:

- **Reranking.** Entre la consulta de RAG (paso 7 de la secuencia) y el armado del `user` (paso 9): un filtro/reordenamiento adicional sobre los candidatos recuperados por similitud, para los casos en que el top-k inicial no sea suficientemente relevante.
- **Guardrails de ciberseguridad de los datos.** Dos puntos de entrada distintos:
  - **De entrada**, inmediatamente después del checkpoint de redacción (Regla 4) — una segunda verificación de que no quedó ningún dato sensible sin redactar, mitigando el riesgo de falso negativo que ADR-0006 ya reconoce.
  - **De salida**, después de que el LLM genera la respuesta y antes de crear el `message` saliente — verificando que no filtre un dato sensible ni contradiga una restricción de `rules`.

Se profundizan cuando corresponda — probablemente en el Paso 12 (DFD de zonas de confianza) o en un ADR propio.

---

## 4. Ejemplo completo renderizado

> Contenido **ilustrativo**: el anteproyecto no provee datos reales del cooperante (el relevamiento detallado es trabajo de la Fase 1, sección 8.6). El dominio —alquiler de departamentos en Jesús María y Córdoba Capital— sí es el declarado en la sección 7.3. Este ejemplo se reemplaza por uno con contenido real cuando ese relevamiento exista.

### 4.1. Los seis `prompt_components` del agente (Capa 1)

| `position` | `type` | `content` |
|---|---|---|
| 1 | `objective` | "Ayudar a potenciales inquilinos a encontrar el departamento que mejor se ajuste a su necesidad y coordinar el inicio del proceso de alquiler." |
| 2 | `personality` | "Asesor inmobiliario cercano y resolutivo. Pregunta zona, presupuesto y ambientes antes de ofrecer opciones." |
| 3 | `tone` | "Formal pero cálido. Usa 'usted' con clientes nuevos; pasa a 'vos' si el cliente lo hace primero." |
| 4 | `brand_voice` | "Representa a una inmobiliaria familiar con trayectoria en Jesús María y Córdoba Capital. Trato personalizado, no de plataforma automatizada." |
| 5 | `behavior` | "Ante una propiedad puntual, consultar la base de conocimiento antes de responder. Si no hay dato, decirlo y ofrecer escalar. Nunca inventar precio, dirección ni disponibilidad." |
| 6 | `rules` | "No confirmar reservas ni tomar pagos por este canal. Si la conversación se deriva a un asesor, avisarle al cliente antes y resumirle brevemente lo hablado hasta ahí." |

> **Por qué `rules` no dice cuándo transferir.** Es tentador escribir ahí *"transferir si hay frustración o si la consulta sale del dominio"* — pero eso duplicaría lo que ya configura `transfer_policies`, y las dos definiciones pueden divergir. **La decisión de transferir la toma siempre el clasificador de F12 según sus umbrales**, y el clasificador no lee `rules`. Si `rules` dijera *"no transfieras nunca"*, el sistema transferiría igual y el administrador no tendría forma de saber por qué se ignoró lo que escribió. `rules` describe el comportamiento conversacional **alrededor** de la transferencia, no su disparo.

`tone` tiene dos filas en `prompt_component_versions`: la v1 era más informal; el administrador la editó a la actual (v2) — se versionó porque cambió el `content` (Regla 2), no por guardar sin tocar nada.

### 4.2. El bloque (F10/F11)

Una clienta ("Lucía", ejemplo) escribe tres mensajes en 40 segundos:

1. "Hola buenas"
2. "Vi el depto de 2 ambientes en Jesús María que publicaron"
3. "Mi DNI es 33445566 y quiero saber si todavía está disponible, mi presupuesto es hasta 350mil"

El agregador los agrupa en un `message_batch` (`closed_reason = silence`, `timer_ms_used ≈ 12000`, `messages_count = 3`).

### 4.3. Checkpoint de redacción (Regla 4)

Se extrae el DNI, se cifra y persiste en `contact_attributes` (`source_message_id` = el del mensaje 3). Texto redactado, el único que se usa de acá en adelante:

> "Hola buenas. Vi el depto de 2 ambientes en Jesús María que publicaron. Mi DNI es **[DNI]** y quiero saber si todavía está disponible, mi presupuesto es hasta 350mil."

### 4.4. `detected_intents`

| `intent_label` | `confidence` | `position` |
|---|---|---|
| `consulta_disponibilidad` | 0.94 | 1 |
| `consulta_precio` | 0.71 | 2 |

### 4.5. Reescritura del contexto (Regla 3)

**Antes** (`conversation_contexts`, bloque anterior, `turn_number = 4`):
```json
{
  "turn_number": 4,
  "summary": "Lucía busca alquilar un depto de 2 ambientes en Jesús María. Todavía no dio presupuesto.",
  "primary_intent": "consulta_zona",
  "objections_json": [],
  "urgency_level": 2,
  "budget_signal": null,
  "extracted_data_json": {"zona_interes": "Jesús María", "ambientes": 2}
}
```

**Después** (nueva fila en `conversation_context_versions`, `turn_number = 5`, `rewritten_by_model = "gpt-4o-mini"`, `latency_ms ≈ 800`):
```json
{
  "turn_number": 5,
  "summary": "Lucía busca depto de 2 ambientes en Jesús María, presupuesto hasta $350.000. Preguntó disponibilidad puntual.",
  "primary_intent": "consulta_disponibilidad",
  "objections_json": [],
  "urgency_level": 3,
  "budget_signal": "350000_ARS",
  "extracted_data_json": {"zona_interes": "Jesús María", "ambientes": 2, "presupuesto_max": 350000}
}
```

### 4.6. `retrieval_logs`

| `rank` | `similarity_score` | fragmento |
|---|---|---|
| 1 | 0.91 | "Depto 2 ambientes, Jesús María, zona centro. $320.000/mes + expensas. Disponible desde el 1°." |
| 2 | 0.78 | "Requisitos para alquilar: recibo de sueldo, garantía propietaria o seguro de caución, depósito de un mes." |

### 4.7. Ensamblado final (Regla 1 aplicada)

**`system`** — los seis componentes de 4.1, concatenados en su `position`.

**Historial** — bloques 1 a 4, como turnos `user`/`assistant` reales.

**`user`** del bloque 5 — contexto → RAG → pregunta redactada:
```
[CONTEXTO DE LA CONVERSACIÓN]
Intención: consulta_disponibilidad · Presupuesto: $350.000
Zona: Jesús María, 2 ambientes · Urgencia: 3/5

[BASE DE CONOCIMIENTO]
1. Depto 2 ambientes, Jesús María, zona centro. $320.000/mes + expensas. Disponible desde el 1°.
2. Requisitos: recibo de sueldo, garantía propietaria o seguro de caución, depósito de un mes.

[MENSAJE DEL CLIENTE]
Hola buenas. Vi el depto de 2 ambientes en Jesús María que publicaron. Mi DNI es [DNI]
y quiero saber si todavía está disponible, mi presupuesto es hasta 350mil.
```

### 4.8. `generations` + `generation_components`

- `generations`: `conversation_context_version_id` → la de `turn_number = 5` · `prompt_rendered` = el bloque 4.7 (ya redactado) · `completion_text` = *"¡Hola! Sí, el departamento de 2 ambientes en zona centro de Jesús María está disponible, a $320.000 + expensas — dentro de tu presupuesto. Para alquilarlo se necesita recibo de sueldo y garantía propietaria o seguro de caución, más un depósito de un mes. ¿Querés que coordine una visita?"* · `status = auto_sent` (modo autónomo).
- `generation_components`: 6 filas, una por cada `prompt_component_version_id` vigente (la **v2** de `tone`, no la v1), con su `position` 1–6.
- `retrieval_logs`: las 2 filas de 4.6, con `generation_id` de esta generación.

Esto es lo que el criterio de cierre del Paso 4 pedía poder señalar: el mensaje de Lucía se generó con estos seis componentes, en estas versiones, con este contexto y estos fragmentos — cuatro saltos completos, sin ningún hueco.

---

## 5. Criterio de cierre

Con las cinco reglas y el ejemplo de la sección 4, es posible responder por escrito, señalando filas concretas, la pregunta que el plan de diseño exige: *"¿con qué componentes, en qué versión, con qué contexto y con qué fragmentos recuperados se generó este mensaje?"* — que es exactamente la definición de "etapa de diseño terminada" en su punto 3.

**Pendiente para el Paso 5:** revisar el tipo de `generations.prompt_rendered` (hoy `text` en el catálogo) contra la Regla 1 — el prompt real es una secuencia de mensajes con rol (`system`/`user`/`assistant`), no un string plano.
