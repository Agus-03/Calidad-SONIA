# ADR-0032 — Acceso a contactos: la agenda la ven los tres roles, los datos sensibles no

| Campo | Valor |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 2026-09-09 |
| **Etapa** | Construcción — Fase 3 (contactos y etiquetas) |
| **Resuelve** | Quién ve la agenda de contactos, quién ve los cuatro datos sensibles y quién puede editar qué · H-32 a H-37, H-39 |
| **Requisitos afectados** | F19, F26, F30, F31, F32, F33 · Anexo B |
| **Reemplaza a** | [ADR-0011](0011-acceso-a-datos-sensibles.md) |
| **Relacionada con** | [ADR-0002](0002-ciclo-de-vida-conversacion-y-recordatorios.md), [ADR-0003](0003-etiquetas-catalogo-y-relaciones.md), [ADR-0004](0004-asignacion-de-conversaciones.md), [ADR-0031](0031-cifrado-en-reposo-de-datos-sensibles.md) |

---

## 1. Contexto

[ADR-0011](0011-acceso-a-datos-sensibles.md) decidió, en el Paso 3, que **el módulo de contactos es exclusivo de `admin`**: ni el `operator` ni el `supervisor` tenían acceso al listado, al ABM ni a la segmentación. La Fase 3 es la que construye ese módulo, así que es la primera vez que la decisión pasa de ser un párrafo a ser una tabla de rutas.

Al releerla contra el anteproyecto para escribir esas rutas, apareció el problema: **ese control no está en ninguna fuente.**

| Fuente | Qué dice sobre quién accede a los contactos |
|---|---|
| **Anexo A — F30, F32, F33** | Describen la ficha del contacto, el historial unificado y la segmentación por etiquetas. **Ninguno nombra un rol** |
| **Anexo A — F19** | *"El administrador define el catálogo de etiquetas"*. Es el **único** requisito del área que nombra un rol, y habla del catálogo, no de los contactos |
| **Anexo B — roles** | Define los tres roles y sus permisos. **Nunca menciona contactos.** Su única restricción explícita para el operador básico es sobre conversaciones: *"No puede reasignar ni ver conversaciones de otros operadores"* |
| **Sección 7.3** | Promete *"almacenamiento encriptado para datos sensibles"*. Habla de cifrado, no de roles |

El propio texto de ADR-0011 lo admite sin querer: dice apoyarse en *"la restricción que el Anexo B ya establece, **más una restricción adicional** sobre el módulo de contactos"*. Esa restricción adicional **es un requisito inventado en la etapa de diseño**, y `CLAUDE.md` lo prohíbe explícitamente: *"No inventar requisitos, entidades ni métricas que no estén en él."*

No fue un capricho: ADR-0011 §3 la justifica como *"la única vía de evasión"* del aislamiento por asignación. El razonamiento es correcto y el problema que señala es real. **Lo que estaba mal era la herramienta**: cerrar el módulo entero para proteger cuatro columnas.

Y tenía un costo que la propia ADR-0011 dejó anotado como punto abierto: al restringir F32 a `admin`, **el operador perdía el historial del cliente que está atendiendo**, que es justamente lo que [ADR-0002](0002-ciclo-de-vida-conversacion-y-recordatorios.md) había usado para justificar que cada mensaje sobre una conversación resuelta abra una conversación nueva.

## 2. Decisión

**El módulo de contactos se abre a los tres roles. Lo que se restringe son los cuatro datos sensibles y la escritura, no la agenda.**

### 2.1. Lectura

| # | Regla |
|---|---|
| 1 | **Los tres roles ven la agenda de contactos** (F30, F33): listado, búsqueda, ficha y segmentación por etiqueta |
| 2 | **El `operator` alcanza únicamente los contactos de las conversaciones que tiene asignadas.** `supervisor` y `admin` alcanzan todos |
| 3 | **"Asignado" es la asignación actual** —`conversations.operator_id`—, **no** el historial de `conversation_assignments` |
| 4 | **DNI, CUIT, CUIL y CBU: los ven `supervisor` y `admin`.** Al `operator` **no le viajan**: no es un campo tapado en la respuesta, es un campo que no sale del servidor |
| 5 | **El catálogo de etiquetas lo leen los tres roles**, porque una etiqueta que el vendedor no puede leer no le sirve para nada en la bandeja |
| 6 | **F32 —el historial unificado del contacto— filtra por rol.** El `operator` ve las conversaciones de ese contacto **asignadas a él**, no todas. Se declara acá; se construye en la Fase 14 |
| 7 | **Una conversación transferida llega con su historial completo.** No es una excepción a la regla 6: la conversación **es** suya desde la transferencia, y F26 lo pide textualmente |

### 2.2. Escritura

