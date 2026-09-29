/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Consumer chat page, served at the app root. Runs on its own port
// separate from apps/console during local dev.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5175,
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test-setup.ts'],
  },
})
