# ADR-0017 — Decomposición en despliegues y reparto de lenguajes

| Campo | Valor |
|---|---|
| **Estado** | **Propuesta** — requiere acuerdo de los tres autores antes de darse por firmada |
| **Fecha** | 2026-08-16 |
| **Etapa** | Diseño — antes del Paso 7 (nivel físico) |
| **Resuelve** | La sección 8.4 deja el lenguaje sin decidir (*"Python o GO"*, *"Go o Python"*) y 8.5 no dice si los módulos son procesos separados |
| **Requisitos afectados** | Transversal · OE5 (cobertura) · OE2 (cadena de trazabilidad) |

---

## 1. Contexto

El anteproyecto fija la arquitectura funcional y deja abierta la de despliegue.

**Sección 8.5** describe *"tres **módulos integrados** con responsabilidades claramente separadas"* —Gateway, Módulo 2 (agregador y clasificador), Módulo 1 (motor de prompts), Módulo 3 (plataforma operativa)— más *"Persistencia (PostgreSQL + Vector DB): almacena **todos** los estados del sistema"*.

**Sección 8.4** asigna lenguajes sin decidirlos: *"Python o GO"* para los Módulos 1 y 2, *"Go (Golang) o Python"* para el Módulo 3, con la justificación *"goroutines para concurrencia masiva de webhooks y WebSockets sin overhead de threading"*.

**El anteproyecto mezcla tres preguntas distintas que conviene separar:**

1. ¿Cómo se reparten las **responsabilidades**? → 8.5 ya la responde: tres módulos.
2. ¿Cuántos **procesos desplegables** hay? → sin responder.
3. ¿En qué **lenguaje** se escribe cada cosa? → sin responder.

Responder (2) y (3) copiando la respuesta de (1) es lo natural, y es lo que este ADR argumenta que no conviene.

### Lo que aparece al mirar quién escribe cada tabla

| Tabla | Módulos que escriben |
|---|---|
| `message_batches`, `detected_intents`, `signal_evaluations`, `transfer_events` | M2 |
| `conversation_contexts`, `generations`, `retrieval_logs`, `pipeline_steps` | M1 |
| `contacts`, `operators`, `labels`, `sales`, configuración de agentes | M3 |
| **`conversations`** | **M2** (estado, transferencia) · **M3** (asignación, modo, destacado) · **M1** (actividad) |
| **`messages`** | **Gateway/M2** (entrantes) · **M1** (respuesta de la IA) · **M3** (respuesta del operador) |

**Las dos tablas del núcleo las escriben los tres módulos.** Es la señal clásica de que la frontera modular no sigue al dato: el agregado central es compartido.

### Y el Módulo 2 son dos cosas distintas

| Dentro de M2 | Qué clase de problema es |
|---|---|
| Timer adaptativo y agregación (F10) | **Concurrencia con estado**: miles de temporizadores vivos que se reinician con cada mensaje entrante |
| Clasificadores de frustración, intención y transferencia (F11, F12) | **Inferencia de modelos** — 8.4 nombra scikit-learn y XGBoost |
| Transcripción de audio (§8.5) | Inferencia de modelos |

La costura real del sistema no es M1 / M2 / M3: es **código que habla con modelos** contra **código que habla con usuarios y con la base**. La frontera de 8.4 la cruza justo por el medio.

## 2. Decisión

**(a) Los tres módulos son *bounded contexts*, no servicios.** Se mantienen como paquetes con fronteras explícitas y se presentan así en el informe, tal como los describe 8.5.

**(b) Dos procesos desplegables:**

| Servicio | Lenguaje | Contiene |
|---|---|---|
| **Núcleo operativo** | **Go** | Gateway (webhooks, validación HMAC-SHA256) · timer adaptativo y agregación · fan-out por WebSockets · ABM y consultas de bandeja, contactos, etiquetas, recordatorios, ventas y métricas |
| **Cerebro** | **Python** | Composición y render de prompts · pipeline RAG y reranking · llamadas al LLM · clasificadores de F11/F12 · transcripción de audio · LLM-as-judge |

**(c) Un único PostgreSQL, compartido por ambos servicios.** Es lo que ya dice 8.5, y es innegociable — ver §3.

**(d) El Módulo 2 se reparte entre los dos servicios:** el timer va al núcleo (Go), los clasificadores al cerebro (Python). Es la desviación explícita respecto del reparto por módulo de 8.4.

**(e) El cerebro se expone como API interna** que el núcleo invoca. No hay cola de mensajes: el protocolo MQTT está fuera de alcance por 7.3.

**(f) El frontend sigue *A DEFINIR*.** Este ADR no lo decide; continúa como pendiente heredado de 8.4.

## 3. Justificación

- **La cadena de trazabilidad de OE2 no puede volverse un join distribuido.** `message → generation → generation_components → prompt_component_version → conversation_context_version → retrieval_logs` cruza los tres módulos, y es *"lo que hace reproducible a OE2"* según [relaciones.md §3](../DER/relaciones.md). Con base por servicio deja de ser una consulta y pasa a ser consistencia eventual entre procesos. No se rompería "la base": se rompería el aporte original que declara la sección 6.3.

