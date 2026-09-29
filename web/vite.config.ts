import { defineConfig } from 'vite'
import preact from '@preact/preset-vite'

// During development run `reviewer serve --token dev` and `npm run dev`.
const target = process.env.REVIEWER_URL ?? 'http://127.0.0.1:7777'
const token = process.env.REVIEWER_TOKEN ?? 'dev'

export default defineConfig({
  plugins: [preact()],
  resolve: {
    // zenn-markdown-html imports the full shiki bundle; use a curated subset.
    alias: [{ find: /^shiki$/, replacement: new URL('./src/preview/zenn-shiki.ts', import.meta.url).pathname }],
  },
  build: {
    outDir: '../internal/server/dist',
    emptyOutDir: true,
    chunkSizeWarningLimit: 1500,
  },
  server: {
    proxy: {
      '/api': {
        target,
        changeOrigin: true,
        headers: { Authorization: `Bearer ${token}` },
      },
    },
  },
})
