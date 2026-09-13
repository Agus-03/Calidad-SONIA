-- =============================================================================
-- SONIA — Consultas de las métricas del anteproyecto
--
-- Documento : docs/design/05-consultas-de-metricas.sql
-- Estado    : En curso — Paso 6 (validación cruzada de las métricas)
-- Fecha     : 2026-08-10
-- Base      : docs/SONIA_Anteproyecto.pdf (7.2 OE1–OE7 · Anexo A F34–F38)
--             docs/design/DER/entidades.md + relaciones.md  ← fuente de verdad
--             docs/design/02-decisiones/  (ADR-0014, ADR-0015, ADR-0016)
--
-- =============================================================================
-- 0. CÓMO SE LEE ESTE ARCHIVO
-- =============================================================================
--
-- El Paso 6 consiste en escribir la consulta SQL de cada métrica declarada en el
-- anteproyecto contra el modelo de papel, SIN EJECUTARLA. El criterio de cierre:
--
--     si una métrica no se puede escribir como consulta, el diseño no está
--     terminado.
--
-- El recorrido mostró que hay DOS causas por las que una métrica no se puede
-- escribir, con remedios opuestos (ver DER/relaciones.md §11):
--
--   · Falta un DATO       → se modela ahora. Descubrirlo con datos productivos
--                           encima es irreparable: el dato nunca se guardó.
--   · Falta una DEFINICIÓN → se registra el hallazgo y se difiere. Modelar antes
--                           de decidir es dejar que el modelo decida en silencio.
--
-- Cada consulta lleva el mismo encabezado:
--     MÉTRICA · ORIGEN (cita textual) · DEFINICIÓN OPERATIVA · SUPUESTOS
--
-- Los parámetros van como :nombre. Las ventanas temporales (:desde, :hasta) son
-- siempre timestamptz en UTC (C06).
--
-- Estado de cada consulta:
--     [OK]        escribible contra el modelo actual
--     [PARCIAL]   una parte se escribe, otra depende de una decisión abierta
--     [BLOQUEADA] falta una definición. La forma está escrita y parametrizada;
--                 el encabezado nombra la decisión que la desbloquea
--     [NO-SQL]    la métrica existe pero no es una consulta. Se declara y se
--                 explica por qué
--
-- =============================================================================
-- 1. SUPUESTOS GLOBALES
-- =============================================================================
--
-- G1. Borrado lógico (C09): toda consulta sobre entidades que lo admiten filtra
--     `deleted_at IS NULL`. `messages` no lo admite y no se filtra.
--
-- G2. Conjuntos de evaluación: NO existe una tabla de muestra. El conjunto de
--     las 200 conversaciones (OE3) y el de las 500 (OE4) se definen por la
--     EXISTENCIA de la fila en `conversation_annotations` con el
--     `annotation_type` correspondiente. Cada consulta de precisión va
--     acompañada de un control de tamaño.
--
-- G3. Moneda (C08): todo importe se agrupa por `currency`. Sumar ARS con USD
--     invalida el resultado. `sales.currency` es ARS por defecto y
--     `generations.currency` es USD: no se mezclan nunca.
--
-- G4. Zona horaria (C06, enmendada): las marcas de tiempo se almacenan en UTC.
--     Las consultas que AGRUPAN por unidad temporal convierten a
--     'America/Argentina/Cordoba' ANTES de agrupar. Bucketizar en UTC y
--     convertir después corre el pico horario tres horas.
--
-- G5. Turno: un `message_batch` cerrado y procesado (03-capa-prompts.md §0).
--
-- =============================================================================


-- #############################################################################
-- OE1 — El 100 % de los prompts incorporan todos los componentes activos   [OK]
-- #############################################################################
--
-- ORIGEN (7.2, OE1, textual):
--   "logrando que el 100% de los prompts generados incorporen todos los
--    componentes activos y que la previsualización del prompt ensamblado esté
--    disponible en tiempo real durante la configuración"
--
-- DEFINICIÓN OPERATIVA
--   Numerador  : generaciones donde el conjunto ensamblado contiene a todos los
--                componentes que estaban ACTIVOS en ese instante.
--   Denominador: todas las generaciones de la ventana.
--   Unidad     : la generación.
--
-- SUPUESTOS
--   S1. Los dos conjuntos se construyen POR CAMINOS INDEPENDIENTES. Es el punto
--       entero de ADR-0015: antes de `prompt_component_versions.active` el único
--       registro de ambos lados era `generation_components`, la consulta se
--       comparaba consigo misma y sólo podía devolver 100 %.
--   S2. "En ese instante" = `generations.created_at`. La versión vigente de cada
--       componente es la última creada en o antes de ese momento; el desempate
--       entre dos versiones del mismo instante es por `version_number`.
--   S3. Una generación de un agente sin ningún componente activo no aparece en
--       el denominador. Es correcto —no había nada que incorporar— pero conviene
--       vigilar que ese conteo sea cero (control C1 abajo).
--
-- NOTA: la segunda cláusula de OE1 —la previsualización en tiempo real— NO es
--       una consulta. Ver la sección [NO-SQL] al final.

WITH activos_al_momento AS (          -- A: lo que SE DEBÍA ensamblar
    SELECT g.id  AS generation_id,
           pc.id AS prompt_component_id
    FROM   generations g
    JOIN   prompt_components pc
           ON  pc.ai_agent_id = g.ai_agent_id
           AND pc.created_at <= g.created_at
           AND (pc.deleted_at IS NULL OR pc.deleted_at > g.created_at)
    CROSS  JOIN LATERAL (             -- la versión vigente en ese instante (S2)
        SELECT v.active
        FROM   prompt_component_versions v
        WHERE  v.prompt_component_id = pc.id
          AND  v.created_at <= g.created_at
        ORDER  BY v.created_at DESC, v.version_number DESC
        LIMIT  1
    ) vigente
    WHERE  vigente.active
      AND  g.created_at >= :desde AND g.created_at < :hasta
),
ensamblados AS (                      -- B: lo que SE ENSAMBLÓ
    SELECT gc.generation_id,
           pcv.prompt_component_id
    FROM   generation_components gc
    JOIN   prompt_component_versions pcv ON pcv.id = gc.prompt_component_version_id
),
por_generacion AS (
    SELECT a.generation_id,
           count(*) FILTER (WHERE e.prompt_component_id IS NULL) AS faltantes
    FROM   activos_al_momento a
    LEFT   JOIN ensamblados e
           ON  e.generation_id       = a.generation_id
           AND e.prompt_component_id = a.prompt_component_id
    GROUP  BY a.generation_id
)
SELECT count(*)                                                        AS generaciones,
       count(*) FILTER (WHERE faltantes = 0)                           AS completas,
       round(100.0 * count(*) FILTER (WHERE faltantes = 0) / count(*), 2) AS oe1_pct
