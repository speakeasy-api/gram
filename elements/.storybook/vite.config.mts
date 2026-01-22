import { defineConfig, loadEnv, Plugin, ViteDevServer } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { createElementsServerHandlers } from '../src/server'

const __dirname = dirname(fileURLToPath(import.meta.url))

const cspPlugin = (): Plugin => ({
  name: 'csp-headers',
  configureServer(server: ViteDevServer) {
    server.middlewares.use((req, res, next) => {
      // Strict CSP without unsafe-eval - matches production
      res.setHeader(
        'Content-Security-Policy',
        "script-src 'self' 'wasm-unsafe-eval'"
      )
      next()
    })
  },
})

const apiMiddlewarePlugin = (): Plugin => ({
  name: 'chat-api-middleware',
  configureServer(server: ViteDevServer) {
    const embedOrigin = process.env.VITE_GRAM_ELEMENTS_STORYBOOK_URL
    if (!embedOrigin) {
      this.error(
        'VITE_GRAM_ELEMENTS_STORYBOOK_URL is not defined in the environment variables.'
      )
      return
    }

    const handlers = createElementsServerHandlers()
    server.middlewares.use('/chat/session', (req, res) =>
      handlers.session(req, res, {
        embedOrigin,
        userIdentifier: 'test',
        expiresAfter: 3600,
      })
    )
  },
})

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')

  return {
    plugins: [react(), tailwindcss(), apiMiddlewarePlugin(), cspPlugin()],
    resolve: {
      alias: {
        '@': resolve(__dirname, '../src'),
      },
    },
    // define the process.env variable for the browser env and attach the .env values (used for storybook stories that rely on external provider keys, namely google)
    define: {
      __GRAM_API_URL__: JSON.stringify(process.env['GRAM_API_URL'] || ''),
      __GRAM_GIT_SHA__: JSON.stringify(process.env['GRAM_GIT_SHA'] || ''),
      'process.env': JSON.stringify(
        Object.fromEntries(
          Object.entries(env)
            .filter(([key]) => key.startsWith('VITE_'))
            .map(([key, value]) => [key.replace('VITE_', ''), value])
        )
      ),
    },
  }
})
