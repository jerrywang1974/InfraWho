import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// Lab: proxy API to Go on :8080 so the SPA can use credentials: 'include'.
// Set INFRAWHO_TRUSTED_ORIGINS=http://localhost:5173 and INFRAWHO_COOKIE_SECURE=false on the API.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
      '/healthz': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
      '/readyz': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
    },
  },
})
