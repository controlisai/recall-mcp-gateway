import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  base: '/dashboard/',
  build: {
    outDir: '../internal/dashboard/static',
    emptyOutDir: false,
    sourcemap: false,
  },
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/ws/audit': {
        target: 'http://localhost:8090',
        ws: true,
      },
      '/admin/v1': {
        target: 'http://localhost:8090',
      },
      '/dashboard/login': {
        target: 'http://localhost:8090',
      },
      '/dashboard/logout': {
        target: 'http://localhost:8090',
      },
      '/dashboard/auth-status': {
        target: 'http://localhost:8090',
      },
    },
  },
})
