# ADR-0012 — Cuenta y bandejas de entrada: previsión estructural para el trabajo futuro declarado

| Campo | Valor |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 2026-08-08 |
| **Etapa** | Diseño — Paso 5 (auditoría del catálogo del DER) |
| **Resuelve** | Hueco detectado en la auditoría: nada define qué agente IA atiende una conversación nueva, ni por qué número entró |
| **Requisitos afectados** | F17, F20, F22 · sección 7.3 (alcance) |

---

## 1. Contexto

La auditoría del catálogo del DER encontró tres huecos encadenados:

1. **`conversations.ai_agent_id` es nullable y ninguna regla dice cómo se puebla.** ADR-0004 definió la política de asignación de *operadores*; el equivalente para agentes IA no existe. Con un solo agente el hueco es invisible; con dos, un mensaje entrante de un contacto nuevo no tiene forma de rutearse.
2. **`channel_configs` no se vincula con `conversations` ni con `messages`.** La tabla admite N filas —Meta permite varios números de teléfono bajo una misma WABA— pero nada indica por qué número entró cada conversación.
3. **`labels`, `quick_replies` y `operators` son catálogos globales**, sin ningún contenedor que los agrupe.

El cooperante puede tener varios números —uno de ventas, otro de soporte— y varios agentes IA por número. Eso está dentro del alcance: la sección 7.3 declara *"canal único: WhatsApp Business API"*, y varios números del mismo canal siguen siendo un solo canal.

Lo que **no** está dentro del alcance es la estructura multi-empresa. La sección 7.3 la lista bajo:

> **Excluido del scope (trabajo futuro)**
> • Canales adicionales: Instagram, canal web, email.
> • Estructura multi-empresa (multi-tenant).

El encabezado importa y conviene leerlo completo: dice **"trabajo futuro"**, no "descartado". Las dos viñetas describen la continuación prevista del proyecto, no ideas rechazadas.

## 2. Decisión

Se toman tres decisiones acopladas:

**a) `channel_configs` se renombra a `inboxes`.** Conserva exactamente los mismos campos, todos específicos de WhatsApp. No se agrega `channel_type` ni ninguna generalización. Se agrega `conversations.inbox_id`.

**b) Se agrega `inboxes.default_ai_agent_id`.** Es el agente que atiende las conversaciones nuevas que entran por esa bandeja. Nullable: una bandeja puede operar sin agente IA (modo `off`).

**c) Se agrega la entidad `accounts`, con `account_id` en las seis entidades raíz** —`contacts`, `ai_agents`, `operators`, `labels`, `quick_replies`, `inboxes`— y **se declara que el sistema opera con una única fila durante todo el proyecto.** La funcionalidad multi-empresa (alta de cuentas, aislamiento entre cuentas, permisos por cuenta) **sigue fuera de alcance** y no se implementa.

## 3. Justificación

**Sobre el renombre a `inboxes`.** No agrega estructura ni capacidad: es vocabulario. Gana por dos motivos. Primero, alinea el modelo con Chatwoot, que [00-convenciones.md C01](../00-convenciones.md) ya declara como la referencia del dominio — mostrar un modelo con `inboxes` a alguien del rubro no requiere traducción. Segundo, es el nombre que **no habría que volver a cambiar** si algún día entra otro canal: `channel_configs` describe la configuración de un canal, `inbox` describe la bandeja que el operador ve, y esa segunda noción sobrevive al cambio de canal.

**Sobre `default_ai_agent_id`.** Es el hueco más concreto de los tres: sin él, el sistema no puede responder *"¿qué agente contesta este mensaje?"*. F17 gestiona agentes en plural y la sección 8.4 contempla comparar GPT-4o, Claude y Cohere — un experimento que requiere dos agentes conviviendo.

**Sobre `accounts`, y por qué el argumento correcto no es el obvio.** El motivo **no** es que una empresa tenga varios números: eso ya lo resuelven varias filas de `inboxes`, sin ningún contenedor. `accounts` se justifica por otra razón, y conviene tenerla escrita porque es la que se sostiene ante una pregunta del tribunal:

