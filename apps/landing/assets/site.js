// 「登入」按鈕指向業主管理後台。位址預設為本機開發的後台，
// 部署時可用 ?console=https://... 覆寫（與訂閱頁共用同一個參數）。
const params = new URLSearchParams(location.search)
const consoleUrl = params.get('console') || 'http://localhost:5177'
const apiBase = (params.get('api') || 'http://localhost:8082').replace(/\/+$/, '')

const links = document.querySelectorAll('[data-console-link]')
links.forEach((a) => {
  a.href = consoleUrl
})

// 已登入時，導覽列的「登入」改成「進入管理後台」，讓使用者知道自己已經
// 登入過，不用重新輸入帳密——沒有 /auth/me 可用的頁面（例如使用者未登入、
// 或後端連不上）維持原本的「登入」文字與連結，行為不變。
fetch(apiBase + '/auth/me', { credentials: 'include' })
  .then((res) => (res.ok ? res.json() : null))
  .then((me) => {
    const email = me && (me.Email || me.email)
    if (!email) return
    links.forEach((a) => {
      a.textContent = '進入管理後台'
      a.title = '已登入：' + email
    })
  })
  .catch(() => {
    // 連不上後端：導覽列維持「登入」，不影響頁面其餘功能。
  })
