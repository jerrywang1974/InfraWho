import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// Lab: proxy API to Go on :8080 so the SPA can use credentials: 'include'.
// changeOrigin left false so Host stays the Vite origin and matches browser Origin
// (avoids requiring INFRAWHO_TRUSTED_ORIGINS for the common localhost lab path).
// Still set INFRAWHO_COOKIE_SECURE=false for plain HTTP.
const apiProxy = {
  '/api': { target: 'http://127.0.0.1:8080' },
  '/healthz': { target: 'http://127.0.0.1:8080' },
  '/readyz': { target: 'http://127.0.0.1:8080' },
}

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: apiProxy,
  },
  preview: {
    port: 4173,
    proxy: apiProxy,
  },
})
