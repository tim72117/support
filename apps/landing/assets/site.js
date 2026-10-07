// 「登入」按鈕指向業主管理後台。位址預設為本機開發的後台，
// 部署時可用 ?console=https://... 覆寫（與訂閱頁共用同一個參數）。
const consoleUrl = new URLSearchParams(location.search).get('console') || 'http://localhost:5177'
document.querySelectorAll('[data-console-link]').forEach((a) => {
  a.href = consoleUrl
})
