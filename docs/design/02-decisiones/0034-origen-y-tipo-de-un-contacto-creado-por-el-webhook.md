# ADR-0034 — Origen y tipo de un contacto que crea el webhook, sin leer `referral`

| Campo | Valor |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 2026-09-12 |
| **Etapa** | Construcción — Fase 4 (canal completo) |
| **Resuelve** | Qué `source_channel` y qué `type` recibe un contacto que el webhook crea solo, sin ABM de por medio |
| **Requisitos afectados** | F15, F34 · ADR-0001, ADR-0032 |

---

## 1. Contexto

La Fase 4 hace que un mensaje de un número que `contacts` no conoce cree su fila, en vez de perderse como hacía la Fase 1 (`log.Warn` y se descartaba). Esa fila necesita, igual que cualquier otra, un `source_channel` y un `type` — las dos son `NOT NULL`.

[ADR-0001](0001-origen-del-contacto.md) fija `contacts.source_channel` como **obligatorio, inmutable y fijado en el alta**, precisamente para que F34 mida la distribución de origen sobre el universo completo de contactos y no sólo sobre los que compraron. La Cloud API de Meta puede mandar, junto al primer mensaje, un objeto `referral` cuando el contacto llegó por un anuncio "Click to WhatsApp" o una publicación de Instagram o Facebook, con un `source_type` que distinguiría ese caso de un mensaje directo. **`whatsapp/payload.go` no lo parsea hoy**: el `Value` que arma la deserialización sólo lee `metadata`, `contacts` y `messages`.

Como el campo es inmutable, la primera lectura que se escribe es la que queda para siempre. Si se resuelve mal ahora, no hay un `UPDATE` que lo arregle después sin reabrir ADR-0001.

## 2. Decisión

**El alta automática fija `source_channel = direct` y `type = prospect` siempre, sin leer `referral`.** El nombre sale de `contacts[].profile.name` del payload cuando Meta lo manda, o del número en dígitos si no.

## 3. Justificación

- **Ningún requisito de F01–F39 pide hoy atribución de campaña dentro del canal de WhatsApp.** F15 pide registrar el canal de ingreso con las cuatro categorías fijas de ADR-0001; leer `referral` para distinguir `meta_ads` de `direct` sería construir una capacidad de atribución que nadie pidió — el mismo argumento con el que [ADR-0009](0009-horario-de-atencion.md) descartó el ruteo por horario.
- **`prospect` es el valor menos comprometido de los tres.** Un contacto que recién escribe no es cliente ni proveedor todavía; escribir `client` sin una fila en `sales` que lo respalde inventaría una venta que no ocurrió.
- **El costo real de la decisión cae sólo sobre `source_channel`, no sobre `type`.** [ADR-0032](0032-acceso-a-contactos-y-datos-sensibles.md) deja `type` editable por los tres roles: si el valor por defecto no encaja, un operador lo corrige en la ficha. `source_channel` no tiene ese camino porque ADR-0001 lo declaró inmutable — es la única de las dos elecciones que no se puede deshacer con un `PATCH`.

## 4. Consecuencias

### Positivas

- El alta automática no se bloquea ni se pospone por una capacidad — atribución de anuncios — que ningún requisito exige todavía.
- `type = prospect` no inventa una relación comercial que la fila no puede probar.
- Un solo camino (`direct`) es más simple de razonar y de probar que uno condicionado a qué trajo el payload.

### Negativas / costos asumidos

- **Todo contacto que llega por un anuncio de WhatsApp Click-to-Chat queda etiquetado `direct` para siempre.** Es el mismo sesgo que ADR-0001 quiso evitar en `sales`, reaparecido en el alta automática: mientras no se lea `referral`, F34 va a subestimar `meta_ads` y sobreestimar `direct` en cada contacto que llegó por el canal en vez de por el ABM manual. **Si el cooperante corre campañas de Click-to-WhatsApp, este número puede no ser chico**, y hay que decirlo así en el informe si F34 se reporta sin esta salvedad.
- **No hay `ALTER` que lo corrija después.** El día que se lea `referral`, los contactos creados antes de ese cambio quedan con el origen que se les asignó al alta — corregirlos exigiría una excepción puntual a la inmutabilidad de ADR-0001, que hoy no existe.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Leer `referral.source_type` y mapearlo a `meta_ads` cuando esté presente** | Correcto en principio, pero es parsear una segunda pieza del payload de Meta sin que ningún requisito de la Fase 4 la pida. Queda declarado acá como el camino a seguir si el cooperante confirma que usa Click-to-WhatsApp — no como trabajo de esta fase |
| **`type = client` por defecto** | Inventa una venta que `sales` no registró; rompe la regla de que la conversión se prueba con una fila, no se supone |
| **Dejar el contacto sin `source_channel` hasta que un operador lo complete a mano** | La columna es `NOT NULL` desde el Paso 5 (H-13) y ADR-0001 la fija obligatoria; volverla opcional para este caso reabre esa decisión por un caso que no la justifica |
