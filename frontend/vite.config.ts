import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  // Never inline assets as data: URIs; the production CSP only allows fonts from 'self'.
  build: { assetsInlineLimit: 0 },
  resolve: { alias: { "@": new URL("./src", import.meta.url).pathname } },
  server: {
    port: 5173,
    proxy: { '/api': { target: process.env.API_URL ?? 'http://localhost:8081', changeOrigin: false } },
  },
})