FROM   por_generacion;


-- OE1 · diagnóstico — QUÉ componente faltó y en qué generación.
-- Es lo que la alternativa barata (un contador de componentes esperados) no
-- podía responder. Si OE1 da menos de 100 %, esta consulta dice dónde mirar.

WITH activos_al_momento AS ( /* … idéntico al bloque anterior … */
    SELECT g.id AS generation_id, pc.id AS prompt_component_id, pc.type
    FROM   generations g
    JOIN   prompt_components pc
           ON  pc.ai_agent_id = g.ai_agent_id
           AND pc.created_at <= g.created_at
           AND (pc.deleted_at IS NULL OR pc.deleted_at > g.created_at)
    CROSS  JOIN LATERAL (
        SELECT v.active FROM prompt_component_versions v
        WHERE  v.prompt_component_id = pc.id AND v.created_at <= g.created_at
        ORDER  BY v.created_at DESC, v.version_number DESC LIMIT 1
    ) vigente
    WHERE  vigente.active
),
ensamblados AS (
    SELECT gc.generation_id, pcv.prompt_component_id
    FROM   generation_components gc
    JOIN   prompt_component_versions pcv ON pcv.id = gc.prompt_component_version_id
)
SELECT a.generation_id, a.type AS componente_faltante
FROM   activos_al_momento a
LEFT   JOIN ensamblados e
       ON e.generation_id = a.generation_id AND e.prompt_component_id = a.prompt_component_id
WHERE  e.prompt_component_id IS NULL
ORDER  BY a.generation_id;


-- OE1 · control C1 — generaciones de agentes sin ningún componente activo (S3).
-- Tiene que dar cero. Si no, hay agentes mal configurados generando respuestas.

SELECT count(*) AS generaciones_sin_componentes_activos
FROM   generations g
WHERE  NOT EXISTS (
    SELECT 1 FROM prompt_components pc
    WHERE  pc.ai_agent_id = g.ai_agent_id
      AND  pc.created_at <= g.created_at
      AND  (pc.deleted_at IS NULL OR pc.deleted_at > g.created_at)
      AND  pc.active
);


-- #############################################################################
-- OE3 — Precisión del clasificador de transferencia ≥ 85 %                 [OK]
-- #############################################################################
--
-- ORIGEN (7.2, OE3, textual):
--   "logrando una precisión de clasificación de transferencia ≥ 85% sobre un
--    conjunto de prueba de 200 conversaciones"
--
-- DEFINICIÓN OPERATIVA (ADR-0014)
--   Precisión  = VP / (VP + FP)  — "de las transferencias que hice, ¿cuántas
--                estuvieron bien?". NO exactitud: con clases desbalanceadas la
--                exactitud premia al clasificador que no hace nada.
--   Unidad     = la conversación. Lo fija el propio OE3 al declarar la muestra
--                en conversaciones ("un conjunto de prueba de 200 conversaciones").
--   Numerador  : conversaciones transferidas cuyo gold NO es 'none'.
--   Denominador: conversaciones con al menos un `transfer_event`.
--
-- SUPUESTOS
--   S1. El conjunto de prueba se define por la existencia de la anotación (G2).
--   S2. Se mide SI transfirió bien, no CUÁNDO. Una transferencia en el turno 3
--       de una conversación que debía transferirse en el turno 8 cuenta como
--       acierto. El anteproyecto no declara el momento óptimo como objetivo.
--   S3. `gold_value` reutiliza los valores de `TransferSignalType` más 'none'.
--   S4. EL CONJUNTO DE 200 SE ELIGE ENTRE CONVERSACIONES EN MODO AUTÓNOMO.
--       Decisión del Paso 8: en modo sugerencia NO se escribe `transfer_events`,
--       porque la conversación ya está en manos de un operador y transferir no
--       es una acción posible. La consulta queda correcta sola —su población
--       sale de `transfer_events`—, pero la ANOTACIÓN es manual: anotar
--       conversaciones supervisadas llenaría el conjunto de casos donde la
--       transferencia era una operación vacía. La evaluación del clasificador
--       en esas conversaciones no se pierde: vive en `signal_evaluations`.

-- OE3.a — precisión gruesa: ¿había que transferir?

WITH gold AS (
    SELECT ca.conversation_id, ca.gold_value
    FROM   conversation_annotations ca
    WHERE  ca.annotation_type = 'transfer'
),
predicho AS (                          -- una fila por conversación transferida
    SELECT te.conversation_id,
           se.classifier_version,
           min(te.turn_number) AS primer_turno
    FROM   transfer_events te
    JOIN   signal_evaluations se ON se.id = te.signal_evaluation_id
    GROUP  BY te.conversation_id, se.classifier_version
)
SELECT p.classifier_version,
       count(*)                                              AS transferencias,
       count(*) FILTER (WHERE g.gold_value <> 'none')         AS aciertos,
       round(100.0 * count(*) FILTER (WHERE g.gold_value <> 'none')
             / count(*), 2)                                   AS precision_pct
FROM   predicho p
JOIN   gold g ON g.conversation_id = p.conversation_id   -- INNER: sólo el conjunto anotado
GROUP  BY p.classifier_version
ORDER  BY p.classifier_version;


