// Prefijos internacionales más probables para una operación argentina. Ordenados de más largo a más corto al buscar,
// para que "+1 809" (Rep. Dominicana) no caiga en "+1" (EE. UU.).
const COUNTRY_PREFIXES = [
  ['1809', 'República Dominicana', '🇩🇴'],
  ['598', 'Uruguay', '🇺🇾'],
  ['595', 'Paraguay', '🇵🇾'],
  ['593', 'Ecuador', '🇪🇨'],
  ['591', 'Bolivia', '🇧🇴'],
  ['507', 'Panamá', '🇵🇦'],
  ['506', 'Costa Rica', '🇨🇷'],
  ['502', 'Guatemala', '🇬🇹'],
  ['351', 'Portugal', '🇵🇹'],
  ['58', 'Venezuela', '🇻🇪'],
  ['57', 'Colombia', '🇨🇴'],
  ['56', 'Chile', '🇨🇱'],
  ['55', 'Brasil', '🇧🇷'],
  ['54', 'Argentina', '🇦🇷'],
  ['53', 'Cuba', '🇨🇺'],
  ['52', 'México', '🇲🇽'],
  ['51', 'Perú', '🇵🇪'],
  ['49', 'Alemania', '🇩🇪'],
  ['44', 'Reino Unido', '🇬🇧'],
  ['39', 'Italia', '🇮🇹'],
  ['34', 'España', '🇪🇸'],
  ['33', 'Francia', '🇫🇷'],
  ['1', 'Estados Unidos / Canadá', '🇺🇸'],
].sort((a, b) => b[0].length - a[0].length)

const digitsOf = (phone) => String(phone ?? '').replace(/\D/g, '')

// El núcleo guarda sólo dígitos, la forma del wa_id de Meta (contacts.normalizePhone), así que el país se deduce del prefijo.
export function countryFromPhone(phone) {
  const d = digitsOf(phone)
  const hit = COUNTRY_PREFIXES.find(([prefix]) => d.startsWith(prefix))
  return hit ? { code: hit[0], name: hit[1], flag: hit[2] } : null
}

export function formatPhone(phone) {
  const d = digitsOf(phone)
  if (!d) return ''
  const country = countryFromPhone(d)
  if (!country) return `+${d}`
  let rest = d.slice(country.code.length)
  let mobile = ''
  // Argentina: el 9 de celular va aparte, como lo escribe WhatsApp.
  if (country.code === '54' && rest.startsWith('9')) {
    mobile = '9 '
    rest = rest.slice(1)
  }
  const groups = rest.length > 8
    ? [rest.slice(0, rest.length - 8), rest.slice(-8, -4), rest.slice(-4)]
    : [rest.slice(0, -4), rest.slice(-4)]
  return `+${country.code} ${mobile}${groups.filter(Boolean).join(' ')}`
}

export function fullName(person) {
  return [person?.name, person?.lastname].filter(Boolean).join(' ').trim()
}

export function initials(person) {
  const parts = [person?.name, person?.lastname].filter(Boolean).join(' ').trim().split(/\s+/)
  const letters = parts.slice(0, 2).map((p) => p[0]?.toUpperCase() ?? '').join('')
  return letters || '?'
}

const dateTime = new Intl.DateTimeFormat('es-AR', { dateStyle: 'medium', timeStyle: 'short' })
const dateOnly = new Intl.DateTimeFormat('es-AR', { dateStyle: 'medium' })

export function formatDateTime(iso) {
  if (!iso) return ''
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? '' : dateTime.format(d)
}

export function formatDate(iso) {
  if (!iso) return ''
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? '' : dateOnly.format(d)
}

const relative = new Intl.RelativeTimeFormat('es-AR', { numeric: 'auto' })

// [tope en segundos, tamaño de la unidad en segundos, unidad]
const RELATIVE_STEPS = [
  [60, 1, 'second'],
  [3600, 60, 'minute'],
  [86400, 3600, 'hour'],
  [604800, 86400, 'day'],
  [2629800, 604800, 'week'],
  [31557600, 2629800, 'month'],
  [Infinity, 31557600, 'year'],
]

export function timeAgo(iso) {
  if (!iso) return ''
  const diff = (new Date(iso).getTime() - Date.now()) / 1000
  if (Number.isNaN(diff)) return ''
  const [, size, unit] = RELATIVE_STEPS.find(([limit]) => Math.abs(diff) < limit)
  return relative.format(Math.round(diff / size), unit)
}

// Un color de avatar estable por contacto, para que el mismo cliente se reconozca de un vistazo entre chats.
export function hueFrom(seed) {
  let h = 0
  for (const ch of String(seed ?? '')) h = (h * 31 + ch.charCodeAt(0)) % 360
  return h
}

export const CONTACT_TYPE_LABEL = { client: 'Cliente', provider: 'Proveedor', prospect: 'Prospecto' }
export const SOURCE_CHANNEL_LABEL = { meta_ads: 'Meta Ads', qr: 'Código QR', referral: 'Referido', direct: 'Directo' }
export const CONVERSATION_STATUS_LABEL = { active: 'Activa', waiting: 'En espera', resolved: 'Resuelta' }

// Las seis claves que extrae el cerebro (ADR-0047 §2.2) más las cuatro cifradas del contacto (ADR-0006).
export const ATTRIBUTE_LABEL = {
  first_name: 'Nombre',
  last_name: 'Apellido',
  email: 'Correo',
  dni: 'DNI',
  cuit_cuil: 'CUIT / CUIL',
  cbu: 'CBU',
  cuit: 'CUIT',
  cuil: 'CUIL',
}