> Agregar `account_id` después obliga a migrar las entidades raíz **y a decidir retroactivamente a qué cuenta perteneció cada fila histórica** — una decisión que, con datos reales de producción ya cargados (Fase 4), no tiene respuesta correcta.

La asimetría de costos es lo que decide: modelarlo ahora cuesta una entidad y seis columnas; modelarlo después cuesta una migración sobre datos productivos. Y el anteproyecto ya declaró que ese futuro está previsto.

**Sobre por qué `account_id` va sólo en seis entidades.** El resto hereda la pertenencia a través de su padre: un `message` pertenece a la cuenta de su `conversation`, que pertenece a la de su `contact`. Duplicar `account_id` en las 34 entidades crearía dos caminos hacia el mismo dato y, con eso, la posibilidad de que algún día no coincidan.

## 4. Consecuencias

### Positivas

- Queda resuelto el ruteo: **de dónde sale `conversations.ai_agent_id`** y por qué número entró cada conversación.
- El modelo soporta varios números de WhatsApp —soporte y ventas separados— sin cambios adicionales. Eso está dentro del alcance declarado.
- La extensión a multi-empresa o a otros canales deja de requerir una migración estructural.
- El vocabulario queda alineado con la referencia del dominio que el propio proyecto eligió.

### Negativas / costos asumidos

- **El modelo contiene una entidad que el alcance declara fuera de alcance.** Es una tensión real y hay que poder explicarla en la defensa, en estos términos: *se modela la previsión, no se implementa la funcionalidad*. Si no se declara, se lee como una inconsistencia entre el informe y el DER.
- **`accounts` con una sola fila es, durante todo el proyecto, una tabla que no hace nada.** Seis JOINs o seis columnas que siempre valen lo mismo. Se acepta a cambio de no migrar después.
- **Riesgo de que la previsión se lea como promesa.** Alguien que vea `accounts` en el DER puede asumir que el sistema es multi-empresa. Se mitiga con la regla escrita en [relaciones.md §9](../DER/relaciones.md) y con esta decisión.
- El renombre obliga a propagar `channel_configs` → `inboxes` en todo documento que ya lo mencione.
- **`default_ai_agent_id` resuelve la operación, no el experimento.** Define qué agente atiende las conversaciones nuevas de una bandeja, que es el 100% del uso real. Lo que deja abierto es cómo se reparten las conversaciones cuando conviven **varios agentes sobre la misma bandeja** — caso que sólo aparece en la comparación de proveedores de §8.4, de la que depende OE2. Es una decisión de diseño experimental y se toma en el **Paso 11**, no acá; registrada como pendiente en [README.md](README.md).

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **No modelar `accounts`; declarar el sistema mono-cuenta** | Era la opción más fiel al texto del alcance y la más barata hoy. Se descarta por la asimetría de costos: la migración posterior cae sobre datos de producción reales de la Fase 4, donde "¿a qué cuenta pertenecía esta conversación?" ya no tiene respuesta reconstruible. |
| **Modelar `accounts` y quitar la exclusión del anteproyecto** | Habría eliminado la tensión, pero cambia el alcance declarado del proyecto — y comprometería al equipo a implementar aislamiento, alta de cuentas y permisos por cuenta, que es trabajo real que no está planificado en ninguna fase. |
| **Generalizar `inboxes` con `channel_type` y tablas por canal** | Prepara Instagram y email de verdad, pero modela estructura para canales que la sección 7.3 excluye, y agrega una indirección a cada consulta a cambio de capacidad que nadie va a usar en este proyecto. El renombre solo captura el beneficio de vocabulario sin ese costo. |
| **`account_id` en las 34 entidades** | Rendimiento a cambio de integridad: dos caminos hacia el mismo dato es una inconsistencia esperando ocurrir (C05). Si la consulta de bandeja de F22/F23 lo justifica, la desnormalización se decide en el Paso 7 (modelo físico), con la medición en la mano. |