- **Los módulos no tienen volumen independiente.** Cada turno atraviesa Gateway → M2 → M1 → M3 **en orden y de forma sincrónica**, porque el cliente está esperando en WhatsApp. Los volúmenes están acoplados 1 a 1: no se puede escalar M1 sin escalar M2. Un pipeline donde toda petición recorre todos los componentes es el caso donde separar deployables agrega latencia y modos de falla **sin comprar independencia**.

- **La frontera propuesta sí separa cosas que difieren de verdad.** El ecosistema de LLM, embeddings y evaluación vive en Python; el manejo de miles de conexiones WebSocket y de temporizadores con estado es más limpio y más barato en Go. Esa diferencia es real; la que hay entre M1 y M2 no lo es.

- **Costo operativo proporcionado al equipo.** Tres o cuatro servicios significan otros tantos despliegues, configuraciones, contratos entre servicios y sesiones de depuración cruzando procesos. Son tres personas y trece meses.

- **No contradice el texto del anteproyecto.** 8.5 dice *"módulos integrados"*, no servicios; 8.4 ofrece los dos lenguajes para todos los módulos. Este ADR elige dentro del espacio que el anteproyecto dejó abierto — salvo en el punto (d), que sí es un refinamiento, y por eso se declara.

## 4. Consecuencias

### Positivas

- Cada lenguaje se usa donde su ecosistema aporta, sin forzar ninguno de los dos.
- La trazabilidad de OE2 sigue siendo una consulta SQL.
- Dos artefactos que desplegar en lugar de cuatro.
- El reparto de personas del plan —A en la capa de prompts, B en núcleo y operación, C en ejecución y evaluación— sigue funcionando sin bloqueos mutuos.

### Negativas / costos asumidos

- **OE5 se complica.** Exige *"cobertura de pruebas unitarias ≥ 80%"*: con dos runtimes hay dos cadenas de herramientas, dos reportes, y hay que definir **cómo se agregan ambos en un solo número**. SonarQube lo soporta, pero es trabajo que un monolito no tendría.
- **Hay un salto de red en el camino caliente de cada turno.** El núcleo llama al cerebro y el cliente espera. Agrega latencia y un modo de falla nuevo —el cerebro caído— que hay que contemplar en la secuencia del Paso 8.
- **Base compartida entre dos servicios es acoplamiento por esquema.** Una migración toca a los dos y hay que ordenar los despliegues. Es el precio deliberado de no romper la trazabilidad.
- **El equipo sostiene dos lenguajes** durante trece meses: estilo, linters y revisión de código en ambos.
- **Se desvía del reparto por módulo de 8.4** al partir el Módulo 2. Hay que explicarlo en el informe: el módulo sigue existiendo conceptualmente, pero su implementación vive en dos procesos.

### Condición de reversión

Si hacia el final de la Fase 2 el equipo está gastando más tiempo en la frontera entre los dos servicios que en las funcionalidades, **la salida es colapsar a un único servicio Python** y mover el timer a `asyncio`. Es una vuelta atrás barata mientras el esquema de base no cambie, y conviene tenerla escrita ahora y no descubrirla bajo presión.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Un servicio por módulo (tres o cuatro procesos)** | Es la lectura literal de 8.5 y la más costosa. Los módulos no tienen volumen independiente —cada turno los recorre todos en orden—, así que la separación agrega latencia, despliegues y depuración distribuida sin comprar escalado independiente. Además `conversations` y `messages` los escriben los tres: la frontera de proceso no coincide con la frontera del dato. |
| **Monolito único en Python** | **Es la alternativa más fuerte y conviene decirlo.** Sería lo más rápido de construir, con una sola cadena de herramientas y un solo reporte de cobertura, y la carga real —una PYME cooperante con decenas de conversaciones diarias— la resuelve `asyncio` sin dificultad. Se descarta porque el timer adaptativo de F10 es genuinamente más limpio con goroutines y canales, y porque el equipo declara interés en sostener el uso de Go. **Pero ése es el motivo real y hay que declararlo así:** la justificación de 8.4 —*"concurrencia masiva de webhooks y WebSockets"*— **no está respaldada por el alcance de 7.3**, que es una cooperante propietaria de departamentos en alquiler. Sostener un argumento de escala que el alcance no respalda es exactamente lo que un tribunal pregunta. |
| **Monolito único en Go** | Renuncia al ecosistema de LangChain, los SDK de LLM, los tokenizadores y las bibliotecas de evaluación, que es donde vive el aporte de la tesina. Reimplementarlo en Go es trabajo sin valor académico. |
| **Base de datos por servicio** | Contradice 8.5 —*"almacena todos los estados del sistema"*— y rompe la cadena de trazabilidad de OE2, convirtiéndola en consistencia eventual entre procesos. Es la única alternativa que este ADR descarta sin matices. |
| **Cola de mensajes entre núcleo y cerebro** | 7.3 declara el protocolo MQTT explícitamente fuera de alcance. Y una cola cambiaría el turno de sincrónico a asincrónico, lo que contradice la expectativa de respuesta casi inmediata de WhatsApp. |
