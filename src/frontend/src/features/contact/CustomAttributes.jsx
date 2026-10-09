import { useId, useState } from 'react'
import { ATTRIBUTE_LABEL, formatDate } from '../../lib/format.js'
import { IconChevron, IconLock, IconSparkles, IconUser } from '../../components/icons.jsx'

// Las cuatro columnas cifradas sólo vienen en la respuesta para supervisor y admin (ADR-0032 regla 4): si la clave no está en el
// JSON, el rol no las ve, y la UI no tiene que adivinarlo.
const SENSITIVE_FIELDS = ['dni', 'cuit', 'cuil', 'cbu']

export function CustomAttributes({ contact }) {
  const [open, setOpen] = useState(true)
  const panelId = useId()

  const sensitive = SENSITIVE_FIELDS
    .filter((k) => k in contact && contact[k])
    .map((k) => ({ key: k, value: contact[k], source: 'manual', sensitive: true }))
  const extracted = (contact.attributes ?? []).map((a) => ({ ...a, sensitive: ['dni', 'cuit_cuil', 'cbu'].includes(a.key) }))
  const rows = [...sensitive, ...extracted]

  return (
    <section className="cp-section">
      <button
        id="cp-attributes-toggle"
        type="button"
        className="accordion-trigger"
        aria-expanded={open}
        aria-controls={panelId}
        onClick={() => setOpen((o) => !o)}
      >
        <span>Atributos personalizados <span className="count-pill">{rows.length}</span></span>
        <IconChevron size={16} className={`chevron ${open ? 'is-open' : ''}`} />
      </button>

      <div id={panelId} className={`accordion-panel ${open ? 'is-open' : ''}`} hidden={!open}>
        {rows.length === 0 ? (
          <p className="muted-sm">Sin atributos registrados. Los que el agente IA extraiga de las conversaciones aparecerán acá.</p>
        ) : (
          <dl className="kv-list">
            {rows.map((row) => (
              <div key={`${row.key}-${row.created_at ?? 'manual'}`} className="kv-row">
                <dt>
                  {row.sensitive && <IconLock size={12} aria-label="Dato sensible" />}
                  {ATTRIBUTE_LABEL[row.key] ?? row.key}
                </dt>
                <dd>
                  <span className="kv-value">{row.value}</span>
                  <span className="kv-meta">
                    {row.source === 'llm' ? (
                      <span className="source-badge is-ai" title="Extraído por el agente IA">
                        <IconSparkles size={11} /> IA{row.confidence != null && ` · ${Math.round(row.confidence * 100)}%`}
                      </span>
                    ) : (
                      <span className="source-badge" title="Cargado por una persona">
                        <IconUser size={11} /> Manual
                      </span>
                    )}
                    {row.created_at && <span>{formatDate(row.created_at)}</span>}
                  </span>
                </dd>
              </div>
            ))}
          </dl>
        )}
      </div>
    </section>
  )
}
