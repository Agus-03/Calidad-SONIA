# Documento Descriptivo de Interfaz de Usuario (UI)

## Módulo: Autenticación (Login)

**Tipo de Aplicación:** Single Page Application (SPA) - React.js
**Referencia visual:** Chatwoot

### Disposición General (Layout)

Siguiendo los estándares de plataformas CRM B2B modernas, el módulo de autenticación ocupará toda la pantalla (100vh, 100vw). En pantallas de escritorio (Desktop), se utilizará un diseño de dos columnas:

* **Columna Izquierda (Informativa/Branding):** Fondo con el color primario de la marca o una ilustración alusiva a la "Orquestación Conversacional". Puede incluir el logo de SONIA, un eslogan ("Potencia tu atención al cliente con IA") y testimonios o métricas clave.

* **Columna Derecha (Interactiva):** Contenedor con fondo blanco, centrado vertical y horizontalmente, que alojará los formularios de las distintas sub-vistas (Login, Recuperación).

* **Móvil:** La columna izquierda se oculta o se reduce a un simple logo en la parte superior, mostrando únicamente la columna derecha (formularios) ocupando el ancho total.

### REGLA GENERAL DE INTEGRACIÓN

Para todos los elementos interactivos que requieran comunicación con la API (Go/PostgreSQL), aplica la siguiente regla estricta:

> **"Solo realizar si se puede hacer con el backend, sino dejar un mensaje de falta de implementación/integración"** (Ej. mediante un Toast notification o Alerta visual que indique: *"Característica en desarrollo o pendiente de integración con backend"*).

#### Estado de soporte en el Backend (Actual)
* ✅ **Disponible:** Inicio de sesión clásico con correo y contraseña vía `POST /auth/login` (valida credenciales con bcrypt y retorna JWT de 12 hs junto a los datos del operador).
* ❌ **No disponible (SSO):** Autenticación de terceros (Google / Microsoft OAuth) no contemplada en la arquitectura actual ([ADR-0030](../design/02-decisiones/0030-autenticacion-de-operadores.md)).
* ❌ **No disponible (Recuperación de contraseña):** No existe servicio de correo/SMTP, almacenamiento de tokens temporales ni endpoints para solicitar o efectuar el restablecimiento de contraseñas (anotado como abierto en [ADR-0030](../design/02-decisiones/0030-autenticacion-de-operadores.md) §4).

---

### Sub-vista 1: Inicio de Sesión (Login) - `/login`

Es la vista por defecto al ingresar a la plataforma sin autenticación.

**Elementos de la UI:**

1. **Encabezado:** Título "Bienvenido de nuevo" y subtítulo "Ingresa a tu cuenta para gestionar tus conversaciones".

2. **Formulario Estándar:**

   * **Input Email:** Campo de texto (tipo `email`) con validación de formato.

   * **Input Contraseña:** Campo de texto (tipo `password`) con ícono de "ojo" para revelar/ocultar texto.

   * **Enlace "Forgot Password":** Texto clickeable "¿Olvidaste tu contraseña?" alineado a la derecha, debajo del input de contraseña. Redirige a `/forgot-password` *(Nota: la recuperación no está disponible en el backend; ver Sub-vista 2)*.

3. **Botón de Acción Principal (Submit):** `[DISPONIBLE EN BACKEND]` Botón ancho total con el texto "Iniciar Sesión". Envía `POST /auth/login` con `{ email, password }`. Muestra un estado de carga (spinner) mientras procesa la respuesta.

4. **Separador Visual:** Línea horizontal con el texto "O" en el medio.

5. **Autenticación de Terceros (SSO):** `[NO DISPONIBLE EN BACKEND]` Botón secundario "Iniciar sesión con Google/Microsoft". Al hacer clic, debe mostrar mensaje/Toast de falta de implementación: *"Característica en desarrollo o pendiente de integración con backend"*.

---

### Sub-vista 2: Recuperación de Contraseña - `/forgot-password` `[NO DISPONIBLE EN BACKEND]`

> **Estado Backend:** El backend no cuenta con cliente SMTP ni proveedor de correos transaccionales, ni endpoint para generar o registrar tokens de restablecimiento.

Para usuarios que han perdido su acceso.

**Elementos de la UI:**

1. **Encabezado:** Título "Restablecer contraseña" y texto descriptivo "Ingresa tu correo electrónico y te enviaremos un enlace para restablecerla".

2. **Input Correo Electrónico:** Único campo de entrada.

3. **Botón de Acción:** "Enviar enlace de recuperación". Al hacer clic, no intentará llamar a una API inexistente; debe disparar un mensaje/Toast o Alerta: *"Característica en desarrollo o pendiente de integración con backend"*.

4. **Enlace de regreso:** Botón tipo texto "← Volver al inicio de sesión" que redirige a `/login`.

---

### Sub-vista 3: Nueva Contraseña (Reset) - `/reset-password` `[NO DISPONIBLE EN BACKEND]`

> **Estado Backend:** No existen endpoints para validar tokens de un solo uso ni para actualizar contraseñas en la base de datos (`POST /auth/reset-password` no existe en el repositorio ni en el controlador de operadores).

Vista a la que el usuario llegaría haciendo clic en un enlace de correo.

**Elementos de la UI:**

1. **Encabezado:** Título "Elige una nueva contraseña".

2. **Formulario:**

   * **Input Nueva Contraseña:** Campo `password`.

   * **Input Confirmar Contraseña:** Campo `password`. (Validación en frontend de que ambos campos coincidan).

3. **Botón de Acción:** "Guardar nueva contraseña". Al interactuar, debe mostrar un mensaje/Toast o Alerta de falta de implementación: *"Característica en desarrollo o pendiente de integración con backend"*.

4. **Manejo de Errores / Vista Fallback:** Si se ingresa a la vista, indicar mediante un estado deshabilitado o banner informativo que la funcionalidad se encuentra pendiente de implementación en el backend, con enlace para regresar a `/login`.