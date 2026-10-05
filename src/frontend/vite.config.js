import react from '@vitejs/plugin-react'
import { fileURLToPath } from 'node:url'
import { defineConfig, loadEnv } from 'vite'

// El .env vive en la raíz del repo, como el del núcleo y el del cerebro (docs/standar/variables-de-entorno.md).
const repoRoot = fileURLToPath(new URL('../..', import.meta.url))

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
  // Prefijo vacío: se lee sólo acá, en Node, y no llega al bundle. Ninguna variable de este archivo se expone al navegador.
  const env = loadEnv(mode, repoRoot, '')
  const coreURL = env.FRONTEND_DEV_CORE_URL || 'http://localhost:8080'

  // El núcleo no habilita CORS: en desarrollo el proxy hace que la SPA y la API compartan origen, como en producción.
  const toCore = { target: coreURL, changeOrigin: true }

  return {
    plugins: [react()],
    server: {
      proxy: {
        '/api': toCore,
        '/auth': toCore,
      },
    },
  }
})
