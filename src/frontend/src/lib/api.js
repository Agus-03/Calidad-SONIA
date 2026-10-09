import { clearSession, getToken, UNAUTHORIZED_EVENT } from './session.js'

// Rutas relativas: en desarrollo las atiende el proxy de Vite (FRONTEND_DEV_CORE_URL, ver vite.config.js) y en producción el
// mismo origen que sirve la SPA. Así el bundle no lleva ninguna URL horneada.
const API_BASE = '/api/v1'

// ApiError conserva el `code` estable del núcleo ({ error: { code, message } }) para que la UI decida por él y no por el texto.
export class ApiError extends Error {
  constructor(status, code, message, details) {
    super(message)
    this.status = status
    this.code = code
    this.details = details
  }
}

async function request(method, path, { body, signal, base = API_BASE, auth = true } = {}) {
  const headers = { Accept: 'application/json' }
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  const token = getToken()
  if (auth && token) headers.Authorization = `Bearer ${token}`

  let res
  try {
    res = await fetch(base + path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      signal,
    })
  } catch (err) {
    if (err.name === 'AbortError') throw err
    throw new ApiError(0, 'network_error', 'No se pudo conectar con el servidor')
  }

  if (res.status === 204) return null

  const data = await res.json().catch(() => null)
  if (!res.ok) {
    if (res.status === 401 && auth) {
      clearSession()
      window.dispatchEvent(new Event(UNAUTHORIZED_EVENT))
    }
    const e = data?.error ?? {}
    throw new ApiError(res.status, e.code ?? 'http_error', e.message ?? `Error ${res.status}`, e.details)
  }
  return data
}

export const api = {
  get: (path, opts) => request('GET', path, opts),
  post: (path, body, opts) => request('POST', path, { ...opts, body }),
  patch: (path, body, opts) => request('PATCH', path, { ...opts, body }),
  put: (path, body, opts) => request('PUT', path, { ...opts, body }),
  del: (path, opts) => request('DELETE', path, opts),
}

export function login(email, password) {
  return request('POST', '/auth/login', { base: '', auth: false, body: { email, password } })
}