-- OE3.b — precisión fina: ¿además acertó POR QUÉ?
-- Es la que le da sustancia a la "tasa de transferencia por tipo de señal" de
-- F36, que sin esto sólo cuenta disparos sin saber si estuvieron bien atribuidos.
--
-- OJO: si dos señales superan el umbral en el mismo turno hay una sola
-- transferencia. Acá se toma la PRIMERA por turno; la regla de atribución
-- definitiva está abierta (ver 02-decisiones/README.md).

WITH gold AS (
    SELECT ca.conversation_id, ca.gold_value
    FROM   conversation_annotations ca
    WHERE  ca.annotation_type = 'transfer'
),
primera_transferencia AS (
    SELECT DISTINCT ON (te.conversation_id)
           te.conversation_id, se.signal_type, se.classifier_version
    FROM   transfer_events te
    JOIN   signal_evaluations se ON se.id = te.signal_evaluation_id
    ORDER  BY te.conversation_id, te.turn_number, te.created_at
)
SELECT p.signal_type,
       count(*)                                                   AS transferencias,
       count(*) FILTER (WHERE g.gold_value = p.signal_type::text)  AS senal_correcta,
       round(100.0 * count(*) FILTER (WHERE g.gold_value = p.signal_type::text)
             / count(*), 2)                                        AS precision_fina_pct
FROM   primera_transferencia p
JOIN   gold g ON g.conversation_id = p.conversation_id
GROUP  BY p.signal_type
ORDER  BY p.signal_type;


-- OE3 · control C2 — tamaño del conjunto de prueba. Debe dar 200 (G2).

SELECT count(*) AS conversaciones_anotadas
FROM   conversation_annotations
WHERE  annotation_type = 'transfer';


-- #############################################################################
-- OE4 — Precisión del detector de frustración ≥ 85 %                  [PARCIAL]
-- #############################################################################
--
-- ORIGEN (7.2, OE4, textual):
--   "capaz de detectar señales de frustración y cambios de intención con
--    precisión ≥ 85 % sobre un conjunto de 500 conversaciones etiquetadas"
--
-- ESTADO: la mitad de FRUSTRACIÓN es escribible desde ADR-0014. La mitad de
--         CAMBIOS DE INTENCIÓN está bloqueada: los datos existen —`detected_intents`
--         permite derivar el cambio comparando bloques consecutivos— pero
--         "cambio de intención" NO ESTÁ DEFINIDO, y bajo la lectura ingenua
--         toda conversación tiene cambios y la métrica colapsa a la tasa base.
--         Decisión abierta en 02-decisiones/README.md.
--
-- DEFINICIÓN OPERATIVA (frustración)
--   Numerador  : conversaciones con detección de frustración cuyo gold es 'frustration'.
--   Denominador: conversaciones con al menos una detección por encima del umbral.
--   Unidad     : la conversación (misma que OE3).
--
-- SUPUESTOS
--   S1. "Detección" = `signal_evaluations` con `score >= threshold_used`, NO
--       `fired`. Es lo que ADR-0014 hizo posible: antes sólo existían las que
--       además dispararon la transferencia, y medir precisión sobre ésas da un
--       número que SUBE al subir el umbral, sin que el clasificador mejore.
--   S2. Se usa `score >= threshold_used` y no `fired` también porque una
--       reclasificación fuera de línea escribe filas con `fired = false`.

WITH gold AS (
    SELECT ca.conversation_id, ca.gold_value
    FROM   conversation_annotations ca
    WHERE  ca.annotation_type = 'frustration'
),
detectado AS (
    SELECT mb.conversation_id,
           se.classifier_version,
           max(se.score) AS score_max
    FROM   signal_evaluations se
    JOIN   message_batches mb ON mb.id = se.message_batch_id
    WHERE  se.signal_type = 'frustration'
      AND  se.score >= se.threshold_used            -- S1, S2
    GROUP  BY mb.conversation_id, se.classifier_version
)
SELECT d.classifier_version,
       count(*)                                                  AS detecciones,
       count(*) FILTER (WHERE g.gold_value = 'frustration')       AS aciertos,
       round(100.0 * count(*) FILTER (WHERE g.gold_value = 'frustration')
             / count(*), 2)                                       AS precision_pct
FROM   detectado d
JOIN   gold g ON g.conversation_id = d.conversation_id
GROUP  BY d.classifier_version
ORDER  BY d.classifier_version;


-- OE4 · barrido de umbral — LO QUE COMPRÓ ADR-0014.
-- Precisión para umbrales hipotéticos, SIN volver a correr las 500
-- conversaciones. Antes de persistir las detecciones que no disparaban, mover
-- `transfer_policies.threshold` obligaba a rehacer la evaluación entera.

WITH gold AS (
    SELECT ca.conversation_id, ca.gold_value
    FROM   conversation_annotations ca
    WHERE  ca.annotation_type = 'frustration'
),
umbrales AS (
    SELECT generate_series(0.50, 0.95, 0.05)::numeric AS umbral
),
detectado AS (
    SELECT u.umbral, mb.conversation_id
    FROM   umbrales u
    JOIN   signal_evaluations se
           ON se.signal_type = 'frustration' AND se.score >= u.umbral
    JOIN   message_batches mb ON mb.id = se.message_batch_id
    GROUP  BY u.umbral, mb.conversation_id
)
SELECT d.umbral,
       count(*)                                                  AS detecciones,
       round(100.0 * count(*) FILTER (WHERE g.gold_value = 'frustration')
             / count(*), 2)                                       AS precision_pct
FROM   detectado d
JOIN   gold g ON g.conversation_id = d.conversation_id
GROUP  BY d.umbral
ORDER  BY d.umbral;


-- OE4 · cambios de intención                                       [BLOQUEADA]
-- DESBLOQUEA: definir qué cuenta como "cambio de intención".
--
-- La predicción ES derivable —abajo está la derivación ingenua— pero bajo esa
-- lectura una conversación normal (consulta_zona → consulta_disponibilidad →
-- consulta_precio) tiene dos "cambios", la predicción es casi siempre verdadera
-- y la precisión colapsa a la tasa base. OE4 pone el cambio de intención al lado
-- de la frustración, como señal que amerita derivar a un humano: eso sugiere un
-- cambio DISRUPTIVO, y esa definición no existe en ningún lado.

