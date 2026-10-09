// Sesión del operador. sessionStorage y no localStorage: el JWT muere con la pestaña, que es lo más corto que se puede
// tener sin pedir login en cada recarga (ADR-0030 fija el TTL del lado del núcleo en JWT_TTL_MS).
const TOKEN_KEY = 'sonia.token'
const OPERATOR_KEY = 'sonia.operator'

export const UNAUTHORIZED_EVENT = 'sonia:unauthorized'

export function saveSession({ token, operator }) {
  sessionStorage.setItem(TOKEN_KEY, token)
  sessionStorage.setItem(OPERATOR_KEY, JSON.stringify(operator ?? null))
}

export function getToken() {
  return sessionStorage.getItem(TOKEN_KEY)
}

export function getOperator() {
  try {
    return JSON.parse(sessionStorage.getItem(OPERATOR_KEY) ?? 'null')
  } catch {
    return null
  }
}

export function clearSession() {
  sessionStorage.removeItem(TOKEN_KEY)
  sessionStorage.removeItem(OPERATOR_KEY)
}

// Los dos roles que el núcleo deja escribir todos los campos del contacto y leer los sensibles (ADR-0032, reglas 4 y 9).
export function isSupervisorOrAdmin(operator) {
  return operator?.role === 'supervisor' || operator?.role === 'admin'
}
