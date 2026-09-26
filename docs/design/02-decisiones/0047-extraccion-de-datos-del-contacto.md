# ADR-0047 — La extracción de datos del contacto: qué se extrae, quién lo guarda y qué pesa más

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · **§2.6 (el consentimiento) es una propuesta, pendiente del grupo** |
| **Fecha** | 2026-09-26 |
| **Etapa** | Construcción — Fase 9 (contexto dinámico y transferencia) |
| **Resuelve** | La contradicción O10 de [ADR-0042](0042-pipeline-de-decision-del-turno.md) —quién escribe `contact_attributes`—, qué datos extrae F31, con qué se extrae cada uno, y qué pasa cuando un dato extraído choca con uno cargado a mano |
| **Requisitos afectados** | F31 · F30 · sección 7.3 (Ley 25.326) · ADR-0006 |
| **Naturaleza** | F31 dice *"El LLM extrae datos del usuario durante la conversación. Toggle por agente. Datos sensibles encriptados según Ley 25.326"*. **No enumera qué datos**: la lista de §2.2 es decisión del equipo. Y **§2.3 se aparta de la letra de F31** para tres de ellos, por una contradicción del anteproyecto que se declara ahí |
| **Enmienda** | [07-secuencia-del-turno.md](../07-secuencia-del-turno.md), **§0** — `contact_attributes` sale de la lista de lo que escribe el cerebro y pasa a la del núcleo; y **el diagrama de §2** — la flecha *"C->>DB: INSERT contact_attributes"* pasa al núcleo, en la transacción que cierra el turno · [03-capa-prompts.md](../03-capa-prompts.md), **Regla 4, paso 1** — el *checkpoint* extrae y sustituye, pero **no persiste**: lo extraído viaja en la respuesta del turno y lo cifra y guarda el núcleo al cerrar. El orden *"persistir y recién entonces sustituir"* deja de valer · [ADR-0042](0042-pipeline-de-decision-del-turno.md), **O10** — deja de ser una contradicción abierta |
| **Relacionada con** | [ADR-0006](0006-anonimizacion-de-pii.md), [ADR-0017](0017-decomposicion-en-despliegues-y-lenguajes.md), [ADR-0031](0031-cifrado-en-reposo-de-datos-sensibles.md), [ADR-0032](0032-acceso-a-contactos-y-datos-sensibles.md), [ADR-0038](0038-alta-baja-y-pausa-del-agente-ia.md), [ADR-0042](0042-pipeline-de-decision-del-turno.md), [08-structure-rules.md](../08-structure-rules.md) |

---

## 1. Contexto

**La contradicción es anterior a esta fase.** [07-secuencia-del-turno.md](../07-secuencia-del-turno.md) §0 lista `contact_attributes` entre las tablas que escribe el cerebro, y la regla 3 de [08-structure-rules.md](../08-structure-rules.md) se lo prohíbe con nombre y apellido. [ADR-0031](0031-cifrado-en-reposo-de-datos-sensibles.md) y [`docs/struct.md`](../../struct.md) ya habían tomado partido —*"el clasificador de extracción devuelve el valor; el núcleo lo cifra y lo persiste"*—, pero `07-secuencia` y la Regla 4 de [03-capa-prompts.md](../03-capa-prompts.md) seguían describiendo al cerebro persistiendo. [ADR-0042](0042-pipeline-de-decision-del-turno.md) la registró como O10 y recomendó el núcleo.

Eso bloquea algo concreto. La Regla 4 pide un *checkpoint* que extrae el dato, lo cifra, lo persiste y recién entonces lo sustituye por su marcador. Como el cerebro no puede escribir la tabla, ese *checkpoint* no existe, y hoy la red del `guardrail` de entrada es lo único que sustituye algo.

Y F31 tiene un problema propio: **dice que el LLM extrae los datos**, y [ADR-0006](0006-anonimizacion-de-pii.md) dice que el DNI, el CUIT/CUIL y el CBU no llegan nunca al proveedor externo. Las dos cosas no pueden ser ciertas a la vez para esos tres datos.

## 2. Decisión