WITH dominante AS (                    -- intención dominante de cada bloque
    SELECT DISTINCT ON (mb.id)
           mb.conversation_id, mb.id AS batch_id, mb.closed_at, di.intent_label
    FROM   message_batches mb
    JOIN   detected_intents di ON di.message_batch_id = mb.id
    ORDER  BY mb.id, di.confidence DESC
),
cambios AS (
    SELECT conversation_id, intent_label,
           lag(intent_label) OVER (PARTITION BY conversation_id ORDER BY closed_at) AS anterior
    FROM   dominante
)
SELECT conversation_id,
       bool_or(anterior IS NOT NULL AND intent_label <> anterior) AS detecto_cambio
FROM   cambios
GROUP  BY conversation_id;
-- …y acá iría el JOIN contra conversation_annotations (annotation_type='intent'),
--    en cuanto el dominio de `gold_value` para 'intent' esté declarado.


-- #############################################################################
-- OE7 — Tasa de resolución autónoma ≥ 75 %                                 [OK]
-- #############################################################################
--
-- ORIGEN (7.2, OE7, textual):
--   "Despliegue en producción real, midiendo tasa de resolución autónoma ≥ 75%
--    y consistencia de tono ≥ 85% en conversaciones reales."
--
-- ESTADO: la mitad de RESOLUCIÓN AUTÓNOMA es escribible. La de CONSISTENCIA DE
--         TONO está bloqueada por H-22 — ver la sección [BLOQUEADA] más abajo.
--
-- DEFINICIÓN OPERATIVA
--   Numerador  : conversaciones resueltas SIN transferencia, con al menos un
--                mensaje de la IA y NINGÚN mensaje de operador.
--   Denominador: conversaciones resueltas que tenían agente asignado.
--
-- SUPUESTOS
--   S1. CERRAR NO ES ATENDER. El cierre es manual (ADR-0002), así que toda
--       conversación resuelta tiene `resolved_by_operator_id`. Eso NO la vuelve
--       no-autónoma: lo que la descalifica es que un operador haya RESPONDIDO
--       (`messages.sender_type = 'operator'`), no que haya apretado "resolver".
--   S2. Las conversaciones sin agente (`ai_agent_id IS NULL`, modo `off`) quedan
--       fuera del denominador: nunca hubo IA que pudiera resolverlas.
--   S3. OE7 es el único objetivo SIN FECHA propia. La población "conversaciones
--       reales" se acota por la ventana de la Fase 4 (1/03 – 6/04/2027, Anexo D).
--       Es un anclaje derivado, no textual: va declarado, no dado por obvio.
--   S4. EL MODO SUGERENCIA TAMBIÉN DESCALIFICA (corrección del Paso 8). Una
--       sugerencia ACEPTADA (F02) produce un mensaje con `sender_type =
--       'ai_agent'` que un humano revisó ANTES de que saliera: eso no es
--       resolución autónoma. El filtro de S1 no la ve, porque el mensaje no es
--       de operador, y sin este supuesto OE7 SE INFLA. La evidencia ya existía
--       en el modelo: `generations.reviewed_by_operator_id` sólo se llena
--       cuando alguien revisó, en cualquiera de los tres desenlaces de F02
--       (aceptada, editada o rechazada).

WITH resueltas AS (
    SELECT c.id, c.ai_agent_id
    FROM   conversations c
    WHERE  c.status = 'resolved'
      AND  c.deleted_at IS NULL
      AND  c.ai_agent_id IS NOT NULL                  -- S2
      AND  c.resolved_at >= :desde AND c.resolved_at < :hasta   -- S3
),
clasificadas AS (
    SELECT r.id, r.ai_agent_id,
           EXISTS (SELECT 1 FROM transfer_events te
                   WHERE te.conversation_id = r.id)               AS hubo_transferencia,
           EXISTS (SELECT 1 FROM messages m
                   WHERE m.conversation_id = r.id
                     AND m.sender_type = 'ai_agent')              AS respondio_ia,
           EXISTS (SELECT 1 FROM messages m
                   WHERE m.conversation_id = r.id
                     AND m.sender_type = 'operator')              AS respondio_humano,  -- S1
           EXISTS (SELECT 1 FROM generations g
                   WHERE g.conversation_id = r.id
                     AND g.reviewed_by_operator_id IS NOT NULL)   AS hubo_revision      -- S4
    FROM   resueltas r
),
evaluadas AS (
    SELECT *,
           (respondio_ia
            AND NOT respondio_humano
            AND NOT hubo_transferencia
            AND NOT hubo_revision)                                AS es_autonoma
    FROM   clasificadas
)
SELECT count(*)                                                          AS resueltas,
       count(*) FILTER (WHERE es_autonoma)                               AS autonomas,
       count(*) FILTER (WHERE hubo_revision)                             AS con_revision_f02,
       round(100.0 * count(*) FILTER (WHERE es_autonoma)
             / count(*), 2)                                              AS oe7_pct
FROM   evaluadas;

-- `con_revision_f02` se devuelve a propósito: es la magnitud de la corrección.
-- Si esa columna es grande, el número de OE7 antes del Paso 8 estaba inflado
-- en esa proporción, y conviene poder decirlo en el informe con el dato al lado.


-- #############################################################################
-- F36 — Métricas del agente IA                                        [PARCIAL]
-- #############################################################################
--
-- ORIGEN (Anexo A, F36, textual):
--   "Tasa de resolución autónoma, tasa de transferencia por tipo de señal y
--    consistencia de tono (LLM-as-judge)."
--
-- Tres métricas. Las dos primeras [OK]; la tercera [BLOQUEADA] por H-22.

-- F36.a — tasa de resolución autónoma POR AGENTE.
--         Misma definición que OE7, desagregada. Usa `generations.ai_agent_id`
--         sólo indirectamente: la conversación ya lo tiene, y acá la mutabilidad
--         de `conversations.ai_agent_id` no molesta porque agrupamos el estado
--         final, no auditamos una generación puntual.

