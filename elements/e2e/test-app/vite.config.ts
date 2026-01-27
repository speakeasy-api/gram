import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  root: __dirname,
  resolve: {
    alias: {
      '@': path.resolve(__dirname, '../../src'),
      // Prevent MSW from being resolved
      'msw': false,
      'msw/node': false,
      '@mswjs/interceptors': false,
    },
  },
  define: {
    // Define build-time globals that the library expects
    __GRAM_API_URL__: JSON.stringify('https://api.getgram.ai'),
    __GRAM_GIT_SHA__: JSON.stringify('test'),
  },
  server: {
    port: 3099,
    strictPort: true,
  },
  optimizeDeps: {
    exclude: ['msw', 'msw/node', '@mswjs/interceptors'],
  },
})
