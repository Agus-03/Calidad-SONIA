# Tecnologías de Frontend para SONIA

En base a los requerimientos de la arquitectura y los estándares definidos, la pila tecnológica para el desarrollo del frontend de SONIA se define de la siguiente manera:

## 1. Core Framework: React.js
El documento de la vista de login (`login.md`) especifica que la aplicación es una **Single Page Application (SPA) en React.js**.
React nos permite crear interfaces declarativas, mantener un estado predecible y generar componentes reutilizables a lo largo de toda la bandeja operativa, lo cual es vital para una aplicación con interacción en tiempo real (WebSockets, chat, etc.).

## 2. Herramienta de Construcción y Empaquetado: Vite
Se adopta **Vite** como el entorno de desarrollo y bundler. 
- **Rapidez**: Vite proporciona un servidor de desarrollo extremadamente rápido gracias al uso de módulos nativos de ES (ESM) y Hot Module Replacement (HMR).
- **Simplicidad**: Proporciona configuraciones predeterminadas optimizadas listas para producción.
- Es el estándar moderno de facto para iniciar SPAs en React en sustitución a Create React App (CRA).

## 3. Estilos: CSS Puro (Vanilla CSS)
Se utilizará **Vanilla CSS** para los estilos.
- **Control total y flexibilidad**: CSS puro permite construir interfaces premium, dinámicas y altamente personalizadas sin acoplarse a clases utilitarias o dependencias de terceros.
- **Estética premium**: Se aplicarán variables (CSS Custom Properties) para el sistema de diseño (tokens), animaciones fluidas (micro-animaciones, hover effects), y paletas de colores sofisticadas (dark mode, cristalismo) para asegurar una experiencia de usuario de primera clase, acorde con una plataforma CRM B2B moderna.

## 4. Gestor de Paquetes: Bun / pnpm
Queda estrictamente **prohibido el uso de `npm`**. Para la gestión de dependencias y ejecución de scripts se utilizará **Bun** (o en su defecto `pnpm`).
- Bun ofrece una velocidad de instalación drásticamente superior y una ejecución de scripts optimizada, alineándose con la necesidad de un desarrollo ágil y eficiente.

## 5. Estado y Enrutamiento
- **React Router**: Para gestionar la navegación entre las diferentes vistas de manera fluida (Login, Inbox, Configuración).
- **Estado Global**: En caso de requerirse para manejar los websockets y datos del usuario a nivel global, se utilizarán las herramientas nativas de React (Context API) o librerías ligeras estándar del ecosistema.

## 6. Integración con el Backend
- Para la comunicación con el módulo Core (Go/PostgreSQL), se utilizará `fetch` configurado para manejar JWT (en encabezados de autorización).
- Para WebSockets, se utilizarán las APIs nativas del navegador para conectar con la bandeja operativa.

## 7. Seguridad y Prevención de Supply Chain Attacks
Dado el carácter crítico de la plataforma y el manejo de datos sensibles, se aplican medidas rigurosas en el frontend para mitigar riesgos de seguridad, particularmente **Supply Chain Attacks**:

- **Fijación de Versiones (Pinning)**: Todas las dependencias en `package.json` deben tener versiones fijas (quitando los símbolos `^` o `~`), asegurando construcciones deterministas.
- **Uso de Lockfiles (`bun.lockb` / `pnpm-lock.yaml`)**: Siempre se deben commitear los lockfiles para garantizar que el árbol de dependencias instalado sea exactamente el mismo en todos los entornos, previniendo actualizaciones silenciosas o inyecciones maliciosas.
- **Auditoría de Dependencias**: Se utilizará de manera continua la auditoría de dependencias (ej. `bun audit` o `pnpm audit`) en el pipeline de CI (`.github/workflows/ci.yml`) para bloquear el despliegue si se detectan vulnerabilidades críticas o altas.
- **Minimización de Dependencias**: Se priorizará el uso de herramientas nativas (como Vanilla CSS y Fetch API) y el desarrollo in-house de componentes sobre el uso de librerías de terceros (por ej. Bootstrap, Tailwind, o librerías de componentes UI), reduciendo drásticamente la superficie de ataque.
- **Content Security Policy (CSP)**: Se configurarán las cabeceras HTTP necesarias en el servidor de despliegue para restringir de dónde pueden cargarse los scripts y estilos (evitando así XSS).
- **Subresource Integrity (SRI)**: Si eventualmente se recurre a un CDN para cargar recursos, se empleará el atributo `integrity` para validar que el contenido no haya sido alterado en origen.