### 2.1 El cerebro extrae y devuelve, el núcleo cifra y guarda

**El cerebro devuelve lo extraído en la respuesta del turno** —clave, valor, confianza y el mensaje de origen—, y **el núcleo lo cifra ([ADR-0031](0031-cifrado-en-reposo-de-datos-sensibles.md)) y lo escribe en `contact_attributes` con `source = 'llm'`**, en la misma transacción que cierra el turno. Lo escribe por el repositorio de `contacts/`, dueño de la tabla ([08-structure-rules.md](../08-structure-rules.md), regla 1).

**El *checkpoint* de la Regla 4 queda en el cerebro, sin persistir**: extrae y sustituye, y todo lo que sigue trabaja sobre el texto redactado, como antes.

### 2.2 Qué se extrae: una lista cerrada

| Clave | Dato |
|---|---|
| `dni` | DNI |
| `cuit_cuil` | CUIT o CUIL, sin distinguir cuál ([ADR-0042](0042-pipeline-de-decision-del-turno.md), D6) |
| `cbu` | CBU |
| `first_name` | Nombre |
| `last_name` | Apellido |
| `email` | Email |

Los tres primeros son los que la sección 7.3 del anteproyecto nombra como sensibles. Los otros tres son campos del contacto de F30. **La lista vive en `rules/extraction.yaml`**, con su `version`. Una clave que no está en la lista no se guarda. **Lo comercial —presupuesto, urgencia, objeciones— no es un dato del contacto**: va al contexto dinámico de F04, que ya tiene columnas para eso en `conversation_contexts`.

### 2.3 Los datos sensibles los extrae el *checkpoint*, no el LLM

- **`dni`, `cuit_cuil` y `cbu` los saca el *checkpoint* con los mismos patrones con que los redacta**, antes de cualquier llamada a un modelo.
- **`first_name`, `last_name` y `email` los saca un clasificador con LLM** (`classifiers/extraction.py`), sobre el texto ya redactado.

**Es una desviación de la letra de F31**, que dice *"el LLM extrae"*, y se declara así: para los tres sensibles, F31 contradice ADR-0006 y la sección 7.3, y **gana la protección del dato**, que es la que tiene una ley detrás.

### 2.4 El *toggle* por agente

**Con `ai_agents.extraction_enabled = false` no se guarda nada y no se llama al clasificador.** La redacción del *checkpoint* sigue ocurriendo: la exige ADR-0006, que no depende de F31. El *toggle* entra en la API del agente IA en esta fase, como anticipó la regla 7 de [ADR-0038](0038-alta-baja-y-pausa-del-agente-ia.md).

### 2.5 Lo manual pesa más

- **Un valor extraído nunca pisa uno cargado a mano.** Si el contacto tiene una fila `source = 'manual'` viva para esa clave, lo extraído se descarta.
- **Un valor extraído reemplaza a otro extraído** de la misma clave: la fila vieja se da de baja (`deleted_at`, C09) y se escribe una nueva. Si el valor es el mismo, no se escribe nada.
- **La extracción no toca las columnas de `contacts`.** Nombre, apellido y email de F30 se cargan en el ABM; lo que el LLM encuentra queda en `contact_attributes`, al lado.

### 2.6 El consentimiento

**Propuesta, pendiente del grupo: no se guardan `dni`, `cuit_cuil` ni `cbu` de un contacto con `consent_given_at` en `NULL`.** La sección 7.3 del anteproyecto se compromete a *"consentimiento explícito y almacenamiento encriptado para datos sensibles (DNI, CUIL, CBU)"* —las dos cosas, no una— y hoy **nada en el sistema le pide consentimiento a la clienta**, así que la columna está en `NULL` en todos los contactos ([DER/entidades.md](../DER/entidades.md)). Con esta propuesta, **hasta que exista ese acto, la extracción de los tres datos sensibles no guarda nada**, aunque el *checkpoint* los siga redactando. La otra salida es guardarlos cifrados igual y declarar la desviación de 7.3.

## 3. Justificación

