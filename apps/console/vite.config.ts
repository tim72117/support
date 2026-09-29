/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Business-owner console. Served at the app root (no onagent-style
// `/app/` base path — ai-support has no separate landing build sharing
// the same origin, so there's nothing to namespace under).
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5174,
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test-setup.ts'],
  },
})
