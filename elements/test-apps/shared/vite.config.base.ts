import http from 'node:http'
import reactPlugin from '@vitejs/plugin-react'
import { createRequire } from 'node:module'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig, type Plugin } from 'vite'

const __dirname = dirname(fileURLToPath(import.meta.url))
const shimPath = resolve(__dirname, 'react-shim.ts')

/**
 * Vite plugin that starts the session server alongside the dev server.
 * Adds connect middleware to proxy /chat/session to the session server.
 */
function sessionServerPlugin(label: string): Plugin {
  let sessionServer: http.Server | null = null
  let sessionPort = 0
  return {
    name: 'session-server',
    async configureServer(viteServer) {
      const { startTestServer } = await import('./server.base')
      sessionPort = await new Promise<number>((res) => {
        const s = startTestServer(0, label)
        s.once('listening', () => {
          const addr = s.address()
          res(typeof addr === 'object' && addr ? addr.port : 0)
        })
        sessionServer = s
      })

      // Register before internal middlewares — POST won't conflict with Vite.
      viteServer.middlewares.use((req, res, next) => {
        if (req.url !== '/chat/session' || req.method !== 'POST') {
          return next()
        }
        const proxyReq = http.request(
          {
            hostname: 'localhost',
            port: sessionPort,
            path: '/chat/session',
            method: req.method,
            headers: req.headers,
          },
          (proxyRes) => {
            res.writeHead(proxyRes.statusCode ?? 500, proxyRes.headers)
            proxyRes.pipe(res)
          },
        )
        proxyReq.on('error', (err) => {
          console.error('Session proxy error:', err.message)
          res.writeHead(502)
          res.end('Session server unavailable')
        })
        req.pipe(proxyReq)
      })
    },
    buildEnd() {
      sessionServer?.close()
    },
  }
}

/**
 * Shared Vite config for integration test apps.
 *
 * @param callerUrl  `import.meta.url` from the calling vite.config.ts — used to
 *                   resolve the correct React package from the test app's own
 *                   node_modules (not the workspace root).
 * @param label      Label for the test app (e.g. 'react-16').
 */
export function createTestAppConfig(callerUrl: string, label: string) {
  // Resolve the real React package from the test app's directory so pnpm
  // gives us the correct version (e.g. React 16 not React 19).
  const require = createRequire(callerUrl)
  const realReactPath = dirname(require.resolve('react/package.json'))

  return defineConfig({
    plugins: [reactPlugin(), sessionServerPlugin(label)],
    resolve: {
      alias: [
        // Order matters: react-original MUST come before react so the shim
        // can import the real package without being caught by the react alias.
        {
          find: 'react-original',
          replacement: realReactPath,
        },
        {
          find: /^react$/,
          replacement: shimPath,
        },
      ],
      dedupe: ['react', 'react-dom'],
    },
  })
}
