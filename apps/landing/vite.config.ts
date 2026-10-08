import { defineConfig } from 'vite'
import { resolve } from 'node:path'

// Static-HTML-first, same as onagent's own apps/landing: Vite here is just
// a dev server + bundler for plain HTML/CSS/JS, not a framework app.
// 每種業主類型一個子資料夾（candidate/ …），各自帶自己的 theme.css；
// 多頁面都要列在 input，build 才會輸出。Builds to the default dist/ (gitignored).
export default defineConfig({
  // Tests must never depend on (or react to) a developer's own .env / .env.local: that is where
  // local analytics is switched on, and it would leak into import.meta.env and process.env.
  test: {
    env: { VITE_GA_ID: '', VITE_ANALYTICS_IN_DEV: '', VITE_DISABLE_ANALYTICS: '' },
    // Some analytics tests run real Vite builds; give them room when the machine is busy.
    testTimeout: 30_000,
  },
  server: {
    port: 5176,
    strictPort: true,
    // 頁面一律用相對位址（登入 → /app/、API → /auth /console /public），與正式環境
    // 同網域掛載的方式一致；開發時由這裡轉發到各自的 dev server。
    proxy: {
      '/app': { target: 'http://localhost:5177', rewrite: (p) => p.replace(/^\/app/, '') },
      '/auth': 'http://localhost:8082',
      '/console': 'http://localhost:8082',
      '/public': 'http://localhost:8082',
    },
  },
  build: {
    rollupOptions: {
      input: {
        main: resolve(__dirname, 'index.html'),
        privacy: resolve(__dirname, 'privacy.html'),
        terms: resolve(__dirname, 'terms.html'),
        candidate: resolve(__dirname, 'candidate/index.html'),
        candidateSubscribe: resolve(__dirname, 'candidate/subscribe.html'),
        candidateReserve: resolve(__dirname, 'candidate/reserve.html'),
        candidatePay: resolve(__dirname, 'candidate/pay.html'),
        candidatePayTest: resolve(__dirname, 'candidate/pay-test.html'),
        business: resolve(__dirname, 'business/index.html'),
        businessSubscribe: resolve(__dirname, 'business/subscribe.html'),
        businessReserve: resolve(__dirname, 'business/reserve.html'),
        businessPay: resolve(__dirname, 'business/pay.html'),
      },
    },
  },
})
