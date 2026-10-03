# ADR-0066 — Los datos personales dentro de los documentos: se avisa y se acepta al subir, y no se redactan

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · avisar y registrar la aceptación en cada subida, sin rechazar ni redactar el documento, y acotar ADR-0006 a la conversación con la clienta, elegidos por el grupo el 2026-10-03 |
| **Fecha** | 2026-10-03 |
| **Etapa** | Construcción — Fase 12 (conocimiento) |
| **Resuelve** | Qué se hace con los datos personales que trae un documento de la base de conocimiento, que [ADR-0064](0064-ingesta-de-documentos.md) dejó abierto |
| **Requisitos afectados** | F06 · secciones 5.2, 6.3 y 7.3 (Ley 25.326) |
| **Enmienda** | [ADR-0006](0006-anonimizacion-de-pii.md), **§2** — *"antes de que cualquier texto salga hacia el proveedor de LLM externo"* pasa a leerse como el texto de la conversación con la clienta: el contenido de los documentos sale sin redactar · [03-capa-prompts.md](../03-capa-prompts.md), **Regla 4** — los fragmentos que recupera el RAG entran al prompt sin pasar por el *checkpoint*, así que la frase *"ninguna llamada a un modelo (…) ve el dato crudo después de este punto"* vale para lo que escribió la clienta y no para lo que trae un documento · [ADR-0064](0064-ingesta-de-documentos.md), ***Lo que queda abierto*** — la fila de los datos personales se cierra |
| **Relacionada con** | [ADR-0031](0031-cifrado-en-reposo-de-datos-sensibles.md), [ADR-0042](0042-pipeline-de-decision-del-turno.md), [ADR-0047](0047-extraccion-de-datos-del-contacto.md), [ADR-0064](0064-ingesta-de-documentos.md) |

---

## 1. Contexto

Un documento de la base de conocimiento puede traer datos personales: el CUIT de la empresa en un contrato modelo, un CBU para las señas, una lista de inquilinos. Cada fragmento viaja al proveedor de embeddings al ingerirse, y al proveedor del LLM cada vez que el RAG lo recupera para armar un prompt.

[ADR-0006](0006-anonimizacion-de-pii.md) §2 dice que *"antes de que **cualquier texto** salga hacia el proveedor de LLM externo, los datos personales identificables se reemplazan por un marcador"*, y la Regla 4 de [03-capa-prompts.md](../03-capa-prompts.md) promete que *"ninguna llamada a un modelo, propio o externo, ve el dato crudo"* después del *checkpoint*. Las dos se escribieron pensando en la conversación: el ejemplo de ADR-0006 es una clienta que pasa su CBU para la seña.

Se evaluaron cuatro salidas:

| | Qué hace | Por qué no se eligió, o sí |
|---|---|---|
| **No hacer nada** | Ingerir el documento como viene, sin avisar | Nadie sabe que los datos salen hacia los proveedores |
| **Redactar al ingerir** | Reemplazar DNI, CUIT/CUIL y CBU por sus marcadores antes de fragmentar | Cambia el contenido del documento sin que nadie lo vea, y con los patrones de hoy redactaría precios: `$ 85.000.000` tiene la forma de un DNI con puntos |
| **Rechazar el documento** | `failed`, con qué tipos encontró y en qué página | Un manual con el CUIT de la empresa no tiene por qué rebotar |
| **Avisar y que quien sube acepte** | El documento se ingiere como viene, y la aceptación queda registrada | **La elegida** |

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **Subir un documento exige aceptar un aviso.** El aviso dice que el documento puede contener datos personales, que su contenido se envía a los proveedores de IA que lo procesan —el de embeddings al ingerirlo y el del LLM al responder con él— y que quien lo sube se responsabiliza por haberlo cargado |
| 2 | **La aceptación se registra en cada subida**, en una columna nueva, `documents.personal_data_terms_version`: la versión del texto que se aceptó. Quién aceptó y cuándo ya están en `uploaded_by_operator_id` y `created_at`, porque se acepta en el mismo pedido que sube |
| 3 | **El texto del aviso y su versión viven en el núcleo**, que es quien registra la aceptación. El pedido de subida trae la versión aceptada, y el núcleo rechaza la subida si falta o si no es la vigente |
| 4 | **El documento no se redacta ni se rechaza por traer datos personales.** Se ingiere como viene, con las reglas de [ADR-0064](0064-ingesta-de-documentos.md) |
| 5 | **El guardrail de salida no cambia** (D3 de [ADR-0042](0042-pipeline-de-decision-del-turno.md)): una respuesta que cite un DNI, un CUIT/CUIL o un CBU se sigue rechazando, aunque el dato venga de un documento aceptado. El dato puede servir de contexto; el AIAgent no lo dicta |
| 6 | **ADR-0006 y la Regla 4 de `03-capa-prompts` cubren la conversación con la clienta**, que es lo que denuncia la sección 5.2: lo que ella escribe se sigue redactando antes de salir hacia cualquier modelo. **El contenido de los documentos queda afuera de esa promesa**, bajo la aceptación de la regla 1 |

