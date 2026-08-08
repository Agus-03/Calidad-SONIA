# Decisiones de diseño — SONIA

Registro de las decisiones de diseño en formato **ADR** (*Architecture Decision Record*): un archivo corto por decisión, con el contexto que la motivó, la decisión tomada, su justificación, sus consecuencias y las alternativas descartadas.

## Por qué se registran

En la defensa no alcanza con mostrar *qué* se hizo: hay que poder explicar *por qué* se hizo así y qué otras opciones se evaluaron. Una decisión sin registro se vuelve, seis meses después, indistinguible de un accidente.

Tiene además un efecto práctico inmediato: cuando dentro de tres meses alguien pregunte "¿por qué los recordatorios cuelgan del contacto y no de la conversación?", la respuesta está escrita y no hay que reconstruirla de memoria ni volver a discutirla.

## Convención

- **Archivo:** `NNNN-titulo-corto.md`, numeración correlativa de cuatro dígitos.
- **Título:** `# ADR-NNNN — Título`.
- **Metadatos:** tabla con estado, fecha, etapa, hallazgo que resuelve y requisitos afectados.
- **Secciones:** Contexto → Decisión → Justificación → Consecuencias → Alternativas descartadas.

Dos reglas que conviene sostener:

1. **Las consecuencias negativas se escriben.** Un ADR que sólo lista ventajas es publicidad, no una decisión — y es además el material que responde la pregunta "¿qué costo asumieron?" en la defensa.
2. **Un ADR no se edita cuando se cambia de opinión.** Se escribe uno nuevo que lo reemplace, y el viejo se marca como `Reemplazada por ADR-NNNN`. El historial de las decisiones que se revirtieron también es parte del trabajo.

---

## Decisiones registradas

| ADR | Título | Resuelve | Estado |
|---|---|---|---|
| [0001](0001-origen-del-contacto.md) | Origen del contacto en `contacts`, no en `sales` | H-13 | Aceptada |
| [0002](0002-ciclo-de-vida-conversacion-y-recordatorios.md) | Ciclo de vida de la conversación y vínculo de los recordatorios | H-14 | Aceptada |
| [0003](0003-etiquetas-catalogo-y-relaciones.md) | Etiquetas: catálogo único con relaciones a contactos y conversaciones | H-07, H-16 | Aceptada |
| [0004](0004-asignacion-de-conversaciones.md) | Asignación de conversaciones: pool con autoasignación | H-15 | Aceptada |
| [0005](0005-base-de-conocimiento.md) | Base de conocimiento: una por agente, con múltiples documentos | H-06 | Aceptada |
| [0006](0006-anonimizacion-de-pii.md) | Anonimización de PII antes del envío al LLM externo | — | Aceptada |
| [0007](0007-alcance-del-registro-de-auditoria.md) | Alcance del registro de auditoría | — | Aceptada |
| [0008](0008-ventana-de-24-horas.md) | Ventana de 24 horas de WhatsApp: incorporación al anteproyecto | — | Aceptada |
| [0009](0009-horario-de-atencion.md) | Horario de atención del agente: fuera del alcance | — | Aceptada |
| [0010](0010-qdrant-como-base-vectorial.md) | Qdrant como base vectorial, una colección por agente | — | Aceptada |
| [0011](0011-acceso-a-datos-sensibles.md) | Acceso a datos sensibles: acotado por asignación, no por rol | — | Aceptada |

Con estas once queda cerrado el **Paso 3** de la etapa de diseño. Los hallazgos restantes de la [matriz de trazabilidad](../01-trazabilidad.md) no requieren decisión: se resuelven modelando, en los Pasos 4 y 5.

---

## Decisiones pendientes

Ninguna bloquea el avance a los Pasos 4 y 5, pero conviene cerrarlas antes de terminar la etapa.

### Derivadas de las decisiones tomadas

- [ ] **Vista de historial del operador.** Con F32 restringido a `admin` ([ADR-0011](0011-acceso-a-datos-sensibles.md)) y las conversaciones divididas por cada reapertura ([ADR-0002](0002-ciclo-de-vida-conversacion-y-recordatorios.md)), el operador atiende a un cliente recurrente sin ver sus conversaciones anteriores. ¿Ve al menos el listado de conversaciones previas desde el chat asignado? **Es el pendiente de mayor impacto.**
- [ ] **Registro de desencriptado de datos sensibles.** Mejora de trazabilidad, sin urgencia una vez acotado el acceso por asignación. Ver [ADR-0007](0007-alcance-del-registro-de-auditoria.md).
- [ ] **Marcador único o por tipo para CUIT/CUIL.** Comparten formato, así que la redacción sobre texto libre no puede distinguirlos. Se resuelve al modelar, en el Paso 5. Ver [ADR-0006](0006-anonimizacion-de-pii.md).

### Heredadas de la sección 8.4 del anteproyecto

- [ ] **Frontend.** Sigue "A DEFINIR" en 8.4. Debe consumir WebSockets para el inbox en tiempo real (F21).
- [ ] **Proveedor de LLM primario y de respaldo.** Conviene fijarlo antes de medir OE2: la consistencia de tono depende del modelo.
- [ ] **Modelo de embeddings.** Condiciona la dimensionalidad de las colecciones del vector store; cambiarlo después obliga a reindexar toda la base de conocimiento.
