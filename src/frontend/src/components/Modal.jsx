import { useEffect, useRef } from 'react'
import { IconX } from './icons.jsx'

// <dialog> nativo: trae el foco atrapado, Escape y el backdrop sin una librería de modales.
export function Modal({ open, title, subtitle, onClose, children, footer, size = 'md', id }) {
  const ref = useRef(null)

  useEffect(() => {
    const dialog = ref.current
    if (!dialog) return
    if (open && !dialog.open) dialog.showModal()
    if (!open && dialog.open) dialog.close()
  }, [open])

  return (
    <dialog
      ref={ref}
      id={id}
      className={`modal modal-${size}`}
      onClose={onClose}
      onCancel={(e) => { e.preventDefault(); onClose?.() }}
      onClick={(e) => { if (e.target === ref.current) onClose?.() }}
      aria-labelledby={id ? `${id}-title` : undefined}
    >
      {open && (
        <div className="modal-body">
          <header className="modal-header">
            <div>
              <h2 id={id ? `${id}-title` : undefined}>{title}</h2>
              {subtitle && <p>{subtitle}</p>}
            </div>
            <button type="button" className="icon-btn" aria-label="Cerrar" onClick={onClose}>
              <IconX />
            </button>
          </header>
          <div className="modal-content">{children}</div>
          {footer && <footer className="modal-footer">{footer}</footer>}
        </div>
      )}
    </dialog>
  )
}
