# ADR-0069 — El almacén de objetos pasa a Cloud Storage for Firebase

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · Cloud Storage for Firebase en lugar de Supabase Storage, dentro de la cuota gratuita del plan Blaze, decidido por el grupo el 2026-10-03 |
| **Fecha** | 2026-10-03 |
| **Etapa** | Construcción — Fase 12 (conocimiento) |
| **Resuelve** | Con qué proveedor se construye el puerto de almacenamiento de F06, ahora que hay que crear la cuenta |
| **Requisitos afectados** | F06 |
| **Enmienda** | [ADR-0019](0019-almacenamiento-de-objetos.md), **§2** —*"Supabase Storage"*— y las menciones a Supabase de **§3 y §4**: el proveedor es Cloud Storage for Firebase. El resto de ADR-0019 sigue en pie: el puerto, `object_key`, quién escribe y quién lee · [ADR-0064](0064-ingesta-de-documentos.md), **regla 5 y dos consecuencias** nombran a Supabase: se leen como el proveedor de este ADR |
| **Relacionada con** | [ADR-0017](0017-decomposicion-en-despliegues-y-lenguajes.md), [ADR-0019](0019-almacenamiento-de-objetos.md), [ADR-0064](0064-ingesta-de-documentos.md), [ADR-0066](0066-datos-personales-en-los-documentos.md), [ADR-0067](0067-modelo-de-embeddings-e-imagenes.md) |

---

## 1. Contexto

[ADR-0019](0019-almacenamiento-de-objetos.md) eligió Supabase Storage en el Paso 7, sin evaluar costos ni cuentas porque nada se construía todavía. Al empezar la ingesta, el grupo tiene que crear la cuenta y necesita que sea gratuita. Al revisar las opciones aparecieron datos que ADR-0019 no tenía:

- **Cloud Storage for Firebase exige el plan Blaze desde el 3 de febrero de 2026**, que pide una cuenta de facturación, aunque el uso sea cero. Dentro de la cuota gratuita —5 GB de almacenamiento en `us-east1`, `us-west1` o `us-central1`, y 100 GB de salida por mes— la factura da cero.
- **Supabase tiene un plan gratuito sin tarjeta, de 1 GB**, pero el proyecto se pausa después de una semana sin actividad.
- **Backblaze B2 da 10 GB sin tarjeta**, con la API de S3.
- **DigitalOcean Spaces no tiene plan gratuito**, y Cloudflare R2 probablemente pide tarjeta.

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **Los documentos se guardan en Cloud Storage for Firebase**, usado sólo como almacén de objetos, igual que ADR-0019 usaba Supabase: ni Firestore, ni Firebase Authentication, ni hosting |
| 2 | **El proyecto está en el plan Blaze y se usa dentro de su cuota gratuita.** El bucket vive en una de las tres regiones que la cuota cubre |
| 3 | **Sólo los servidores tocan el bucket**, con la credencial de una cuenta de servicio: el núcleo escribe y el `worker` lee, como fijó ADR-0019. **Las reglas de seguridad de Firebase niegan todo acceso desde clientes**: ni el frontend ni nadie con la configuración pública del proyecto lee un objeto |
| 4 | **El resto de ADR-0019 y de ADR-0064 no cambia**: el proveedor queda detrás de `adapters/storage/`, la clave del objeto es `knowledge-bases/<knowledge_base_id>/<document_id>`, el objeto se escribe antes que la fila y se cifra con `DOCUMENT_ENCRYPTION_KEY` antes de subirlo |

## 3. Justificación

- **Es el proveedor que el grupo puede usar ahora**, y dentro de la cuota no cuesta nada. El límite de F06 es de 100 MB por agente, muy lejos de los 5 GB.
- **No se pausa**, a diferencia de Supabase: una subida que llega después de una semana sin actividad no encuentra el almacén apagado.
- **Cambiar de proveedor sigue siendo escribir un adaptador.** Es el argumento con el que ADR-0017 creó el puerto, y este cambio es el primero que lo usa.
- **Cerrar el bucket a los clientes es lo que ya decía ADR-0019 con otro proveedor.** Firebase está pensado para que los buckets los lean aplicaciones cliente, con reglas de seguridad que deciden qué se permite, y la consola ofrece un "modo de prueba" que los abre. Con una regla abierta, la configuración pública del proyecto alcanzaría para leerlos, y sólo el cifrado de ADR-0064 los protegería.

## 4. Consecuencias

### Positivas

- La cuenta se puede crear hoy, sin esperar presupuesto.
- El almacén no se apaga por falta de uso.
- ADR-0019 y ADR-0064 se sostienen enteros salvo el nombre del proveedor.

### Negativas / costos asumidos

- **Hace falta una tarjeta en la cuenta de facturación, y nada impide que se cobre.** Si el uso pasa la cuota, Google factura. Una alerta de presupuesto avisa, pero no corta el servicio.
- **Los documentos quedan en EE. UU.**, porque la cuota sólo cubre regiones de ese país. Es una transferencia internacional (art. 12 de la Ley 25.326), como la de Voyage ([ADR-0067](0067-modelo-de-embeddings-e-imagenes.md)), y Google se suma a los proveedores cuyo contrato de tratamiento de datos hay que verificar. El aviso de [ADR-0066](0066-datos-personales-en-los-documentos.md) tiene que nombrarlo.
- **Un secreto nuevo que custodiar**: la credencial de la cuenta de servicio es un archivo JSON, y la tienen el núcleo y el `worker`.
- **Dos clientes distintos**, uno en Go para el núcleo y otro en Python para el `worker`, contra una API que no es la de S3. Cambiar de proveedor obliga a reescribir los dos adaptadores.
- **Las pruebas necesitan un doble.** El emulador de Firebase depende de herramientas de Node y Java que el stack no tiene. Cómo se dobla el almacén en el *smoke* se decide al construir la subida.
- **Se descartó la recomendación técnica.** Era Backblaze B2 con un cliente S3 genérico, que no pide tarjeta y deja el adaptador independiente del proveedor.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Supabase Storage**, lo que fijaba ADR-0019 | El proyecto se pausa después de una semana sin actividad. Las subidas son esporádicas, así que se pausaría seguido, y la ingesta fallaría hasta que alguien lo reactive a mano |
| **Backblaze B2 con un cliente S3 genérico** | Era la recomendación: sin tarjeta, 10 GB y el adaptador independiente del proveedor. El grupo eligió Firebase, que es el proveedor que puede usar ahora |
| **Cloudflare R2** | Probablemente pide tarjeta, igual que Firebase, y no es el proveedor que el grupo ya puede usar |
| **DigitalOcean Spaces** | No tiene plan gratuito: cobra USD 5 por mes desde el primer día |
