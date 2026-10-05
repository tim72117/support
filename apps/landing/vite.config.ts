import { defineConfig } from 'vite'
import { resolve } from 'node:path'

// Static-HTML-first, same as onagent's own apps/landing: Vite here is just
// a dev server + bundler for plain HTML/CSS/JS, not a framework app.
// 每種業主類型一個子資料夾（candidate/ …），各自帶自己的 theme.css；
// 多頁面都要列在 input，build 才會輸出。Builds to the default dist/ (gitignored).
export default defineConfig({
  build: {
    rollupOptions: {
      input: {
        main: resolve(__dirname, 'index.html'),
        candidate: resolve(__dirname, 'candidate/index.html'),
        candidateSubscribe: resolve(__dirname, 'candidate/subscribe.html'),
        candidatePayTest: resolve(__dirname, 'candidate/pay-test.html'),
      },
    },
  },
})
