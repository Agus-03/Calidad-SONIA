# Módulos derivados del anteproyecto — ejercicio de validación

| Campo | Valor |
|---|---|
| **Ubicación** | `z_helps/` — **material de trabajo, sin peso oficial.** No es una decisión ni se referencia desde `docs/` |
| **Fecha** | 2026-08-16 |
| **Para qué** | Contrastar la división en módulos que ya existe contra una derivada desde cero |
| **Base** | Anteproyecto: Anexo A (F01–F39), sección 7.2 (OE1–OE7), Anexo B (roles) |

---

## 0. Regla del ejercicio

**Se ignoran deliberadamente los Módulos 1, 2 y 3 del anteproyecto.** No se los menciona hasta la sección 7, y sólo para comparar. La idea es no anclarse: si se parte de la división existente, cualquier análisis termina justificándola.

Los módulos de abajo salen de recorrer los 39 requisitos y preguntar tres cosas por cada uno:

1. **¿Qué clase de trabajo es?** — IO externo · concurrencia con estado · inferencia · ABM · consulta · trabajo por lotes.
2. **¿Qué datos escribe?** — un módulo que no es dueño de ninguna tabla probablemente no es un módulo.
3. **¿Qué forma de ejecución tiene?** — servidor de larga vida · llamada sincrónica · trabajo en segundo plano · herramienta offline.

La tercera es la que decide cuántos procesos hay. Las dos primeras deciden dónde está cada frontera.

---

## 1. Clasificación de los 39 requisitos

| Requisito | Clase de trabajo |
|---|---|
| F20 webhooks · F21 (recepción) · F29 restricción de borrado | **IO externo** |
| F08 · F37 (sincronización de plantillas con Meta) | **IO externo** |
| F10 timer adaptativo | **Concurrencia con estado** |
| F28 recordatorios (el disparo, no el ABM) | **Concurrencia con estado** |
| F04 reescritura de contexto · F11 intenciones · F12 transferencia · F31 extracción de datos · F08 (sugerencia del LLM) | **Inferencia** |
| F06 (búsqueda semántica) | **Inferencia** |
| F06 (ingesta de documentos, hasta 100 MB) | **Trabajo por lotes** |
| F01 · F02 · F03 · F05 · F07 configuración y render del agente | **ABM + render** |
| F21 (distribución) · F22 · F23 · F25 · F26 · F27 | **Tiempo real + consulta caliente** |
| F09 · F13 · F14 · F15 · F17 · F18 · F19 · F24 · F28 · F30 · F32 · F33 | **ABM** |
| F16 · F34 · F35 · F36 · F38 · F37 (visualización) | **Consulta agregada** |
| F39 restricción de campañas | Documental |

**Lo primero que salta:** la inferencia no está concentrada. Aparece en requisitos que temáticamente no tienen nada que ver entre sí — extracción de datos del contacto (F31), sugerencia de reformulación de plantillas (F08), clasificación de transferencia (F12). **Cualquier división temática va a repartir inferencia por todos lados.**

---

## 2. Los módulos que salen

Nueve. Cada uno con las tablas que sería dueño de escribir.

### M-A · Canal

> **Qué hace:** recibir webhooks de Meta, validar la firma HMAC-SHA256, deduplicar por `wamid`, registrar el evento y reintentar. Enviar los mensajes salientes. Sincronizar el catálogo de plantillas.
>
> **Requisitos:** F20 · F21 (mitad de recepción) · F08 y F37 (mitad de sincronización) · F29
>
> **Escribe:** `inboxes` · `webhook_events` · `meta_templates` · `messages` (entrantes y salientes)
>
> **Forma:** servidor HTTP de larga vida. Debe ser idempotente y no perder nada.

### M-B · Turno

> **Qué hace:** agrupar los mensajes fragmentados. Un temporizador por conversación que se reinicia con cada mensaje y cierra el bloque tras el silencio.
>
> **Requisitos:** F10
>
> **Escribe:** `message_batches`
>
> **Forma:** proceso con estado en memoria + persistencia. Muchos temporizadores vivos y cancelables.

