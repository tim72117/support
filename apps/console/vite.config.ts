/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Business-owner console. Served at the app root (no onagent-style
// `/app/` base path — ai-support has no separate landing build sharing
// the same origin, so there's nothing to namespace under).
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5177,
    strictPort: true, // 5173-5175 belong to other projects on this machine; fail loudly instead of drifting
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test-setup.ts'],
  },
})
