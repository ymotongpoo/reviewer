import { defineConfig } from 'vite'
import preact from '@preact/preset-vite'
import { readFileSync, readdirSync } from 'node:fs'

// During development run `reviewer serve --token dev` and `npm run dev`.
const target = process.env.REVIEWER_URL ?? 'http://127.0.0.1:7777'
const token = process.env.REVIEWER_TOKEN ?? 'dev'

export default defineConfig({
  plugins: [preact(), {
    // Keep PDF text extraction self-contained, including predefined CJK maps.
    name: 'pdf-text-resources',
    generateBundle() {
      for (const dir of ['cmaps', 'standard_fonts']) {
        const root = new URL(`./node_modules/pdfjs-dist/${dir}/`, import.meta.url)
        for (const name of readdirSync(root)) {
          this.emitFile({ type: 'asset', fileName: `assets/pdf/${dir}/${name}`, source: readFileSync(new URL(name, root)) })
        }
      }
    },
    configureServer(server) {
      server.middlewares.use('/assets/pdf/', (req, res, next) => {
        const path = req.url?.split('?')[0] ?? ''
        if (!/^\/(cmaps|standard_fonts)\/[\w.-]+$/.test(path)) return next()
        try { res.end(readFileSync(new URL(`./node_modules/pdfjs-dist${path}`, import.meta.url))) } catch { next() }
      })
    },
  }],
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