WITH resueltas AS (
    SELECT c.id, c.ai_agent_id
    FROM   conversations c
    WHERE  c.status = 'resolved' AND c.deleted_at IS NULL
      AND  c.ai_agent_id IS NOT NULL
      AND  c.resolved_at >= :desde AND c.resolved_at < :hasta
),
clasificadas AS (
    SELECT r.id, r.ai_agent_id,
           EXISTS (SELECT 1 FROM transfer_events te WHERE te.conversation_id = r.id) AS hubo_transferencia,
           EXISTS (SELECT 1 FROM messages m WHERE m.conversation_id = r.id AND m.sender_type = 'ai_agent') AS respondio_ia,
           EXISTS (SELECT 1 FROM messages m WHERE m.conversation_id = r.id AND m.sender_type = 'operator') AS respondio_humano,
           EXISTS (SELECT 1 FROM generations g WHERE g.conversation_id = r.id
                     AND g.reviewed_by_operator_id IS NOT NULL) AS hubo_revision   -- S4 de OE7
    FROM   resueltas r
)
SELECT a.name AS agente,
       count(*)                                                          AS resueltas,
       round(100.0 * count(*) FILTER (WHERE respondio_ia
                                        AND NOT respondio_humano
                                        AND NOT hubo_transferencia
                                        AND NOT hubo_revision)
             / count(*), 2)                                              AS autonoma_pct
FROM   clasificadas c
JOIN   ai_agents a ON a.id = c.ai_agent_id
GROUP  BY a.name
ORDER  BY autonoma_pct DESC;


-- F36.b — tasa de transferencia POR TIPO DE SEÑAL.
--         El tipo de señal ya no vive en `transfer_events` (ADR-0014): se
--         alcanza por join con la evidencia que causó la transferencia.

WITH base AS (
    SELECT count(*)::numeric AS total
    FROM   conversations c
    WHERE  c.deleted_at IS NULL
      AND  c.ai_agent_id IS NOT NULL
      AND  c.created_at >= :desde AND c.created_at < :hasta
)
SELECT se.signal_type,
       count(DISTINCT te.conversation_id)                                AS conversaciones,
       round(100.0 * count(DISTINCT te.conversation_id) / b.total, 2)    AS tasa_pct
FROM   transfer_events te
JOIN   signal_evaluations se ON se.id = te.signal_evaluation_id
JOIN   conversations c       ON c.id = te.conversation_id
CROSS  JOIN base b
WHERE  c.deleted_at IS NULL
  AND  c.created_at >= :desde AND c.created_at < :hasta
GROUP  BY se.signal_type, b.total
ORDER  BY conversaciones DESC;


-- #############################################################################
-- F34 — Métricas de volumen                                                [OK]
-- #############################################################################
--
-- ORIGEN (Anexo A, F34, textual):
--   "Conversaciones activas/resueltas/en espera, tiempos de respuesta y
--    distribución por canal de origen."

-- F34.a — estados.
--
-- SUPUESTO: una conversación sin resolver de un contacto dado de baja NO cuenta.
--   Queda `active` u oculta para siempre (ADR-0035, regla 1 y §4): nadie la ve
--   ni la cierra, así que contarla haría crecer "activas" y "en espera" sin
--   límite con carga que nadie puede atender. Las resueltas sí cuentan: son
--   historial, y un contacto dado de baja no se lleva sus métricas (C05).
--   CAMBIO DE DEFINICIÓN, decidido por el equipo el 2026-09-13: hasta entonces
--   la consulta contaba toda conversación no borrada.

SELECT c.status,
       count(*) AS conversaciones
FROM   conversations c
JOIN   contacts ct ON ct.id = c.contact_id
WHERE  c.deleted_at IS NULL
  AND  (ct.deleted_at IS NULL OR c.status = 'resolved')
GROUP  BY c.status
ORDER  BY conversaciones DESC;


-- F34.b — tiempos de respuesta.
--
-- SUPUESTO S1: `conversations.first_response_at` NO distingue quién respondió.
--   En modo autónomo la IA responde en segundos, así que un promedio conjunto
--   no significa nada operativamente. Se deriva de `messages` separando por
--   emisor. Es más caro, pero esto es una consulta de reporte, no el render de
--   la bandeja: el argumento de rendimiento de H-12 no aplica acá.
-- SUPUESTO S2: se reporta MEDIANA además del promedio. Los tiempos de respuesta
--   tienen cola larga y el promedio se desplaza con unos pocos casos extremos.
-- SUPUESTO S3: la primera respuesta es la primera que LE LLEGÓ al cliente, no el
--   primer intento. Un saliente `failed`, o uno que quedó sin estado porque el
--   envío nunca se confirmó, no cuenta: el cliente siguió esperando. Es la misma
--   regla que `first_response_at` (ADR-0035, regla 7). Tomar el pool es otra
--   cosa y cuenta el primer envío llegue o no, pero eso no es un tiempo de
--   respuesta.

WITH primer_saliente AS (
    SELECT DISTINCT ON (m.conversation_id)
           m.conversation_id, m.sender_type, m.created_at
    FROM   messages m
    WHERE  m.direction = 'outbound'
      AND  m.status IN ('sent', 'delivered', 'read')                        -- S3
    ORDER  BY m.conversation_id, m.created_at
)
SELECT p.sender_type,
       count(*)                                                             AS conversaciones,
       round(avg(extract(epoch FROM p.created_at - c.created_at)))::int      AS seg_promedio,
       percentile_cont(0.5) WITHIN GROUP (
           ORDER BY extract(epoch FROM p.created_at - c.created_at))::int    AS seg_mediana
FROM   primer_saliente p
JOIN   conversations c ON c.id = p.conversation_id
WHERE  c.deleted_at IS NULL
  AND  c.created_at >= :desde AND c.created_at < :hasta
GROUP  BY p.sender_type;


