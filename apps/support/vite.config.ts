/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Consumer chat page (/support/<slug>). Fixed port 5178 with strictPort so it
// fails loudly instead of silently hopping to a port that belongs to another
// project (5173-5177, 5180, 5183, 5190 are taken by other local projects).
// The backend's PUBLIC_ALLOWED_ORIGIN must include http://localhost:5178.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5178,
    strictPort: true,
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test-setup.ts'],
  },
})
