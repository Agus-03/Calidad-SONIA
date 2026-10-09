import { createContext, useCallback, useContext, useMemo, useRef, useState } from 'react'

const ToastContext = createContext(null)

export const NOT_INTEGRATED_MESSAGE = 'Característica en desarrollo o pendiente de integración con backend'

export function ToastProvider({ children }) {
  const [toasts, setToasts] = useState([])
  const seq = useRef(0)

  const remove = useCallback((id) => setToasts((prev) => prev.filter((t) => t.id !== id)), [])

  const push = useCallback((message, type = 'warning') => {
    const id = ++seq.current
    setToasts((prev) => [...prev, { id, message, type }])
    setTimeout(() => remove(id), 5000)
  }, [remove])

  const value = useMemo(() => ({ push, addToast: push, notIntegrated: () => push(NOT_INTEGRATED_MESSAGE, 'warning') }), [push])

  return (
    <ToastContext.Provider value={value}>
      {children}
      <div className="toast-container" role="status" aria-live="polite">
        {toasts.map((toast) => (
          <div key={toast.id} className={`toast ${toast.type}`}>
            <div style={{ flex: 1 }}>{toast.message}</div>
            <button
              type="button"
              className="toast-close"
              aria-label="Cerrar notificación"
              onClick={() => remove(toast.id)}
            >
              ✕
            </button>
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  )
}

export function useToast() {
  const ctx = useContext(ToastContext)
  if (!ctx) throw new Error('useToast debe usarse dentro de <ToastProvider>')
  return ctx
}
