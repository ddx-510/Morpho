import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: '../static',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/events': 'http://localhost:8390',
      '/chat': 'http://localhost:8390',
      '/history': 'http://localhost:8390',
      '/api': 'http://localhost:8390',
    },
  },
})
