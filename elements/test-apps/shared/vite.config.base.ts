import reactPlugin from '@vitejs/plugin-react'
import { createRequire } from 'node:module'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vite'

const __dirname = dirname(fileURLToPath(import.meta.url))
const shimPath = resolve(__dirname, 'react-shim.ts')

/**
 * Shared Vite config for integration test apps.
 *
 * @param callerUrl  `import.meta.url` from the calling vite.config.ts — used to
 *                   resolve the correct React package from the test app's own
 *                   node_modules (not the workspace root).
 * @param sessionServerPort  Port the session server runs on.
 */
export function createTestAppConfig(
  callerUrl: string,
  sessionServerPort: number
) {
  // Resolve the real React package from the test app's directory so pnpm
  // gives us the correct version (e.g. React 16 not React 19).
  const require = createRequire(callerUrl)
  const realReactPath = dirname(require.resolve('react/package.json'))

  return defineConfig({
    plugins: [reactPlugin()],
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
    server: {
      proxy: {
        '/api/session': {
          target: `http://localhost:${sessionServerPort}`,
          changeOrigin: true,
        },
      },
    },
  })
}
