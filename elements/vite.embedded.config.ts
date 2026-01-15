import tailwindcss from '@tailwindcss/vite'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vite'

const __dirname = dirname(fileURLToPath(import.meta.url))

// Separate config for building the embedded CSS only
export default defineConfig({
  plugins: [tailwindcss()],
  build: {
    emptyOutDir: false, // Don't clean dist, we're adding to it
    lib: {
      entry: resolve(__dirname, 'src/embedded.ts'),
      formats: ['es'],
      fileName: 'elements-embedded',
    },
    rollupOptions: {
      output: {
        assetFileNames: 'elements-embedded[extname]',
      },
    },
  },
})
