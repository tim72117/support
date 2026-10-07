/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Platform-admin back office. In production this is embedded into the Go
// backend binary (see backend/cmd/server/static_admin.go) and served under
// "/admin/" — "/admin/api/" is the admin API (see main.go's
// mountCredentialedRoutes), so base must match whatever prefix
// static_admin.go mounts this at, same convention as apps/console's own
// base: '/app/' (see its vite.config.ts for the full reasoning). The dev
// server is unaffected: Vite ignores `base` for the dev middleware itself.
export default defineConfig({
  base: '/admin/',
  plugins: [react()],
  server: {
    port: 5179,
    strictPort: true, // 5173-5178 belong to other projects on this machine; fail loudly instead of drifting
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test-setup.ts'],
  },
})
