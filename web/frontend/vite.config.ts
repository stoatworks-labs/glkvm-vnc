import { defineConfig } from 'vite'

// Build output goes to ../dist so the Go server can go:embed it.
export default defineConfig({
    build: {
        outDir: '../dist',
        emptyOutDir: true,
    },
    server: {
        // Dev proxy to the Go gateway.
        proxy: {
            '/api': 'http://localhost:8600',
            '/connect-vnc': { target: 'ws://localhost:8600', ws: true },
        },
    },
})