-- F34.c — distribución por canal de origen.
--         El origen vive en `contacts`, no en `sales` (ADR-0001): se alcanza
--         por join desde la conversación.

SELECT ct.source_channel,
       count(*) AS conversaciones
FROM   conversations c
JOIN   contacts ct ON ct.id = c.contact_id
WHERE  c.deleted_at IS NULL
  AND  c.created_at >= :desde AND c.created_at < :hasta
GROUP  BY ct.source_channel
ORDER  BY conversaciones DESC;


-- #############################################################################
-- F37 — Panel de plantillas de Meta                                        [OK]
-- #############################################################################
--
-- ORIGEN (Anexo A, F37, textual):
--   "Visualiza estado de plantillas. Para rechazadas muestra sugerencia del LLM.
--    Sin creación ni campañas."
--
-- No es una métrica sino un listado. Se incluye porque el Paso 6 recorre F34–F38.

SELECT i.display_name          AS bandeja,
       mt.status,
       count(*)                AS plantillas,
       count(*) FILTER (WHERE mt.llm_suggestion IS NOT NULL) AS con_sugerencia
FROM   meta_templates mt
JOIN   inboxes i ON i.id = mt.inbox_id
WHERE  mt.deleted_at IS NULL
GROUP  BY i.display_name, mt.status
ORDER  BY i.display_name, mt.status;


-- #############################################################################
-- F38 — Gráficas temporales                                                [OK]
-- #############################################################################
--
-- ORIGEN (Anexo A, F38, textual):
--   "Volumen de conversaciones en el tiempo, distribución horaria/semanal y
--    evolución de métricas del agente IA."
--
-- SUPUESTO CRÍTICO (G4): la conversión a hora local ocurre ANTES de agrupar.
--   C06 decía que la conversión ocurre "exclusivamente en la capa de
--   presentación", y para estas tres consultas eso es insuficiente: agrupar por
--   hora en UTC y convertir después corre el pico tres horas (Córdoba es UTC−3).
--   El bucketizado es parte del cálculo, no de la presentación.

-- F38.a — volumen por día.

SELECT date_trunc('day', c.created_at AT TIME ZONE 'America/Argentina/Cordoba') AS dia_local,
       count(*) AS conversaciones
FROM   conversations c
WHERE  c.deleted_at IS NULL
  AND  c.created_at >= :desde AND c.created_at < :hasta
GROUP  BY dia_local
ORDER  BY dia_local;


-- F38.b — distribución horaria (hora local).

SELECT extract(hour FROM c.created_at AT TIME ZONE 'America/Argentina/Cordoba')::int AS hora_local,
       count(*) AS conversaciones
FROM   conversations c
WHERE  c.deleted_at IS NULL
  AND  c.created_at >= :desde AND c.created_at < :hasta
GROUP  BY hora_local
ORDER  BY hora_local;


-- F38.c — distribución semanal (1 = lunes … 7 = domingo, hora local).

SELECT extract(isodow FROM c.created_at AT TIME ZONE 'America/Argentina/Cordoba')::int AS dia_semana,
       count(*) AS conversaciones
FROM   conversations c
WHERE  c.deleted_at IS NULL
  AND  c.created_at >= :desde AND c.created_at < :hasta
GROUP  BY dia_semana
ORDER  BY dia_semana;


-- F38.d — evolución de la resolución autónoma en el tiempo.
--         "Evolución de métricas del agente IA" = F36 agrupada por período.

WITH resueltas AS (
    SELECT c.id,
           date_trunc('week', c.resolved_at AT TIME ZONE 'America/Argentina/Cordoba') AS semana_local
    FROM   conversations c
    WHERE  c.status = 'resolved' AND c.deleted_at IS NULL
      AND  c.ai_agent_id IS NOT NULL
      AND  c.resolved_at >= :desde AND c.resolved_at < :hasta
),
clasificadas AS (
    SELECT r.id, r.semana_local,
           EXISTS (SELECT 1 FROM transfer_events te WHERE te.conversation_id = r.id) AS hubo_transferencia,
           EXISTS (SELECT 1 FROM messages m WHERE m.conversation_id = r.id AND m.sender_type = 'ai_agent') AS respondio_ia,
           EXISTS (SELECT 1 FROM messages m WHERE m.conversation_id = r.id AND m.sender_type = 'operator') AS respondio_humano
    FROM   resueltas r
)
SELECT semana_local,
       count(*) AS resueltas,
       round(100.0 * count(*) FILTER (WHERE respondio_ia
                                        AND NOT respondio_humano
                                        AND NOT hubo_transferencia)
             / count(*), 2) AS autonoma_pct
FROM   clasificadas
GROUP  BY semana_local
ORDER  BY semana_local;


-- #############################################################################
-- OE2 / OE7 (tono) / F36.c — Consistencia de tono              [BLOQUEADA]
-- #############################################################################
--
-- DESBLOQUEA: H-22 — decisión diferida al Paso 11.
--             Ver 02-decisiones/README.md, "métrica de consistencia de tono".
--
-- ORIGEN — el anteproyecto dice TRES cosas distintas:
--   · 5.3  : "consistencia de tono de marca medida por EVALUACIÓN HUMANA en más
--             del 85 % de las interacciones"
--   · OE2  : "consistencia de tono medida por LLM-AS-A-JUDGE ≥ 80%"
--   · OE7  : "consistencia de tono ≥ 85%" (no dice quién juzga)
--   8.1 y F36 dicen LLM-as-judge. El recuento es 3 a 1, pero la contradicción
--   hay que reconciliarla por escrito en el informe final igual.
--
-- QUÉ FALTA
--   (a) Quién juzga: humano · LLM-as-judge · clasificador entrenado. De eso
--       depende si `tone_evaluations` necesita `judge_type` y evaluador humano.
--   (b) Escala de `tone_score` y punto de corte. Hoy es `numeric` sin declarar.
--   (c) Unidad: respuesta o conversación. El modelo soporta las dos
--       (`generation_id` es nullable), así que NO es decisión de modelado.
--
-- QUÉ YA ESTÁ RAZONADO (no rediscutir)
--   · El número es una PROPORCIÓN, no un promedio. Único anclaje textual sobre
--     la forma del cálculo: 5.3 dice "en más del 85 % de LAS INTERACCIONES". Y
--     para algo llamado "consistencia", un promedio esconde justo lo que hay que
--     detectar: perfecto el 80 % del tiempo y pésimo el 20 % promedia igual que
--     mediocre siempre.
--   · Si el juez devuelve puntaje continuo, el corte se congela en la fila
--     (`threshold_used`), mismo argumento que `signal_evaluations`. NO se agrega
--     la columna hasta decidir el instrumento.
--   · OE2 (80 %) y OE7 (85 %) se separan por `rubric_version` + ventana: OE2
--     vence el 17/08/2026 sobre corpus controlado, OE7 mide la Fase 4.
--   · UNA CONSULTA DE TONO NUNCA MEZCLA ALCANCES. Sumar filas por respuesta con
--     filas por conversación no da ningún número interpretable. Vale sea cual
--     sea la unidad que se elija.
--
-- FORMA DE LA CONSULTA (parametrizada; :corte lo fija el Paso 11)