### M-C · Cognición

> **Qué hace:** todo lo que llama a un modelo dentro de un turno — redacción de PII, detección de intenciones, clasificación de señales, triage, recuperación RAG, grounding, generación de la respuesta, reescritura del contexto, extracción de datos del contacto.
>
> **Requisitos:** F04 · F06 (búsqueda) · F11 · F12 · F31 · F08 (sugerencia)
>
> **Escribe:** `detected_intents` · `signal_evaluations` · `transfer_events` · `pipeline_steps` · `conversation_contexts` (+ versiones) · `generations` · `generation_components` · `retrieval_logs` · `contact_attributes`
>
> **Forma:** servicio sincrónico. Su latencia la domina la API externa del LLM, no el CPU propio.

### M-D · Identidad del agente

> **Qué hace:** administrar los seis componentes del prompt, sus versiones y las políticas de transferencia. **Y ensamblar el prompt** — tanto para previsualizarlo (F03) como para usarlo en un turno real.
>
> **Requisitos:** F01 · F02 · F03 · F05 · F07 · F17
>
> **Escribe:** `ai_agents` · `prompt_components` · `prompt_component_versions` · `transfer_policies`
>
> **Forma:** ABM común… con una excepción que se detalla en §5. **Es el módulo con la frontera más incómoda de todo el sistema.**

### M-E · Conocimiento

> **Qué hace:** ingerir los documentos del negocio, fragmentarlos, generar embeddings e indexarlos en Qdrant. Mantener el límite de 100 MB por agente y reindexar cuando cambie el modelo de embeddings.
>
> **Requisitos:** F06 (mitad de ingesta)
>
> **Escribe:** `knowledge_bases` · `documents` · `document_chunks`
>
> **Forma:** **trabajo en segundo plano.** Es el único del sistema que no cabe en una petición HTTP — ver §5.

### M-F · Bandeja y tiempo real

> **Qué hace:** distribuir los mensajes a los operadores conectados por WebSocket, servir el listado de chats con sus filtros, gestionar asignación, modo y destacado por conversación.
>
> **Requisitos:** F21 (mitad de distribución) · F22 · F23 · F25 · F26 · F27
>
> **Escribe:** `conversations` (campos operativos) · `conversation_assignments`
>
> **Forma:** servidor de larga vida con muchas conexiones ociosas. **Es la consulta más caliente del sistema.**

### M-G · Operación

> **Qué hace:** el ABM de todo lo demás — contactos, operadores y roles, etiquetas, respuestas rápidas, recordatorios, ventas, puntajes, notas internas.
>
> **Requisitos:** F09 · F13 · F14 · F15 · F18 · F19 · F24 · F28 · F30 · F32 · F33
>
> **Escribe:** `contacts` · `operators` · `labels` · `contact_labels` · `conversation_labels` · `reminders` · `sales` · `quick_replies` · `internal_notes` · `audit_logs`
>
> **Forma:** CRUD con autenticación y control de acceso por rol (Anexo B).

### M-H · Reportes

> **Qué hace:** las consultas agregadas de los paneles.
>
> **Requisitos:** F16 · F34 · F35 · F36 · F38 · F37 (visualización)
>
> **Escribe:** nada. **Es de sólo lectura**, y eso lo vuelve trivial de mover o replicar más adelante.
>
> **Forma:** consultas pesadas y poco frecuentes. Perfil opuesto al de M-F.

### M-I · Evaluación

> **Qué hace:** medir los objetivos. Gestionar los conjuntos de 200 y 500 conversaciones, correr el juez de tono, calcular precisión y barridos de umbral, producir los números que van al informe.
>
> **Objetivos:** OE2 · OE3 · OE4 · OE6 · OE7
>
> **Escribe:** `conversation_annotations` · `tone_evaluations`
>
> **Forma:** **herramienta offline.** No se despliega: se corre cuando hace falta medir.

---

