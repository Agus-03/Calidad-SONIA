# ADR-0007 — Alcance del registro de auditoría

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · §2 ampliado por [ADR-0038](0038-alta-baja-y-pausa-del-agente-ia.md), regla 13: las modificaciones de configuración de agentes IA incluyen qué agente IA atiende cada número · §2 acotado por [ADR-0039](0039-componentes-del-prompt-y-su-versionado.md), regla 5: los cambios de los componentes del prompt se auditan en `prompt_component_versions`, no en `audit_logs` · **§2 ampliado por [ADR-0062](0062-gestion-de-operadores.md), regla 11 (2026-10-02)**: los cambios de operadores —alta, edición, rol, `active`, contraseña y baja— también se auditan |
| **Fecha** | 2026-08-06 |
| **Etapa** | Diseño — Paso 3 |
| **Requisitos afectados** | F26, OE2 · secciones 7.3 y Anexo B |
| **Relacionada con** | [ADR-0004](0004-asignacion-de-conversaciones.md), [ADR-0006](0006-anonimizacion-de-pii.md) |

---

## 1. Contexto

El alcance (7.3) se compromete a *"consentimiento explícito y almacenamiento encriptado para datos sensibles (DNI, CUIL, CBU) según Ley 25.326"*, y el Anexo B define tres roles con permisos distintos. El Anexo C **no tiene ninguna entidad de auditoría**.

En la práctica, la Ley 25.326 espera que se pueda **demostrar** quién accedió a datos personales: no alcanza con cifrarlos. Un rol `admin` que puede ver todo, sin registro de qué vio, es cifrado sin trazabilidad.

Pero el extremo opuesto tampoco sirve: registrar todas las operaciones de escritura es costo de almacenamiento y de desarrollo que después nadie consulta.

## 2. Decisión

Existe un registro de auditoría, **acotado a dos clases de evento**:

1. **Modificaciones de configuración de agentes IA** — quién, qué cambió y cuándo.
2. **Asignaciones y reasignaciones de conversaciones.**
3. *(agregada por [ADR-0062](0062-gestion-de-operadores.md))* **Cambios de operadores**: alta, edición, rol, `active`, contraseña y baja, sin el hash.

La segunda ya queda cubierta por `conversation_assignments` ([ADR-0004](0004-asignacion-de-conversaciones.md)), que registra origen, destino, autor, motivo y fecha. **No se duplica** en la tabla de auditoría: el registro de auditoría propiamente dicho cubre la primera clase.

## 3. Justificación

- **La primera protege OE2.** Si alguien edita el componente de tono a mitad del experimento, sin registro las mediciones anteriores dejan de ser interpretables y no hay forma de saber cuándo cambió ni quién lo hizo. La consistencia de tono ≥ 80 % deja de ser defendible.
- **La segunda es un requisito operativo antes que legal** — F26 pide preservar el historial de reasignaciones— y ya tiene su entidad.
- **Acotarlo es deliberado.** Un registro exhaustivo que nadie consulta es costo sin beneficio, y en la defensa no suma nada que no sume un registro bien elegido.

## 4. Consecuencias

### Positivas

- OE2 gana trazabilidad sobre los cambios de configuración: se puede responder "contra qué configuración se midió".
- F26 queda respaldado sin agregar una tabla extra.
- El costo de almacenamiento es bajo y previsible: los agentes se configuran pocas veces.

### Negativas / costos asumidos

- **Un registro acotado no cubre todo lo que un auditor externo podría pedir.** Se asume conscientemente; ampliarlo después es agregar filas, no rehacer el modelo.
- **Punto abierto: no se registra quién desencripta un dato sensible.** [ADR-0011](0011-acceso-a-datos-sensibles.md) resolvió que el acceso se acota **por asignación** (cada operador alcanza sólo a los contactos de sus conversaciones), lo que ya limita la exposición de forma razonable. Con ese acotamiento, el registro deja de ser imprescindible y pasa a ser una mejora de trazabilidad: permitiría responder *qué* operador de los que tuvieron el contacto asignado vio el dato, y cuándo. Queda como decisión abierta, sin urgencia.
- El diagrama de clases **todavía no está elaborado**, así que este alcance no tiene que reconciliarse con ninguna promesa previa: el diagrama se derivará del modelo de datos lógico (Paso 5) y reflejará esta decisión, no al revés.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Auditoría completa** (toda operación de escritura sobre toda entidad) | Costo de desarrollo y almacenamiento desproporcionado frente al uso real. La tabla más grande del sistema pasaría a ser una que nadie lee. |
| **Sin auditoría** | Deja el compromiso de 7.3 sin respaldo demostrable y a OE2 sin trazabilidad sobre los cambios de configuración. |
| **Auditoría sólo de datos sensibles** | Cubre lo legal pero deja OE2 sin protección, que es donde la falta de registro tiene consecuencia metodológica directa. |
