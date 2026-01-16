import react from '@vitejs/plugin-react'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vite'
import tailwindcss from '@tailwindcss/vite'
import dts from 'vite-plugin-dts'
import { externalizeDeps } from 'vite-plugin-externalize-deps'

const __dirname = dirname(fileURLToPath(import.meta.url))

export default defineConfig({
  plugins: [
    react(),
    dts(),
    tailwindcss(),

    // Automatically keep peerDependencies as they are defined in the package.json in sync
    // with the rollupOptions.external list
    externalizeDeps({
      // We mark deps as false because the plugins default behaviour is externalise all deps.
      deps: false,
      peerDeps: true,
      optionalDeps: false,
      devDeps: false,
    }),
  ],
  build: {
    sourcemap: true,
    minify: 'esbuild',
    lib: {
      entry: {
        elements: resolve(__dirname, 'src/index.ts'),
        server: resolve(__dirname, 'src/server.ts'),
        plugins: resolve(__dirname, 'src/plugins/index.ts'),
      },
      formats: ['es', 'cjs'],
    },
    rollupOptions: {
      // NOTE: do not define externals here, as they are defined in the externalizeDeps plugin
      output: {
        globals: {
          react: 'React',
          'react-dom': 'ReactDOM',
        },

        sourcemapExcludeSources: true,
      },
    },
  },
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
    },
  },
  define: {
    __GRAM_API_URL__: JSON.stringify(process.env['GRAM_API_URL'] || ''),
    __GRAM_GIT_SHA__: JSON.stringify(process.env['GRAM_GIT_SHA'] || ''),
  },
})