## 3. Justificación

- **Avisar en vez de rechazar** porque un documento con datos personales es muchas veces un documento legítimo: un manual, un contrato modelo, una política de pagos. Rechazarlo obliga a editar el PDF por un dato que el negocio eligió publicar en su propia base.
- **Avisar en vez de redactar** porque redactar cambia el contenido en silencio, y con los patrones del chat redactaría precios.
- **La responsabilidad es de quien carga el documento.** El contenido de la base de conocimiento lo elige el negocio, no la clienta ni el sistema. El aviso hace explícito que quien sube sabe adónde va el contenido, y la columna deja la prueba de qué aceptó.
- **La promesa de 5.2 es sobre la conversación.** La sección 5.2 habla de *"las conversaciones comerciales"* que *"frecuentemente incluyen datos personales"*. Lo que escribe la clienta no lo elige el negocio: por eso se redacta sin preguntarle a nadie. Un documento sí lo elige el negocio.
- **Una columna por subida y no una aceptación por persona** porque el aviso puede cambiar de versión, y lo que hay que poder probar es qué texto se aceptó para cada documento.
- **D3 se mantiene** porque protege a la clienta: que el negocio haya aceptado mandar un dato a un proveedor no significa que el AIAgent tenga que dictárselo a quien pregunte.

## 4. Consecuencias

### Positivas

- Ningún documento legítimo se rechaza ni cambia por traer un dato personal.
- Queda registrado, por documento, qué aviso aceptó quien lo subió.
- No hace falta un detector de datos personales para documentos, con sus falsos positivos sobre los precios.
- La conversación con la clienta mantiene toda la protección de ADR-0006.

### Negativas / costos asumidos

- **El sistema sí manda datos personales sin redactar a un proveedor externo.** Los que traiga un documento llegan a Cohere al ingerirse y al proveedor del LLM en cada prompt que use ese fragmento. **Es una excepción a la promesa de 5.2 y al diferenciador de 6.3**, y el argumento de ADR-0006 §3 —*"no se puede denunciar la fuga en la formulación del problema y producirla en la solución"*— sólo se sostiene acotado a la conversación. **Tiene que estar escrito así en el informe**, antes de que un tribunal lo encuentre leyendo los dos documentos juntos.
- **El aviso no es el consentimiento de los terceros.** Quien sube acepta en nombre del negocio, que es el responsable de la base según la Ley 25.326. Las personas que aparecen en un documento —una lista de inquilinos, por ejemplo— no consintieron nada. El aviso traslada la responsabilidad al negocio; no la hace desaparecer.
- **Aceptar se vuelve un clic.** Un aviso que se acepta en cada subida se termina aceptando sin leer, y la columna prueba que se aceptó, no que se entendió.
- **Un dato aceptado no le sirve al AIAgent para responder**, por la regla 5: si un manual trae el CBU para las señas, toda respuesta que lo cite se rechaza.
- **El prompt guarda los datos de los documentos.** `generations.prompt_rendered` va a contener los fragmentos recuperados tal como vienen, así que [ADR-0006](0006-anonimizacion-de-pii.md) §3 —*"`prompt_rendered` debe guardar lo redactado"*— deja de ser cierto para esa parte del prompt.
- **Una columna nueva** en `documents`, que se agrega editando `0001` mientras no haya una WABA real ([ADR-0033](0033-secretos-de-bandejas-nullables-y-congelamiento-de-0001.md)).
- **Dos promesas sobre datos personales**, una para cada origen del texto, que hay que explicar juntas en el informe y en el DFD del Paso 12.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Rechazar el documento que trae datos personales** | Rebota manuales y contratos legítimos, y con los patrones de hoy, casi cualquier lista de precios en pesos |
| **Redactar los cuatro tipos al ingerir**, con reglas propias para documentos | Mantenía ADR-0006 intacto, pero cambia el contenido sin que nadie lo vea y suma un juego de reglas que mantener. El grupo prefirió que la responsabilidad sea de quien carga |
| **Aceptar una vez por persona y no por subida** | No dice qué versión del aviso valía para cada documento |
| **Registrar la aceptación en `audit_logs`** | [ADR-0007](0007-alcance-del-registro-de-auditoria.md) acota esa tabla a la configuración del agente IA, y habría que enmendarlo para una sola columna |
| **Liberar al guardrail de salida para los datos aceptados** | Obliga a saber de dónde vino cada número de la respuesta, y expone a la clienta a que el AIAgent le dicte datos de otras personas |