**Por qué el núcleo.** Es lo que dicen la regla 3, ADR-0031 y `struct.md`, y el argumento de fondo es el de `07-secuencia` §0: el cerebro escribe evidencia y el núcleo escribe hechos. Un dato del contacto no es evidencia de cómo se decidió un turno. Además, la llave de cifrado ya vive en el núcleo (`platform/encryption`), y darle una copia al cerebro duplicaría la superficie que ADR-0031 cuida.

**Por qué ya no se persiste antes de sustituir.** La Regla 4 no da un motivo para ese orden, y el dato crudo no se pierde si el turno falla después: el mensaje entrante está guardado entero en `messages`, así que se puede volver a extraer. Lo que la Regla 4 sí justifica —que ningún modelo vea el dato crudo después del *checkpoint*— se sostiene igual.

**Por qué patrones para lo sensible.** Mandarle el DNI al LLM para que lo extraiga es exactamente la fuga que ADR-0006 existe para impedir. Los patrones ya están escritos y probados para redactar: extraer con los mismos es usar lo que ya se encontró.

**Por qué una lista cerrada.** Una clave libre del LLM llena la tabla de nombres distintos para lo mismo (`telefono`, `tel`, `celular`) y hace imposible mostrarla o filtrarla. Y la regla 9 de `08-structure-rules.md` pide que lo que decide qué se guarda esté versionado.

**Por qué lo manual gana.** Lo cargó una persona que habló con la clienta. Lo extraído es una inferencia, con su margen de error.

## 4. Consecuencias

### Positivas

- **O10 se cierra**, y `07-secuencia`, la Regla 4, la regla 3, ADR-0031 y `struct.md` dicen lo mismo.
- **El *checkpoint* de la Regla 4 existe**, y la red del `guardrail` de entrada vuelve a ser la segunda verificación que la Regla 4 quería.
- **Ningún dato sensible llega al proveedor**, ni siquiera para extraerlo.
- **El vendedor ve los datos del contacto que surgieron en la conversación** sin tener que cargarlos.

### Negativas / costos asumidos

- **Lo extraído viaja sin cifrar entre los dos servicios**, en la respuesta del turno. Es la red interna del despliegue, pero es un camino nuevo para el dato, y el cerebro no puede registrarlo en sus logs.
- **Se desvía de la letra de F31** para los tres datos sensibles, y hay que decirlo en el informe.
- **Los patrones se equivocan como se equivocan al redactar.** Un celular argentino con el 9 adelante se guarda como `cuit_cuil` (ADR-0042, D6), y **esta vez el error no es sólo una etiqueta: queda guardado como dato del contacto**.
- **Un CUIT y un CUIL comparten clave.** Distinguirlos al persistir sigue abierto desde ADR-0042.
- **Una llamada más al LLM por turno** cuando el *toggle* está prendido. Es un criterio de costo del equipo, no una métrica del anteproyecto.
- **Si el turno falla después del *checkpoint*, lo extraído en ese turno no se guarda.** Se recupera sólo si alguien vuelve a correr la extracción sobre `messages`, y nada lo hace.
- **Lo extraído que choca con lo manual se descarta sin dejar rastro**, así que no se puede medir cuántas veces el LLM contradijo a una persona.
- **Si §2.6 se acepta, los tres datos sensibles no se guardan nunca** hasta que exista el acto de consentimiento, y la mitad de F31 que más importa queda construida y sin efecto.
- **La lista de §2.2 es una lectura del equipo**, y el anteproyecto no dice qué datos le sirven al vendedor.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **El cerebro escribe `contact_attributes`** | Contradice la regla 3, ADR-0031 y `struct.md`, y obliga a darle al cerebro la llave de cifrado |
| **El LLM extrae también los datos sensibles** | Le manda al proveedor externo el dato que ADR-0006 redacta |
| **Claves libres que elige el LLM** | Nombres distintos para el mismo dato, y nada versionado que decida qué se guarda |
| **Lo extraído actualiza las columnas de `contacts`** | Pisa lo que cargó una persona, en la tabla que usa la bandeja |
| **El valor extraído más reciente gana siempre, también sobre lo manual** | Lo decidió el equipo en contra: lo manual pesa más |
