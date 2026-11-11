import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// Public path the app is served under. "/" locally; "/evergreen/" behind the shared droplet proxy
// (set by the BASE_PATH build arg). The proxy strips the prefix before forwarding.
const base = process.env.BASE_PATH || '/'

export default defineConfig({
  base,
  plugins: [vue()],
  // Never inline assets as data: URIs; the production CSP only allows fonts from 'self'.
  build: { assetsInlineLimit: 0 },
  resolve: { alias: { "@": new URL("./src", import.meta.url).pathname } },
  server: {
    port: 5173,
    proxy: { '/api': { target: process.env.API_URL ?? 'http://localhost:8081', changeOrigin: false } },
  },
})