| # | Regla |
|---|---|
| 8 | **El `operator` edita `name` y `lastname`, y sólo de los contactos que alcanza por la regla 2.** Nada más |
| 9 | **`supervisor` y `admin` editan el resto de los campos**, incluidos los cuatro cifrados |
| 10 | **`phone_whatsapp` es inmutable después de crear, para todos los roles** |
| 11 | **Crear un contacto: `supervisor` y `admin`** |
| 12 | **Baja lógica de un contacto: sólo `admin`** |
| 13 | **Etiquetar un contacto o una conversación: los tres roles.** Crear, renombrar y borrar etiquetas del catálogo: sólo `admin` (F19) |
| 14 | **Un campo que el rol no puede escribir se rechaza con un error explícito, nunca se ignora en silencio** |

### 2.3. Lo que esta decisión no protege

**Un dato sensible que el cliente escribió en el chat lo ve cualquiera que pueda ver esa conversación.** `messages.content_text` no está cifrado ([ADR-0031](0031-cifrado-en-reposo-de-datos-sensibles.md), regla 10) y no se tapa por rol.

La regla 4 protege **la ficha del contacto**, que es el lugar donde el dato queda guardado, consultable y desvinculado de la conversación en que se dijo. No protege el mensaje: si un cliente manda su CBU por WhatsApp, el vendedor que lo atiende lo lee, y eso es lo correcto — es el que tiene que confirmar la seña.

## 3. Justificación

- **Ninguna fuente pedía cerrar el módulo, y `CLAUDE.md` prohíbe inventar requisitos.** Es el argumento principal y basta por sí solo. Un control que no está en el anteproyecto y que además rompe una funcionalidad que sí está (F32) no se sostiene.
- **El objeto a proteger son cuatro columnas, no una pantalla.** Cerrar el módulo entero era proteger el perímetro equivocado: dejaba afuera el nombre, el teléfono, las etiquetas y el historial —que el vendedor necesita— para proteger un DNI que puede tapar solo. La regla 4 apunta al dato.
- **Un campo que no viaja no se filtra.** Enmascarar en el frontend deja el valor en la respuesta HTTP, a un `F12` de distancia. Los cuatro campos se arman en un DTO aparte que al `operator` **no se le construye**: no hay nada que revelar porque no hay nada que llegue.
- **La regla 2 conserva el aislamiento que el Anexo B sí establece.** El operador básico no ve conversaciones ajenas; derivar de ahí que tampoco vea los contactos de esas conversaciones es leer la fuente, no ampliarla. La agenda del vendedor es exactamente el conjunto de gente que atiende.
- **La regla 3 —asignación actual y no historial— es lo que evita la acumulación invisible** que ADR-0011 §4 marcó como su peor costo: un conjunto que sólo crece y del que nadie sale. Con la asignación actual, el conjunto se achica cuando la conversación cambia de manos.
- **La regla 8 sale de mirar qué pasa cuando el vendedor guarda.** Un formulario que muestra el CBU tapado y hace `PUT` del objeto entero **borra el CBU del cliente** con un campo que el vendedor nunca vio. La salida no es un formulario más cuidadoso: es que el vendedor escriba menos campos de los que lee. Corrige el nombre mal escrito, que es el caso real, y no toca nada más.
- **La regla 10 protege una clave natural.** `phone_whatsapp` tiene índice único parcial y es por donde el webhook de la Fase 4 encuentra al contacto de un mensaje entrante. Editarlo no es corregir un dato: es reapuntar toda la historia de una persona a otro número.
- **La regla 11 evita un contacto huérfano.** Un `operator` que creara un contacto se quedaría con uno que no puede volver a ver, porque todavía no tiene ninguna conversación asignada con él.
- **La regla 12 es por alcance del daño.** Un contacto dado de baja desaparece de la agenda de todo el equipo, no sólo de la de quien lo borró.
- **La regla 14 es la diferencia entre un permiso y una mentira.** Ignorar en silencio un campo prohibido le dice al vendedor que guardó algo que no se guardó.
- **La regla 5 es la lectura fiel de F19.** El requisito le da al administrador **la definición** del catálogo, no su lectura. Restringir también la lectura sería el mismo error que se está corrigiendo, en escala menor.

## 4. Consecuencias

### Positivas

- F32 vuelve a existir para el operador, y con eso [ADR-0002](0002-ciclo-de-vida-conversacion-y-recordatorios.md) recupera la justificación que se le había caído: el humano vuelve a tener la vista continua del cliente recurrente.
- El diseño deja de contener un requisito inventado, que es material que no hay que defender.
- Los cuatro datos sensibles quedan **mejor** protegidos que con ADR-0011, no peor: antes el `operator` los veía desde la conversación asignada sin restricción alguna; ahora no le llegan nunca.
- El control es una sola consulta —qué contactos alcanza el rol— derivada de la regla de la bandeja, en vez de un mecanismo paralelo que hay que mantener en sincronía con ella.
- El `supervisor` deja de tener un permiso menor que el de un rol que supervisa, que era una rareza de ADR-0011 difícil de explicar.

### Negativas / costos asumidos

