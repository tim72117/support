import { useState, type FormEvent } from 'react'
import styles from './Login.module.css'
import { useBackend } from './BackendContext.tsx'

// Signs in through the exact same /auth/login the business-owner console
// uses (backend/internal/session) — there is no separate admin login form
// or password. Whether this particular account is allowed to see the admin
// back office is decided afterwards, by App.tsx checking adminState (see
// BackendContext.tsx's /admin/api/me call) — this form has no notion of
// "admin" at all, on purpose.
export function Login() {
  const { login } = useBackend()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const trimmedEmail = email.trim()
  const canSubmit = trimmedEmail !== '' && password !== '' && !busy

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    if (!canSubmit) return
    setBusy(true)
    setError('')
    try {
      await login(trimmedEmail, password)
    } catch (err) {
      setError(err instanceof Error ? err.message : '發生錯誤，請稍後再試')
      setBusy(false)
    }
  }

  return (
    <div className={styles.page}>
      <div className={styles.card}>
        <h1 className={styles.title}>平台管理後台</h1>
        <span className={styles.subtitle}>使用你的一般帳號登入。只有管理員名單中的帳號能看到管理資料。</span>
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
              required
              className={styles.input}
              placeholder="輸入密碼"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </div>
          {error && (
            <span className={styles.error} role="alert">
              {error}
            </span>
          )}
          <button type="submit" className={styles.submit} disabled={!canSubmit}>
            {busy ? '請稍候…' : '登入'}
          </button>
        </form>
      </div>
    </div>
  )
}
