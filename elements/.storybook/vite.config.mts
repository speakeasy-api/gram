import { defineConfig, ViteDevServer } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { createElementsServerHandlers } from '../src/server'

const __dirname = dirname(fileURLToPath(import.meta.url))

const apiMiddlewarePlugin = () => ({
  name: 'chat-api-middleware',
  configureServer(server: ViteDevServer) {
    const handlers = createElementsServerHandlers()
    server.middlewares.use('/chat/session', (req, res) =>
      handlers.session(req, res, {
        embedOrigin: 'http://localhost:6006',
        userIdentifier: 'test',
        expiresAfter: 3600,
      })
    )
  },
})

export default defineConfig({
  plugins: [react(), tailwindcss(), apiMiddlewarePlugin()],
  resolve: {
    alias: {
      '@': resolve(__dirname, '../src'),
    },
  },
})
