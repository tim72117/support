import { useState, type FormEvent } from 'react'
import styles from './Login.module.css'
import { useBackend } from './BackendContext.tsx'
import { Mascot } from './Mascot.tsx'

// Real login / register against the backend (/auth/login, /auth/register).
// Same email trimmed for the "can submit" check and the request, and the
// password is sent exactly as typed (never trimmed).
export function Login() {
  const { login, register } = useBackend()
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const trimmedEmail = email.trim()
  const canSubmit = trimmedEmail !== '' && password !== '' && !busy
  const isRegister = mode === 'register'

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    if (!canSubmit) return
    setBusy(true)
    setError('')
    try {
      await (isRegister ? register : login)(trimmedEmail, password)
    } catch (err) {
      setError(err instanceof Error ? err.message : '發生錯誤，請稍後再試')
      setBusy(false)
    }
    // On success the whole screen is replaced by the console, so there is
    // nothing to reset here.
  }

  return (
    <div className={styles.page}>
      <div className={styles.card}>
        <div className={styles.brandRow}>
          <Mascot id="fox" color="#ff8a5b" size={72} />
        </div>
        <h1 className={styles.title}>{isRegister ? '建立帳號' : '歡迎回來'}</h1>
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
              required
              className={styles.input}
              placeholder={isRegister ? '至少 8 個字元' : '輸入密碼'}
              autoComplete={isRegister ? 'new-password' : 'current-password'}
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
            {busy ? '請稍候…' : isRegister ? '註冊並登入' : '登入'}
          </button>
        </form>
        <span className={styles.hint}>
          {isRegister ? '已經有帳號？' : '還沒有帳號？'}
          <button
            type="button"
            className={styles.switchMode}
            onClick={() => {
              setMode(isRegister ? 'login' : 'register')
              setError('')
            }}
          >
            {isRegister ? '改為登入' : '建立帳號'}
          </button>
        </span>
      </div>
    </div>
  )
}
