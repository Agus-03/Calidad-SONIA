# ADR-0040 — La previsualización del prompt

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · revisada antes del merge, tras la revisión de diseño: se justifica la regla 2 y se declara la enmienda a ADR-0017 (k) |
| **Fecha** | 2026-09-19 |
| **Etapa** | Construcción — Fase 6 (identidad del agente IA) |
| **Resuelve** | Qué arma la previsualización de F03, con qué textos, qué muestra y qué deja escrito |
| **Requisitos afectados** | F03 · OE1 |
| **Enmienda** | [ADR-0017](0017-decomposicion-en-despliegues-y-lenguajes.md) (k) — la previsualización no se registra en `audit_logs`: el núcleo audita la configuración que cambia, no quién la previsualizó (regla 5) |
| **Relacionada con** | [03-capa-prompts](../03-capa-prompts.md) (regla 1), [ADR-0017](0017-decomposicion-en-despliegues-y-lenguajes.md), [ADR-0039](0039-componentes-del-prompt-y-su-versionado.md), [05-consultas-de-metricas](../05-consultas-de-metricas.sql) (OE1, sección NO-SQL) |

---

## 1. Contexto

F03 pide que *"el administrador ve[a] en tiempo real el prompt ensamblado a medida que edita los componentes"*, y OE1 que *"la previsualización del prompt ensamblado esté disponible en tiempo real durante la configuración"*. ADR-0017 ya fijó que el ensamblador tiene **una sola implementación, en Python**: si el núcleo armara su propia versión, OE1 mediría un texto distinto del que ve el administrador.

Quedaban tres preguntas abiertas:

1. ¿Qué textos arma: los guardados o los que están en pantalla?
2. ¿Qué muestra?
3. ¿Qué deja escrito?

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **La previsualización arma lo que está en pantalla, guardado o no.** El panel manda **todos** los componentes que muestra, no sólo el que se está editando, y el cerebro no mezcla borradores con lo guardado |
| 2 | **Muestra sólo el `system`**, con la versión de `rules/generation.yaml` que lo armó y los tipos que entraron, en orden. No muestra el sobre del mensaje del cliente, porque el sobre no sale de la configuración |
| 3 | **El cerebro arma con el mismo `assemble()` que el turno**, ordenando por `position` como lo hace la lectura del turno. Un borrador en blanco no entra al prompt, igual que en un turno |
| 4 | **Los borradores cumplen las reglas de un guardado** ([ADR-0039](0039-componentes-del-prompt-y-su-versionado.md), reglas 1 y 2): los seis tipos de F01, uno de cada tipo y posiciones distintas de 1 a 32767. El núcleo los rechaza antes de preguntarle al cerebro (400), y el cerebro los vuelve a validar (422) |
| 5 | **No escribe nada ni se audita.** No es configuración, así que no va a `audit_logs` ([ADR-0007](0007-alcance-del-registro-de-auditoria.md)). Tampoco es evidencia, así que el cerebro no tiene tabla donde escribirla (ADR-0017 (i).2) |
| 6 | **Un borrador no tiene versión, y por eso nunca se convierte en evidencia.** `version_id` es opcional sólo para los borradores, y `generate()` rechaza un componente sin versión antes de llamar al proveedor |
| 7 | **Dos rutas.** En el núcleo, `POST /api/v1/ai-agents/:id/preview`, sólo para `admin`. En el cerebro, `POST /prompts/preview`, con el token interno. Es un `POST` porque los borradores viajan en el cuerpo |
| 8 | **El núcleo comprueba que el agente IA esté a su alcance antes de preguntar**, porque el cerebro no sabe de cuentas. Un agente IA pausado se previsualiza: pausado es justo cuando el administrador lo corrige ([ADR-0039](0039-componentes-del-prompt-y-su-versionado.md), regla 8). Uno dado de baja es un 404 |
| 9 | **Timeout corto y sin reintentos.** La llamada usa `BRAIN_PREVIEW_TIMEOUT_MS` (3 s), no los 10 s del turno. Un cerebro que no responde es un 503 `preview_unavailable`, y la próxima tecla vuelve a preguntar |
| 10 | **La previsualización no escribe en el log que faltan componentes.** Esa línea la escribe sólo el turno: `assemble()` dejó de loguear, porque la previsualización lo ejecuta a medida que el administrador tipea |

