import { useState, type FormEvent } from 'react'
import styles from './Login.module.css'
import { useMockBackend } from './MockBackendContext.tsx'
import { Mascot } from './Mascot.tsx'

// Fake login: any email/password combination "works" and just stores the
// email in the mock session. This is the front-end-first build — real
// register/login wiring against internal/session comes later (see
// docs/refactor-initial-scaffold-plan-2026-09-27.md item 1).
export function Login() {
  const { login } = useMockBackend()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    if (!email.trim()) return
    login(email.trim())
  }

  return (
    <div className={styles.page}>
      <div className={styles.card}>
        <div className={styles.brandRow}>
          <Mascot id="fox" color="#ff8a5b" size={72} />
        </div>
        <h1 className={styles.title}>歡迎回來</h1>
        <span className={styles.subtitle}>
          登入管理你的 AI 客服小幫手，設定顧客可以問到的內容。
        </span>
        <form className={styles.form} onSubmit={handleSubmit}>
          <div>
            <label className={styles.label} htmlFor="email">
              電子信箱
            </label>
            <input
              id="email"
              type="email"
              className={styles.input}
              placeholder="you@example.com"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required
            />
          </div>
          <div>
            <label className={styles.label} htmlFor="password">
              密碼
            </label>
            <input
              id="password"
              type="password"
              className={styles.input}
              placeholder="輸入密碼"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </div>
          <button type="submit" className={styles.submit}>
            登入
          </button>
        </form>
        <span className={styles.hint}>示範版本：輸入任何信箱即可登入體驗</span>
      </div>
    </div>
  )
}