## 3. De nueve módulos a cuatro procesos

Los módulos son fronteras conceptuales. Los procesos salen de la **forma de ejecución**, que es lo único que obliga a separar:

| Proceso | Módulos | Por qué van juntos |
|---|---|---|
| **Núcleo** | M-A · M-B · M-F · M-G · M-H | Todos son un servidor de larga vida sobre la misma base. Comparten sesión, autenticación y conexiones |
| **Cerebro** | M-C · M-D (render) | Servicio sincrónico de inferencia. Se invoca por turno |
| **Trabajador** | M-E · evaluación de tono asincrónica | Trabajos largos que no caben en una petición |
| **Herramientas** | M-I | Offline. No se despliega |

Cuatro artefactos, de los cuales **tres se despliegan**.

---

## 4. Reparto Go / Python

| Proceso | Lenguaje | Por qué |
|---|---|---|
| **Núcleo** | **Go** | Webhooks e IO, HMAC en biblioteca estándar, una goroutine por conexión WebSocket, temporizadores con `time.Timer` y canales, y tipado en compilación sobre un esquema de 37 entidades |
| **Cerebro** | **Python** | SDKs de LLM, tokenizadores para el presupuesto de tokens, LangChain/LlamaIndex, `sentence-transformers` para reranking, y scikit-learn/XGBoost que la sección 8.4 nombra explícitamente |
| **Trabajador** | **Python** | Comparte todo el código de embeddings y fragmentación con el cerebro |
| **Herramientas** | **Python** | Notebooks, métricas, y el arnés de LLM-as-judge |

**La línea que separa Go de Python es una sola: ¿este código llama a un modelo?**

No es temática, no es por módulo del anteproyecto y no depende de qué funcionalidad se esté construyendo. Es la única frontera que se sostiene sola al recorrer los 39 requisitos.

---

## 5. Tres cosas que aparecieron y no están en ninguna descripción del sistema

### 5.1. La ingesta de documentos necesita un trabajador, y nadie lo nombró

F06 admite **documentos de hasta 100 MB por agente**. Fragmentar, generar embeddings e indexar eso lleva minutos, no milisegundos. **No cabe dentro de una petición HTTP**: el administrador sube el archivo, la petición responde, y el trabajo sigue.

Lo notable es que **el modelo de datos ya lo sabe**: `documents.status` tiene los valores `pending · processing · indexed · failed`. Eso es, literalmente, el ciclo de vida de un trabajo en segundo plano. El DER anticipó un proceso que la arquitectura nunca declaró.

Consecuencia: hace falta decidir **cómo se encolan esos trabajos**. Y hay una restricción: 7.3 declara el protocolo MQTT fuera de alcance, así que la cola tiene que resolverse de otra forma — una tabla de trabajos en PostgreSQL alcanza y es lo más simple.

### 5.2. El ensamblador del prompt vive en dos lugares a la vez

F03 pide que **el administrador vea el prompt ensamblado en tiempo real mientras edita los componentes**. Y el turno real necesita ensamblar exactamente lo mismo.

Si el ABM del agente lo sirve el núcleo (Go) y el ensamblador vive en el cerebro (Python), previsualizar un prompt es una llamada del núcleo al cerebro. Es aceptable — pero **hay que decidirlo explícitamente**, porque la alternativa aparece sola: alguien reimplementa el ensamblado en Go "porque es sólo concatenar seis textos".

El riesgo de que eso pase es concreto y grave: **OE1 mediría un ensamblador mientras el administrador previsualiza otro.** Dos implementaciones del mismo render divergen en semanas, y la métrica del 100 % pasaría a medir algo que nadie ve.

**El ensamblador tiene que tener una sola implementación, y el lado que la tenga es Python** — porque es el que necesita el tokenizador para el presupuesto de tokens.

### 5.3. La capa de evaluación es un módulo entero que nadie declaró

**Cinco de los siete objetivos específicos son mediciones.** Requieren: gestionar los conjuntos de 200 y 500 conversaciones etiquetadas, correr el juez de tono, calcular precisiones, hacer barridos de umbral, y producir los números del informe.

