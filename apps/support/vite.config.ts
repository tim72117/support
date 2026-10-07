/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Consumer chat page (/support/<slug>). Fixed port 5178 with strictPort so it
// fails loudly instead of silently hopping to a port that belongs to another
// project (5173-5177, 5180, 5183, 5190 are taken by other local projects).
// The backend's PUBLIC_ALLOWED_ORIGIN must include http://localhost:5178.
//
// base: '/support/' matches where the production build is embedded into the
// Go backend binary (see backend/Dockerfile,
// backend/cmd/server/static_support.go) — this is also already the path
// shape the frontend itself expects (App.tsx reads the slug out of
// window.location.pathname as /support/<slug>), so this isn't introducing a
// new assumption, just making the built asset URLs match it too.
export default defineConfig({
  base: '/support/',
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
