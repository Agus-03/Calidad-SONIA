# ADR-0011 — Acceso a datos sensibles: acotado por asignación, no por rol

| Campo | Valor |
|---|---|
| **Estado** | **Reemplazada por [ADR-0032](0032-acceso-a-contactos-y-datos-sensibles.md)** el 2026-09-09 |
| **Fecha** | 2026-08-06 |
| **Etapa** | Diseño — Paso 3 |
| **Requisitos afectados** | F26, F30, F31, F32 · sección 7.3, Anexo B |
| **Relacionada con** | [ADR-0004](0004-asignacion-de-conversaciones.md), [ADR-0006](0006-anonimizacion-de-pii.md), [ADR-0007](0007-alcance-del-registro-de-auditoria.md) |

> **Esta decisión ya no rige.** La reemplaza [ADR-0032](0032-acceso-a-contactos-y-datos-sensibles.md), que abre el módulo de contactos a los tres roles y restringe en cambio los cuatro campos sensibles. El motivo del reemplazo está en su sección 1: **el control central de este ADR —"el módulo de contactos es exclusivo de `admin`"— no lo pide ninguna fuente**. El Anexo A no nombra roles en F30, F32 ni F33, y el Anexo B nunca menciona contactos; este mismo documento lo admite al describirlo como *"una restricción adicional"*.
>
> **El texto se conserva sin editar** —regla 2 del README: un ADR no se corrige, se reemplaza— con la tabla de la sección 2 tachada para que se vea qué decía y que dejó de valer. El punto abierto sobre la vista de historial del operador (sección 4) **queda resuelto** por la regla 6 de ADR-0032.

---

## 1. Contexto

El alcance (7.3) se compromete a *"almacenamiento encriptado para datos sensibles (DNI, CUIL, CBU) según Ley 25.326"*, y el Anexo B define tres roles con permisos distintos. [ADR-0006](0006-anonimizacion-de-pii.md) ya resolvió que esos datos **no salen** hacia el proveedor de LLM externo.

Queda la pregunta que ninguno de los dos responde: **¿quién puede verlos en claro desde la interfaz?**

## 2. Decisión

**No existe un nivel de rol específico para ver datos sensibles.** Cualquier operador que tenga acceso a un contacto puede desencriptar y ver su DNI, CUIT y CBU.

**El control de acceso es horizontal, por asignación, no vertical por rol.** Se apoya en la restricción que el Anexo B ya establece, más una restricción adicional sobre el módulo de contactos:

| ~~Rol~~ | ~~Conversaciones~~ | ~~Módulo de contactos (F30, F32, F33)~~ |
|---|---|---|
| ~~`operator`~~ | ~~Únicamente las **asignadas**~~ | ~~Sin acceso~~ |
| ~~`supervisor`~~ | ~~Todas las del equipo~~ | ~~Sin acceso~~ |
| ~~`admin`~~ | ~~Todas~~ | ~~Acceso completo~~ |

Es decir: **un vendedor ve los datos sensibles de los clientes que atiende, y de ningún otro.** El módulo de contactos —el listado navegable, el ABM y la segmentación— es exclusivo de los dueños del negocio.

## 3. Justificación

- **El aislamiento por asignación ya es el control, y es el adecuado.** El Anexo B establece que el operador básico *"no puede ver conversaciones de otros operadores"*. Esa frontera acota la exposición de forma natural: cada vendedor alcanza únicamente a sus propios clientes. Agregar encima un control por rol duplicaría el mecanismo sin reducir la exposición real.
- **Son datos operativos, no de archivo.** El operador necesita el CBU para confirmar una seña y el DNI para armar un contrato. Restringirlos por rol lo obligaría a escalar a un admin para completar una gestión normal — sobre un cliente que ya está atendiendo.
- **El módulo de contactos cierra la única vía de evasión.** Los campos cifrados viven en `contacts`, y F30/F32/F33 son un módulo separado de la bandeja: si fuera accesible a todos, un operador llegaría a los datos de cualquier cliente sin pasar por sus conversaciones asignadas, y el acotamiento no existiría en la práctica. Restringirlo a `admin` es lo que vuelve real el control descrito arriba.
- **Que `supervisor` no tenga acceso al módulo es coherente:** supervisa la atención (ve las conversaciones de todo el equipo, reasigna) pero no gestiona la cartera de clientes, que es una función del dueño del negocio.
- **El riesgo principal está afuera, no adentro.** Lo que ataca esta arquitectura es la exposición del dato **fuera del perímetro propio**: el cifrado en reposo (F31) protege ante un acceso no autorizado a la base, y la anonimización ([ADR-0006](0006-anonimizacion-de-pii.md)) ante la salida hacia el proveedor de LLM.

## 4. Consecuencias

### Positivas

- **La exposición queda acotada sin fricción operativa:** cada vendedor alcanza a sus clientes y a ninguno más, sin necesidad de pedir permisos.
- El control reutiliza una restricción que el sistema necesita de todos modos (F26, Anexo B), en vez de agregar un mecanismo paralelo.
- Menos superficie de configuración de permisos que construir y probar.

### Negativas / costos asumidos

- **La frontera es la asignación, así que se mueve con ella.** Cada reasignación (F26, [ADR-0004](0004-asignacion-de-conversaciones.md)) amplía el conjunto de personas que vieron los datos de ese contacto, y esa acumulación es invisible: nada muestra "cuántos operadores pasaron por este contacto". `conversation_assignments` deja el rastro, pero hay que ir a buscarlo.
- **`supervisor` y `admin` ven todo por definición.** Para esos dos roles no hay acotamiento, y son los que en la práctica más consultan datos.
- **No se registra quién desencriptó qué.** Con el acotamiento por asignación el riesgo baja mucho respecto de un acceso global, pero ante un incidente sobre un contacto concreto la respuesta sigue siendo "alguno de los operadores que lo tuvo asignado, en algún momento". Ver el punto abierto en [ADR-0007](0007-alcance-del-registro-de-auditoria.md).

> **Punto abierto — la vista de historial del operador.** [ADR-0002](0002-ciclo-de-vida-conversacion-y-recordatorios.md) decidió que cada mensaje entrante sobre una conversación resuelta **abre una conversación nueva**, y apoyó esa decisión en que F32 (historial unificado por contacto) le devolvería al humano la vista continua. Al quedar F32 restringido a `admin`, **el operador pierde esa vista**: atiende a un cliente recurrente sin ver sus conversaciones anteriores, que ahora son filas separadas.
>
> Hay que decidir si, desde una conversación asignada, el operador ve al menos el listado de conversaciones previas de ese contacto. Es una vista distinta del módulo de contactos —acotada al contacto que está atendiendo, sin listado navegable ni ABM— y no contradice esta decisión.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Restringir el desencriptado al rol `admin`** | Máxima protección, pero obliga a escalar operaciones cotidianas y convierte al admin en cuello de botella. Desproporcionado dado que el acotamiento por asignación ya limita la exposición. |
| **Restringirlo a `supervisor` y `admin`** | Mismo motivo, en menor grado: el operador que gestiona la seña es normalmente el que necesita el CBU del cliente que atiende. |
| **Enmascarar por defecto con revelado explícito** (mostrar `****7151` y un botón "ver") | No restringe el acceso, pero convierte cada revelado en un acto deliberado y registrable. **Es la forma natural de endurecer esto más adelante** sin bloquear a nadie, si el registro de desencriptado se incorpora. |