SELECT te.rubric_version,
       te.judge_model,
       count(*)                                                    AS evaluaciones,
       count(*) FILTER (WHERE te.tone_score >= :corte)              AS consistentes,
       round(100.0 * count(*) FILTER (WHERE te.tone_score >= :corte)
             / count(*), 2)                                         AS consistencia_pct
FROM   tone_evaluations te
WHERE  te.generation_id IS NOT NULL       -- alcance = respuesta. NUNCA mezclar
  AND  te.created_at >= :desde AND te.created_at < :hasta
GROUP  BY te.rubric_version, te.judge_model
ORDER  BY te.rubric_version;


-- #############################################################################
-- OE6 — 90 % de las consultas del dominio sin escalada          [BLOQUEADA]
-- #############################################################################
--
-- DESBLOQUEA: H-24 (b) y (c) — diferidas al Paso 11.
--             (a) ya está resuelta por ADR-0016.
--
-- ORIGEN (7.2, OE6, textual):
--   "logrando que el 90% de las consultas dentro del dominio del negocio sean
--    respondidas sin escalada al operador humano"
--
-- (a) RESUELTO — "dentro del dominio" sale del paso `triage` del pipeline.
--     Definirlo por `similarity_score` sería CIRCULAR: dejaría que el sistema
--     decidiera qué le corresponde saber y después se evaluara sólo sobre eso,
--     de modo que MEJORA su métrica justo cuando falla en recuperar. El veredicto
--     del triage se emite ANTES de recuperar y es independiente de si la
--     recuperación funcionó. Sigue siendo el sistema juzgándose: hay que
--     validarlo contra ground truth en una muestra (falta el valor en
--     `AnnotationType`).
--
-- (b) FALTA — la unidad de "consulta". Abajo se asume el BLOQUE, que es lo que
--     03-capa-prompts.md llama turno. Con generación o con intención detectada
--     el número cambia, y F11 admite varias intenciones por bloque.
--
-- (c) FALTA — qué cuenta como "escalada". ¿Cualquier transferencia, o sólo la de
--     tipo `out_of_domain`? Si un cliente pregunta algo del dominio y escala por
--     FRUSTRACIÓN, literalmente no fue "respondida sin escalada" — pero OE6 mide
--     cobertura del RAG, no paciencia del cliente. Y como la escalada mueve la
--     CONVERSACIÓN ENTERA, hay que decidir además qué pasa con las consultas
--     posteriores del mismo chat.
--
-- FORMA DE LA CONSULTA (con (b) y (c) asumidos y marcados)