## 3. Justificación

- **Regla 1.** *"A medida que edita"* es antes de guardar: previsualizar lo guardado mostraría el prompt anterior al cambio que el administrador está escribiendo. Con todos los componentes en el pedido, lo que se ve es exactamente lo que está en pantalla. Si el panel mandara sólo el editado, el cerebro tendría que completar con lo guardado, y un componente editado en otra pestaña, o borrado entre medio, haría que la previsualización no coincidiera con la pantalla.
- **Regla 2.** El sobre (el contexto de F04, lo que recupera la base de conocimiento y el mensaje del cliente) se arma en cada turno con datos que durante la configuración no existen. Mostrarlo con un mensaje de ejemplo pondría en pantalla un texto que ningún turno va a enviar, y el administrador podría leer el ejemplo como parte de lo que configura. Lo que sus ediciones cambian es el `system`, y eso es lo que se muestra.
- **Regla 3.** Es la regla de ADR-0017 llevada a esta ruta. Hay un test que compara, para los mismos componentes, el `system` de la previsualización con el de un turno.
- **Regla 4.** Si la previsualización aceptara lo que el guardado rechaza, mostraría un prompt que nunca va a existir. El núcleo valida primero para devolver un código con nombre y no gastar una llamada. El cerebro valida de nuevo porque el contrato es suyo.
- **Regla 6.** Sin esta regla, un error de código podría pasar un borrador a una generación, y `generation_components` quedaría sin versión que nombrar. La columna es `NOT NULL`, así que la base también lo rechazaría, pero recién después de haber pagado la llamada al modelo.
- **Regla 5.** ADR-0017 (k) decía que el núcleo registra en `audit_logs` a quien opera. Para la previsualización no aplica: [ADR-0007](0007-alcance-del-registro-de-auditoria.md) audita los cambios de configuración, porque son los que vuelven ininterpretable una medición de OE2, y previsualizar no cambia nada. Auditarla llenaría la tabla con una fila por tecla.
- **Regla 7.** Las rutas del cerebro quedan detrás del token interno, como `/turns` y `/models`. La del núcleo cuelga del agente IA, como sus componentes, y es sólo de `admin` porque es parte de configurarlo.
- **Regla 9.** Hay un administrador tipeando. Un reintento sólo retrasa la respuesta a una pregunta que la próxima tecla va a reemplazar.

## 4. Consecuencias

### Positivas

- F03 queda construido de punta a punta, y el smoke lo recorre con el cerebro real.
- La segunda cláusula de OE1 tiene su prueba funcional: el texto que se previsualiza y el que se envía salen de la misma función.

### Negativas / costos asumidos

- **Cada tecla es una llamada al cerebro** (o cada pausa, si el panel espera a que se deje de tipear). No hay caché, porque el pedido cambia con cada tecla.
- **Al no mostrar el sobre, la previsualización no es todo lo que recibe el modelo.** El historial y el mensaje del cliente no se ven. OE1 habla del *"prompt ensamblado"*, y lo que se arma a partir de los componentes es el `system`.
- **Las reglas de un guardado viven en tres lugares**: el servicio del núcleo, los índices de `0001` y el esquema del cerebro. Cambiarlas exige cambiar los tres.
- **Quien llame a `assemble()` fuera del turno tiene que decidir por su cuenta si avisa que faltan componentes** (regla 10): el aviso ya no viene con el ensamblador.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Previsualizar lo guardado** | No cumple *"a medida que edita"*: muestra el prompt de antes del cambio |
| **Mandar sólo el componente editado** | El cerebro mezclaría el borrador con lo guardado, y la previsualización podría no coincidir con la pantalla |
| **Mostrar también el sobre del mensaje, con un mensaje de ejemplo** | Mostraría un texto que ningún turno envía, y el ejemplo podría leerse como parte de la configuración (regla 2) |
| **Ensamblar en el núcleo** | Dos implementaciones del ensamblador: OE1 mediría un texto distinto del que ve el administrador (ADR-0017) |
| **Auditar la previsualización** | No cambia nada, así que no hay nada que auditar |