- **La agenda de un `operator` va a dar vacía durante toda la Fase 3.** Hoy nada asigna conversaciones: la autoasignación al primero que responde es de la Fase 5 y la reasignación manual (F26) es de la Fase 11. El código queda escrito y correcto, pero **la regla 2 sólo se puede verificar sembrando conversaciones a mano en el test de integración**. Queda anotado acá para que en la Fase 5 no aparezca como un descubrimiento.
- **Una reasignación le saca el contacto de la agenda al vendedor anterior, al instante**, aunque lo haya atendido durante semanas. Es el precio de la regla 3, y es deliberado: la alternativa —acumular por historial— es el costo que ADR-0011 ya había marcado como el peor.
- **Dos operadores pueden editar el mismo contacto y la última escritura gana, sin que nadie se entere** (H-37). No hay columna de versión ni control de concurrencia optimista. Se asume: es un problema transversal a todo el sistema y merece su propio momento, no un parche en `contacts`.
- **La población que lee datos personales se multiplica por tres y sigue sin haber rastro de lectura** (H-39). Ante un incidente, la respuesta sigue siendo *"algún `supervisor` o `admin`, en algún momento"*. Ningún requisito pide el registro, así que no se construye — pero la deuda se declara, y la salida natural está nombrada abajo.
- **`contact_attributes` no distingue sensible de inocuo** (H-36). Su `value_encrypted` es `NOT NULL`, así que todo lo que extraiga F31 se cifra, incluso *"prefiere que lo llamen a la tarde"*. Esta decisión escribe la regla sobre los cuatro campos nombrados y **no** sobre los atributos extraídos: ese criterio es de las Fases 8 y 9.
- **La regla 6 se declara hoy y se construye en la Fase 14.** Entre una cosa y la otra hay una decisión escrita sin código que la sostenga, que es exactamente lo que pasó con ADR-0011. La diferencia es que ahora está anotado.
- **Hay que decirlo en el informe.** El trabajo abre a tres roles un módulo que una decisión previa había cerrado a uno. La corrección es a favor de la fuente, y el ADR reemplazado queda visible con su tabla original tachada — pero el cambio de criterio se declara, no se disimula.

### Lo que queda abierto

| Qué | Por qué no se resuelve acá |
|---|---|
| **Enmascarar con revelado explícito y registrar el revelado** | Es la salida natural de H-39 y ya estaba nombrada en ADR-0011 §5. No la pide ningún requisito y necesita el registro de auditoría de [ADR-0007](0007-alcance-del-registro-de-auditoria.md). Sin fase asignada |
| **Si el `operator` puede reasignar sus propias conversaciones** | Lo propuso el equipo y contradice dos cosas a la vez: [ADR-0004](0004-asignacion-de-conversaciones.md) y el Anexo B literal (*"No puede reasignar"*). Se decide al empezar la **Fase 5**, con su propio ADR, porque cambia quién ve qué por vía de la regla 2 |
| **Qué ve cada rol de las ventas ajenas** | Sale gratis de la regla 2 —la venta vive dentro de una conversación—, pero `sales/` es de la **Fase 11** y ahí se escribe |
| **Concurrencia optimista en la edición** | H-37. Transversal, no de `contacts` |
| **Qué es un atributo sensible en `contact_attributes`** | H-36. Fases 8 y 9, cuando exista el clasificador de extracción |

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Sostener ADR-0011 — módulo exclusivo de `admin`** | Es el requisito inventado que motiva este ADR. Y rompe F32 para el operador, que es quien más lo necesita |
| **Abrir el módulo a `supervisor` y `admin`, dejando afuera al `operator`** | Menos malo, mismo defecto: sigue sin apoyarse en ninguna fuente y sigue dejando al vendedor sin el historial del cliente que está atendiendo |
| **Enmascarar los cuatro campos en la respuesta (`****7151`) en vez de omitirlos** | El valor real igual viaja en el JSON, o hay que enmascararlo en el servidor y entonces ya es la regla 4 con más pasos. Como **producto** el enmascarado es mejor —muestra que el dato existe—, y por eso queda como punto abierto junto con el revelado registrable; como **control de acceso**, la omisión es más fuerte |
| **Permitir que el `operator` edite todos los campos que puede leer** | Es la opción intuitiva y la que rompe: un `PUT` del objeto entero desde un formulario con campos tapados borra el CBU. Ver H-33 |
| **Matriz de permisos configurable por campo y por rol** | Es la solución general y la que corresponde a un producto maduro. Acá serían tres roles y catorce campos fijos: una pantalla de configuración, un modelo de permisos y sus pruebas, para expresar una tabla que cabe en este documento y que nadie pidió poder cambiar |
| **Acotar por el historial de asignaciones en vez de la asignación actual** | El vendedor conservaría los contactos que atendió alguna vez. Se descarta porque el conjunto **sólo crece**, y nadie mira `conversation_assignments` para saber quién quedó adentro |
