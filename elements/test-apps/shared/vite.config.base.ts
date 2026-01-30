import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

/**
 * Shared Vite config for integration test apps.
 * Each app calls this with its session server port.
 */
export function createTestAppConfig(sessionServerPort: number) {
  return defineConfig({
    plugins: [react()],
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
