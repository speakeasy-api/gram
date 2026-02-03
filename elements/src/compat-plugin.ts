/**
 * Vite plugin for React 16/17 compatibility.
 *
 * Usage:
 * ```ts
 * import { reactCompat } from '@gram-ai/elements/compat'
 * export default defineConfig({ plugins: [reactCompat(), react()] })
 * ```
 */

import { createRequire } from 'node:module'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import type { Plugin } from 'vite'

const __dirname = dirname(fileURLToPath(import.meta.url))
const shimPath = resolve(__dirname, 'react-shim.js')

/** Redirects `import ... from 'react'` through a shim that polyfills React 18 APIs. */
export function reactCompat(): Plugin {
  return {
    name: 'gram-elements-react-compat',
    enforce: 'pre',
    config() {
      const require = createRequire(resolve(process.cwd(), 'package.json'))
      const realReactPath = dirname(require.resolve('react/package.json'))
      return {
        resolve: {
          alias: [
            { find: 'react-original', replacement: realReactPath },
            { find: /^react$/, replacement: shimPath },
          ],
          dedupe: ['react', 'react-dom'],
        },
      }
    },
  }
}
