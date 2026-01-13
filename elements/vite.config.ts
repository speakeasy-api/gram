import react from '@vitejs/plugin-react'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vite'
import tailwindcss from '@tailwindcss/vite'
import dts from 'vite-plugin-dts'

const __dirname = dirname(fileURLToPath(import.meta.url))

export default defineConfig({
  plugins: [react(), dts(), tailwindcss()],
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
      external: [
        'react',
        'react-dom',
        'react/jsx-runtime',
        // Externalize heavy dependencies - consumers must install these
        'motion',
        'motion/react',
        'motion/react-m',
        'zustand',
        'zustand/shallow',
        'remark-gfm',
        'vega',
        'shiki',
      ],
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
