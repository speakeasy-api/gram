/**
 * Vite plugin for React 16/17 compatibility.
 *
 * Redirects `import ... from 'react'` to the react-shim module which
 * re-exports React with polyfilled hooks (useSyncExternalStore, useId,
 * useInsertionEffect). This is necessary because named imports bind at
 * module load time — runtime patching (compat.ts) alone cannot inject
 * missing APIs into third-party libraries like zustand or @assistant-ui/react.
 *
 * Usage:
 * ```ts
 * import { reactCompat } from '@gram-ai/elements/compat'
 *
 * export default defineConfig({
 *   plugins: [react(), reactCompat()],
 * })
 * ```
 */

import { createRequire } from 'node:module'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import type { Plugin } from 'vite'

const __dirname = dirname(fileURLToPath(import.meta.url))
const shimPath = resolve(__dirname, 'react-shim.js')

/**
 * Vite plugin that shims React 18 APIs for React 16/17 projects.
 *
 * Redirects all `import ... from 'react'` statements (including from
 * dependencies) through the react-shim module that polyfills missing hooks.
 * Safe to use with React 18/19 — existing APIs take precedence via `??`.
 */
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
            // Order matters: react-original MUST come before react so the
            // shim can import the real package without being caught by the
            // react alias.
            { find: 'react-original', replacement: realReactPath },
            { find: /^react$/, replacement: shimPath },
          ],
          dedupe: ['react', 'react-dom'],
        },
      }
    },
  }
}