WITH consultas_del_dominio AS (        -- (b): unidad = bloque
    SELECT ps.message_batch_id,
           mb.conversation_id,
           mb.closed_at
    FROM   pipeline_steps ps
    JOIN   message_batches mb ON mb.id = ps.message_batch_id
    WHERE  ps.step = 'triage'
      AND  ps.outcome = 'in_domain'    -- (a): veredicto previo a recuperar
      AND  mb.closed_at >= :desde AND mb.closed_at < :hasta
),
escaladas AS (                          -- (c): SUPUESTO — cualquier transferencia
    SELECT c.message_batch_id
    FROM   consultas_del_dominio c
    WHERE  EXISTS (
        SELECT 1 FROM transfer_events te
        WHERE te.conversation_id = c.conversation_id
    )
    -- Variante (c'): sólo escaladas por fuera de dominio. Descomentar y
    -- comentar la de arriba si el Paso 11 decide así.
    -- WHERE EXISTS (
    --     SELECT 1 FROM transfer_events te
    --     JOIN signal_evaluations se ON se.id = te.signal_evaluation_id
    --     WHERE te.conversation_id = c.conversation_id
    --       AND se.signal_type = 'out_of_domain')
)
SELECT count(*)                                                    AS consultas_del_dominio,
       count(*) - (SELECT count(*) FROM escaladas)                 AS sin_escalada,
       round(100.0 * (count(*) - (SELECT count(*) FROM escaladas))
             / count(*), 2)                                        AS oe6_pct
FROM   consultas_del_dominio;


-- OE6 · diagnóstico — separar "falló el RAG" de "falló el LLM".
-- Es lo que `retrieval_logs` y el paso `grounding` permiten distinguir, y la
-- razón por la que ADR-0005 pidió registrar la recuperación.

SELECT ps_grounding.outcome                                        AS veredicto_grounding,
       count(*)                                                    AS bloques,
       round(avg(rl.similarity_score), 3)                          AS similitud_promedio
FROM   pipeline_steps ps_triage
JOIN   message_batches mb          ON mb.id = ps_triage.message_batch_id
JOIN   pipeline_steps ps_grounding ON ps_grounding.message_batch_id = mb.id
                                  AND ps_grounding.step = 'grounding'
LEFT   JOIN generations g          ON g.message_batch_id = mb.id
LEFT   JOIN retrieval_logs rl      ON rl.generation_id = g.id
WHERE  ps_triage.step = 'triage' AND ps_triage.outcome = 'in_domain'
GROUP  BY ps_grounding.outcome;


-- #############################################################################
-- F35 / F16 — Métricas por vendedor                             [BLOQUEADA]
-- #############################################################################
--
-- DESBLOQUEA: H-25 — regla de atribución por operador. Diferida al Paso 11.
--
-- ORIGEN
--   · F35: "Conversaciones resueltas, ventas generadas, monto atribuido y tasa
--           de conversión por operador."
--   · F16: "…comparativa de rendimiento por operador." (no define "rendimiento")
--
-- NINGÚN DATO FALTA. Las cuatro reglas son calculables hoy; falta elegir UNA y
-- sostenerla. El problema no es cosmético: el NUMERADOR de la tasa de conversión
-- ya está fijo (`sales.operator_id`), así que si el denominador usa otra regla,
-- el cociente MEZCLA DOS ATRIBUCIONES DISTINTAS y no significa nada.
--
--   R1  conversations.resolved_by_operator_id  → quién cerró.
--       Defecto: el cierre es manual (ADR-0002), así que acredita al que sólo
--       apretó "resolver" en un chat que atendió la IA.
--   R2  conversations.operator_id              → último asignado.
--       Defecto: se sobrescribe; F26 lo vuelve arbitrario; y desde ADR-0037
--       toda conversación tiene vendedor, así que acredita lo que atendió la IA.
--   R3  conversation_assignments.to_operator_id → todos los que intervinieron.
--       Defecto: los porcentajes suman más de 100 %.
--   R4  messages.sender_operator_id            → quién efectivamente respondió.
--       El más fiel al trabajo hecho, el más caro de calcular.
--
-- FORMA DE LA CONSULTA — escrita con R4, que es mi recomendación. Para cambiar
-- de regla se reemplaza únicamente el CTE `atribuidas`.

WITH atribuidas AS (                   -- ← R4. Reemplazar este CTE para cambiar de regla
    SELECT DISTINCT m.sender_operator_id AS operator_id, m.conversation_id
    FROM   messages m
    JOIN   conversations c ON c.id = m.conversation_id
    WHERE  m.sender_type = 'operator'
      AND  m.sender_operator_id IS NOT NULL
      AND  c.status = 'resolved' AND c.deleted_at IS NULL
      AND  c.resolved_at >= :desde AND c.resolved_at < :hasta
),
ventas AS (                            -- G3: agrupado por moneda, siempre
    SELECT s.operator_id, s.currency,
           count(*)     AS ventas,
           sum(s.amount) AS monto
    FROM   sales s
    WHERE  s.deleted_at IS NULL
      AND  s.created_at >= :desde AND s.created_at < :hasta
    GROUP  BY s.operator_id, s.currency
)
SELECT o.name || ' ' || o.lastname                         AS operador,
       count(DISTINCT a.conversation_id)                   AS conversaciones_atribuidas,
       v.currency,
       coalesce(v.ventas, 0)                               AS ventas,
       coalesce(v.monto, 0)                                AS monto_atribuido,
       round(100.0 * coalesce(v.ventas, 0)
             / nullif(count(DISTINCT a.conversation_id), 0), 2) AS conversion_pct
FROM   atribuidas a
JOIN   operators o ON o.id = a.operator_id
LEFT   JOIN ventas v ON v.operator_id = a.operator_id
GROUP  BY o.name, o.lastname, v.currency, v.ventas, v.monto
ORDER  BY conversion_pct DESC NULLS LAST;


-- #############################################################################
-- [NO-SQL] — Métricas del anteproyecto que NO son consultas
-- #############################################################################
--
-- Se declaran acá en vez de fabricarles una consulta. Un objetivo verificado por
-- dos vías de naturaleza distinta es normal; fingir que las dos son SQL, no.
--
-- OE1 (segunda cláusula) — "que la previsualización del prompt ensamblado esté
--   disponible en tiempo real durante la configuración".
--   Es comportamiento de interfaz (F03). Ninguna consulta responde si una
--   pantalla actualiza en tiempo real, y ningún dato del modelo lo registraría
--   sin inventar telemetría que el anteproyecto no pide.
--   → Se verifica con prueba funcional de F03.
--
-- OE5 — "cobertura de pruebas unitarias ≥ 80%".
--   Es una métrica de proceso, no de datos. Sale de la herramienta de cobertura
--   (SonarQube, §8.4), no de PostgreSQL.
--   → Se verifica con el reporte de cobertura del pipeline de CI.
--
--
-- =============================================================================
-- ESTADO DEL RECORRIDO
-- =============================================================================
--
--   OE1  [OK]         · con ADR-0015 (`active` versionado + `ai_agent_id`)
--   OE2  [BLOQUEADA]  · H-22 — falta definición del instrumento y del corte
--   OE3  [OK]         · con ADR-0014 (precision, unidad = conversación)
--   OE4  [PARCIAL]    · frustración OK con ADR-0014; intención sin definir
--   OE5  [NO-SQL]     · cobertura de tests
--   OE6  [BLOQUEADA]  · H-24 (b) y (c); (a) resuelta por ADR-0016
--   OE7  [PARCIAL]    · resolución autónoma OK; tono bloqueado por H-22
--   F34  [OK]
--   F35  [BLOQUEADA]  · H-25 — regla de atribución
--   F36  [PARCIAL]    · a y b OK; consistencia de tono bloqueada
--   F37  [OK]
--   F38  [OK]         · exige AT TIME ZONE antes de agrupar (G4)
--   F16  [BLOQUEADA]  · H-25, y "rendimiento" sin definir
--
-- Ninguna consulta quedó sin escribir por falta de un DATO: los cuatro huecos de
-- ese tipo (H-18, H-20, H-21, H-23) se resolvieron modelando en este mismo paso.
-- Las cinco bloqueadas lo están por falta de una DEFINICIÓN, y cada una nombra
-- arriba la decisión que la desbloquea. Esa lista es la agenda del Paso 11.
