---
description: "Estándares de herramientas y seguridad para el Frontend"
---

# Gestión de Paquetes
- **Prohibición estricta de npm:** NUNCA utilices `npm` para instalar paquetes, ejecutar comandos o inicializar proyectos (`npm install`, `npm run`, etc.).
- Utiliza **Bun** como gestor principal (ej. `bun install`, `bun dev`, `bun create`). En su defecto, puedes utilizar `pnpm` o `npx` si es estrictamente necesario, pero Bun es la prioridad.

# Seguridad (Supply Chain Attacks)
Cuando agregues tecnologías o configures el frontend, debes considerar y priorizar activamente:
- **Pinning de versiones:** Remover símbolos de versión flexible (`^`, `~`).
- **Lockfiles:** Siempre respetar y requerir la persistencia de lockfiles (`bun.lockb`).
- **Minimización de dependencias:** Evitar instalar librerías de terceros (UI, utilidades) si el problema puede resolverse con código nativo (ej. Vanilla CSS).
- Mantener siempre actualizados los documentos de arquitectura (como `technologies.md`) ante cualquier cambio en la pila tecnológica de frontend.