Eso es trabajo real, con sus propias tablas (`conversation_annotations`, `tone_evaluations`) y su propia forma de ejecución. Y **no aparece en ninguna descripción de módulos**, porque no es una funcionalidad del producto: es el instrumental de la tesina.

No hace falta desplegarlo. Pero sí conviene que exista como artefacto con dueño, o va a terminar siendo una carpeta de scripts sueltos — que es exactamente lo que el hallazgo H-10 advirtió que arruinaba la reproducibilidad.

---

## 6. Lo que revela el reparto

Al agrupar por forma de ejecución en vez de por tema, quedan visibles tres perfiles que conviene no mezclar:

| Perfil | Módulos | Característica |
|---|---|---|
| **Camino caliente** | M-A, M-B, M-F | Miles de operaciones por día, latencia importa, el cliente espera |
| **Camino caro** | M-C, M-E | Pocas operaciones, cada una cuesta plata y segundos |
| **Camino frío** | M-G, M-H, M-I | Poco volumen, sin urgencia, mayormente lectura |

Un solo proceso que mezcla los tres perfiles es lo que hace difícil razonar sobre rendimiento: una consulta de reporte pesada compite con el fan-out de WebSockets.

---

## 7. Comparación con los tres módulos del anteproyecto

Recién acá se levanta la regla de §0.

| Módulo del anteproyecto | Se corresponde con | Observación |
|---|---|---|
| **Gateway** | M-A | Coincide bien. Es el componente mejor delimitado del anteproyecto |
| **Módulo 1** — motor de prompts (F01–F09) | M-C + M-D + M-E | **Mezcla tres formas de ejecución distintas**: ABM (F01, F09), inferencia sincrónica (F04, F06 búsqueda) y trabajo por lotes (F06 ingesta) |
| **Módulo 2** — agregador y clasificador (F10–F16) | M-B + parte de M-C + M-G + M-H | Mezcla concurrencia con estado (F10), inferencia (F11, F12) y ABM/reportes (F13–F16, que no tienen nada de IA) |
| **Módulo 3** — plataforma operativa (F17–F39) | M-F + M-G + M-H + parte de M-C | Contiene **F31, que es inferencia**: el LLM extrayendo datos del contacto |

### El hallazgo principal

**Los tres módulos del anteproyecto contienen trabajo de inferencia:**

- Módulo 1 → F04, F06, F08
- Módulo 2 → F11, F12
- Módulo 3 → **F31**

Por lo tanto **asignar un lenguaje por módulo no puede funcionar**, sea cual sea la asignación. No es un problema de haber elegido mal: es que la división temática y la división por tipo de trabajo son perpendiculares.

### Lo que la división del anteproyecto hace bien

No es un mal diseño, y conviene decirlo: **los tres módulos se corresponden casi exactamente con las tres contribuciones que declara la sección 6.3** — composición de prompts, clasificación de transferencia y agregación, plataforma operativa. Como decomposición **del aporte**, y por lo tanto como estructura del informe y reparto entre las tres personas, funciona muy bien.

El problema aparece sólo cuando se la usa para decidir **procesos y lenguajes**, que es una pregunta distinta.

---

## 8. Preguntas para validar

1. ¿Están de acuerdo en que la línea Go/Python es *"¿llama a un modelo?"* y no *"¿qué módulo es?"*
2. ¿Aceptan un **tercer artefacto desplegable** (el trabajador) para la ingesta de documentos, o prefieren resolverlo dentro del cerebro con el riesgo de peticiones de minutos?
3. ¿Dónde vive el ensamblador del prompt, y cómo se garantiza que haya **una sola implementación**?
4. ¿La capa de evaluación va a ser un artefacto con dueño, o scripts sueltos?
5. ¿Se mantienen los tres módulos como estructura del **informe** aunque los procesos se dividan distinto? (Recomendado: sí, y decirlo explícitamente en el documento de arquitectura.)
