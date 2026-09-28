import { useState, useEffect } from 'react'

// Toast Component
const ToastContainer = ({ toasts, removeToast }) => {
  return (
    <div className="toast-container">
      {toasts.map((toast) => (
        <div key={toast.id} className={`toast ${toast.type}`}>
          <div style={{ flex: 1 }}>{toast.message}</div>
          <button onClick={() => removeToast(toast.id)} style={{ background: 'none', border: 'none', color: '#fff', cursor: 'pointer' }}>
            ✕
          </button>
        </div>
      ))}
    </div>
  )
}

function App() {
  const [currentView, setCurrentView] = useState('login')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [isLoading, setIsLoading] = useState(false)
  const [toasts, setToasts] = useState([])

  const addToast = (message, type = 'warning') => {
    const id = Date.now()
    setToasts((prev) => [...prev, { id, message, type }])
    setTimeout(() => {
      removeToast(id)
    }, 5000)
  }

  const removeToast = (id) => {
    setToasts((prev) => prev.filter((t) => t.id !== id))
  }

  const handleLogin = async (e) => {
    e.preventDefault()
    setIsLoading(true)
    
    try {
      // Intentamos pegarle al backend (que puede o no estar levantado)
      const res = await fetch('/auth/login', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json'
        },
        body: JSON.stringify({ email, password })
      })
      
      if (!res.ok) {
        throw new Error('Credenciales inválidas o error de conexión')
      }
      
      const data = await res.json()
      addToast('Inicio de sesión exitoso', 'success')
      console.log('Token recibido:', data.token)
    } catch (error) {
      addToast(error.message, 'error')
    } finally {
      setIsLoading(false)
    }
  }

  const handleNotImplemented = () => {
    addToast('Característica en desarrollo o pendiente de integración con backend', 'warning')
  }

  const renderLoginForm = () => (
    <div className="form-container">
      <div className="form-header">
        <h2>Bienvenido de nuevo</h2>
        <p>Ingresa a tu cuenta para gestionar tus conversaciones</p>
      </div>

      <form onSubmit={handleLogin}>
        <div className="form-group">
          <label>Correo Electrónico</label>
          <div className="input-wrapper">
            <input 
              type="email" 
              placeholder="tu@empresa.com"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required 
            />
          </div>
        </div>

        <div className="form-group">
          <label>Contraseña</label>
          <div className="input-wrapper">
            <input 
              type={showPassword ? 'text' : 'password'} 
              placeholder="••••••••"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required 
            />
            <button 
              type="button" 
              className="toggle-password"
              onClick={() => setShowPassword(!showPassword)}
            >
              {showPassword ? (
                <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"></path><line x1="1" y1="1" x2="23" y2="23"></line></svg>
              ) : (
                <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"></path><circle cx="12" cy="12" r="3"></circle></svg>
              )}
            </button>
          </div>
          <div className="text-right">
            <button type="button" className="btn-link" onClick={() => setCurrentView('forgot')}>
              ¿Olvidaste tu contraseña?
            </button>
          </div>
        </div>

        <button type="submit" className="btn-primary" disabled={isLoading}>
          {isLoading ? <span className="spinner"></span> : 'Iniciar Sesión'}
        </button>

        <div className="separator">O</div>

        <button type="button" className="btn-secondary" onClick={handleNotImplemented}>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={{marginRight: 8}}><path d="M21 12.24c0-.82-.07-1.61-.2-2.37H12v4.49h5.05a4.34 4.34 0 0 1-1.88 2.85v2.37h3.04c1.78-1.64 2.81-4.06 2.81-6.94Z" fill="currentColor"/><path d="M12 21.5c2.53 0 4.65-.84 6.2-2.27l-3.04-2.37c-.84.56-1.92.89-3.16.89-2.43 0-4.49-1.64-5.23-3.85H3.6v2.44C5.15 19.43 8.32 21.5 12 21.5Z" fill="#34A853"/><path d="M6.77 13.9a5.41 5.41 0 0 1 0-3.8V7.66H3.6a9.5 9.5 0 0 0 0 8.68l3.17-2.44Z" fill="#FBBC05"/><path d="M12 5.38c1.38 0 2.62.48 3.59 1.41l2.69-2.69C16.63 2.54 14.51 1.5 12 1.5 8.32 1.5 5.15 3.57 3.6 6.66l3.17 2.44c.74-2.21 2.8-3.72 5.23-3.72Z" fill="#EA4335"/></svg>
          Iniciar sesión con Google/Microsoft
        </button>
      </form>
    </div>
  )

  const renderForgotForm = () => (
    <div className="form-container">
      <div className="form-header">
        <h2>Restablecer contraseña</h2>
        <p>Ingresa tu correo electrónico y te enviaremos un enlace para restablecerla</p>
      </div>

      <form onSubmit={(e) => { e.preventDefault(); handleNotImplemented(); }}>
        <div className="form-group">
          <label>Correo Electrónico</label>
          <div className="input-wrapper">
            <input 
              type="email" 
              placeholder="tu@empresa.com"
              required 
            />
          </div>
        </div>

        <button type="submit" className="btn-primary">
          Enviar enlace de recuperación
        </button>

        <div style={{ marginTop: '24px', textAlign: 'center' }}>
          <button type="button" className="btn-link" onClick={() => setCurrentView('login')}>
            ← Volver al inicio de sesión
          </button>
        </div>
      </form>
    </div>
  )

  return (
    <div className="layout">
      {/* Columna Izquierda - Branding */}
      <div className="col-brand">
        <div className="brand-content">
          <h1 className="brand-logo">SONIA</h1>
          <p className="brand-slogan">Potencia tu atención al cliente con orquestación conversacional y agentes de IA avanzados.</p>
        </div>
      </div>

      {/* Columna Derecha - Formularios */}
      <div className="col-form">
        {currentView === 'login' ? renderLoginForm() : renderForgotForm()}
      </div>

      <ToastContainer toasts={toasts} removeToast={removeToast} />
    </div>
  )
}

export default App
