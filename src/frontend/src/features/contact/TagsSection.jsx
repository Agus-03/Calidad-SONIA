import { useEffect, useMemo, useRef, useState } from 'react'
import { api } from '../../lib/api.js'
import { useToast } from '../../components/Toast.jsx'
import { IconPlus, IconTag, IconX } from '../../components/icons.jsx'

// Estilo de una píldora a partir del color del catálogo: el núcleo guarda el color tal cual lo eligió el admin, así que la
// UI lo atenúa con color-mix en lugar de confiar en que tenga contraste sobre fondo oscuro.
export function chipStyle(color) {
  const c = color || '#8b949e'
  return {
    '--chip': c,
  }
}

// Sistema de etiquetas del contacto: POST/DELETE /api/v1/contacts/:id/labels, con el catálogo de GET /api/v1/labels.
export function TagsSection({ contact, onCardChange }) {
  const toast = useToast()
  const [adding, setAdding] = useState(false)
  const [catalogue, setCatalogue] = useState(null)
  const [query, setQuery] = useState('')
  const [highlight, setHighlight] = useState(0)
  const [pending, setPending] = useState(null)
  const inputRef = useRef(null)

  useEffect(() => {
    if (!adding || catalogue) return
    const ctrl = new AbortController()
    api.get('/labels?per_page=100', { signal: ctrl.signal })
      .then((page) => setCatalogue(page.items ?? []))
      .catch((err) => {
        if (err.name === 'AbortError') return
        toast.push(`No se pudo cargar el catálogo de etiquetas: ${err.message}`, 'error')
        setCatalogue([])
      })
    return () => ctrl.abort()
  }, [adding, catalogue, toast])

  useEffect(() => {
    if (adding) inputRef.current?.focus()
  }, [adding])

  const assigned = useMemo(() => new Set(contact.labels.map((l) => l.id)), [contact.labels])
  const options = useMemo(() => {
    const q = query.trim().toLowerCase()
    return (catalogue ?? []).filter((l) => !assigned.has(l.id) && l.name.toLowerCase().includes(q)).slice(0, 8)
  }, [catalogue, assigned, query])

  const close = () => {
    setAdding(false)
    setQuery('')
    setHighlight(0)
  }

  const assign = async (label) => {
    setPending(label.id)
    try {
      const card = await api.post(`/contacts/${contact.id}/labels`, { label_id: label.id })
      onCardChange(card)
      toast.push(`Etiqueta “${label.name}” añadida`, 'success')
      close()
    } catch (err) {
      toast.push(`No se pudo añadir la etiqueta: ${err.message}`, 'error')
    } finally {
      setPending(null)
    }
  }

  const unassign = async (label) => {
    setPending(label.id)
    try {
      const card = await api.del(`/contacts/${contact.id}/labels/${label.id}`)
      onCardChange(card)
    } catch (err) {
      toast.push(`No se pudo quitar la etiqueta: ${err.message}`, 'error')
    } finally {
      setPending(null)
    }
  }

  const onKeyDown = (e) => {
    if (e.key === 'ArrowDown') { e.preventDefault(); setHighlight((h) => Math.min(h + 1, options.length - 1)) }
    if (e.key === 'ArrowUp') { e.preventDefault(); setHighlight((h) => Math.max(h - 1, 0)) }
    if (e.key === 'Enter' && options[highlight]) { e.preventDefault(); assign(options[highlight]) }
    if (e.key === 'Escape') { e.stopPropagation(); close() }
  }

  return (
    <section className="cp-section" aria-labelledby="cp-tags-title">
      <div className="cp-section-head">
        <h4 id="cp-tags-title"><IconTag size={15} /> Etiquetas</h4>
        {!adding && (
          <button id="cp-add-tag" type="button" className="btn-ghost btn-sm" onClick={() => setAdding(true)}>
            <IconPlus size={14} /> Añadir etiqueta
          </button>
        )}
      </div>

      <div className="chip-list">
        {contact.labels.length === 0 && !adding && <span className="muted-sm">Sin etiquetas todavía</span>}
        {contact.labels.map((label) => (
          <span key={label.id} className={`chip ${pending === label.id ? 'is-pending' : ''}`} style={chipStyle(label.color)}>
            <span className="chip-dot" />
            {label.name}
            <button
              type="button"
              className="chip-remove"
              aria-label={`Quitar etiqueta ${label.name}`}
              disabled={pending === label.id}
              onClick={() => unassign(label)}
            >
              <IconX size={12} />
            </button>
          </span>
        ))}
      </div>

      {adding && (
        <div className="autocomplete">
          <input
            ref={inputRef}
            id="cp-tag-input"
            type="text"
            role="combobox"
            aria-expanded="true"
            aria-controls="cp-tag-options"
            aria-autocomplete="list"
            placeholder="Buscar en el catálogo…"
            value={query}
            onChange={(e) => { setQuery(e.target.value); setHighlight(0) }}
            onKeyDown={onKeyDown}
            onBlur={() => setTimeout(close, 150)}
          />
          <ul id="cp-tag-options" className="autocomplete-list" role="listbox">
            {catalogue === null && <li className="autocomplete-empty">Cargando catálogo…</li>}
            {catalogue !== null && options.length === 0 && (
              <li className="autocomplete-empty">
                {catalogue.length === 0 ? 'El catálogo está vacío: un admin crea las etiquetas' : 'Sin coincidencias'}
              </li>
            )}
            {options.map((label, i) => (
              <li
                key={label.id}
                role="option"
                aria-selected={i === highlight}
                className={i === highlight ? 'is-active' : ''}
                onMouseEnter={() => setHighlight(i)}
                onMouseDown={(e) => { e.preventDefault(); assign(label) }}
              >
                <span className="chip-dot" style={chipStyle(label.color)} />
                {label.name}
              </li>
            ))}
          </ul>
        </div>
      )}
    </section>
  )
}
