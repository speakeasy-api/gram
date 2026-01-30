import { readFileSync } from 'node:fs'
import reactPlugin from '@vitejs/plugin-react'
import { createRequire } from 'node:module'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig, type Plugin } from 'vite'

const __dirname = dirname(fileURLToPath(import.meta.url))
const shimPath = resolve(__dirname, 'react-shim.ts')

/**
 * Vite plugin that handles /chat/session requests directly as middleware.
 * Proxies session creation to the Gram API using credentials from .env.local.
 */
function sessionServerPlugin(label: string): Plugin {
  return {
    name: 'session-server',
    configureServer(viteServer) {
      const vitePort = viteServer.config.server.port ?? 5173
      const embedOrigin = `http://localhost:${vitePort}`

      // Handle /chat/session directly — no separate server or proxy needed.
      viteServer.middlewares.use((req, res, next) => {
        if (req.url !== '/chat/session' || req.method !== 'POST') {
          return next()
        }

        const projectSlug = Array.isArray(req.headers['gram-project'])
          ? req.headers['gram-project'][0]
          : req.headers['gram-project']

        const apiKey = process.env.GRAM_API_KEY ?? ''
        const base = process.env.GRAM_API_URL ?? 'https://app.getgram.ai'

        fetch(base + '/rpc/chatSessions.create', {
          method: 'POST',
          body: JSON.stringify({
            embed_origin: embedOrigin,
            user_identifier: `${label}-test-user`,
          }),
          headers: {
            'Content-Type': 'application/json',
            'Gram-Project': typeof projectSlug === 'string' ? projectSlug : '',
            'Gram-Key': apiKey,
          },
        })
          .then(async (response) => {
            const body = await response.text()
            res.writeHead(response.status, {
              'Content-Type': 'application/json',
              'Access-Control-Allow-Origin': '*',
            })
            res.end(body)
          })
          .catch((error) => {
            console.error('Session error:', error)
            res.writeHead(500, { 'Content-Type': 'application/json' })
            res.end(JSON.stringify({ error: error.message }))
          })
      })
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
  const callerDir = dirname(fileURLToPath(callerUrl))

  // Resolve the real React package from the test app's directory so pnpm
  // gives us the correct version (e.g. React 16 not React 19).
  const require = createRequire(callerUrl)
  const realReactPath = dirname(require.resolve('react/package.json'))

  // Load env vars from .env.local so server-side code like GRAM_API_KEY
  // is available. Force-read the file because loadEnv gives existing
  // process.env values precedence, which breaks local overrides.
  const envLocalPath = resolve(callerDir, '.env.local')
  try {
    const content = readFileSync(envLocalPath, 'utf-8')
    for (const line of content.split('\n')) {
      const match = line.match(/^([^#=]+)=(.*)$/)
      if (match) process.env[match[1].trim()] = match[2].trim()
    }
  } catch {
    // .env.local is optional
  }

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
