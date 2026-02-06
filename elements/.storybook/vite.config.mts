import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { createServer as createHttpServer } from 'node:http'
import { createServer as createNetServer, Socket } from 'node:net'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig, loadEnv, Plugin, ViteDevServer } from 'vite'
import { createElementsServerHandlers } from '../src/server'

const __dirname = dirname(fileURLToPath(import.meta.url))

/**
 * Protocol-sniffing proxy that detects HTTP vs HTTPS and routes accordingly.
 * HTTP requests get redirected to HTTPS on the same port.
 *
 * Storybook runs HTTPS on an internal port (publicPort + 1), and this proxy
 * listens on the public port, routing TLS to HTTPS and plain HTTP to redirect.
 */
const protocolSniffingPlugin = (publicPort: number): Plugin => {
  let proxyServer: ReturnType<typeof createNetServer> | null = null
  let httpRedirectServer: ReturnType<typeof createHttpServer> | null = null
  const internalHttpsPort = publicPort + 1 // Storybook HTTPS runs here
  const internalHttpPort = publicPort + 2 // Internal redirect server

  const cleanup = () => {
    proxyServer?.close()
    httpRedirectServer?.close()
  }

  return {
    name: 'protocol-sniffing-proxy',
    configureServer(server: ViteDevServer) {
      // Create internal HTTP server for redirects
      httpRedirectServer = createHttpServer((req, res) => {
        const host = req.headers.host?.split(':')[0] || 'localhost'
        res.writeHead(301, {
          Location: `https://${host}:${publicPort}${req.url}`,
        })
        res.end()
      })
      httpRedirectServer.listen(internalHttpPort, '127.0.0.1')

      // Create TCP proxy that sniffs protocol
      proxyServer = createNetServer((socket: Socket) => {
        socket.once('data', (data) => {
          // TLS handshake starts with 0x16 (22 = handshake)
          const isTLS = data[0] === 0x16

          const targetPort = isTLS ? internalHttpsPort : internalHttpPort
          const targetSocket = new Socket()

          targetSocket.connect(targetPort, '127.0.0.1', () => {
            targetSocket.write(data)
            socket.pipe(targetSocket)
            targetSocket.pipe(socket)
          })

          targetSocket.on('error', () => socket.destroy())
          socket.on('error', () => targetSocket.destroy())
        })

        socket.on('error', () => {})
      })

      proxyServer.listen(publicPort, () => {
        console.log(`\n  ➜  Protocol sniffing proxy on :${publicPort} → HTTPS on :${internalHttpsPort}\n`)
      })

      proxyServer.on('error', (err: NodeJS.ErrnoException) => {
        if (err.code !== 'EADDRINUSE') {
          console.error('Protocol sniffing proxy error:', err)
        }
      })

      // Clean up when dev server closes
      server.httpServer?.on('close', cleanup)
    },
    closeBundle: cleanup,
  }
}

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
    }

    const handlers = createElementsServerHandlers()
    server.middlewares.use('/chat/session', (req, res) =>
      handlers.session(req, res, {
        embedOrigin,
        userIdentifier: process.env.VITE_GRAM_ELEMENTS_STORYBOOK_USER_IDENTIFIER || 'user',
        expiresAfter: 3600,
      })
    )
  },
})

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')

  return {
    plugins: [
      react(),
      tailwindcss(),
      apiMiddlewarePlugin(),
      cspPlugin(),
      protocolSniffingPlugin(6006),
    ],
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
    optimizeDeps: {
      force: true,
    },
  }
})
