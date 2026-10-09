// Piezas chicas de estado que comparten todas las vistas: el skeleton de carga y el aviso de lo que el backend no soporta.

export function Skeleton({ width = '100%', height = 14, radius = 6, circle = false, className = '' }) {
  return (
    <span
      className={`skeleton ${className}`}
      style={{ width: circle ? height : width, height, borderRadius: circle ? '50%' : radius }}
      aria-hidden="true"
    />
  )
}

export function NotIntegrated({ children = 'Funcionalidad no integrada con el backend', inline = false }) {
  const Tag = inline ? 'span' : 'p'
  return <Tag className="not-integrated">{children}</Tag>
}

export function ErrorState({ message, onRetry }) {
  return (
    <div className="error-state" role="alert">
      <p>{message}</p>
      {onRetry && (
        <button type="button" className="btn-ghost btn-sm" onClick={onRetry}>Reintentar</button>
      )}
    </div>
  )
}
