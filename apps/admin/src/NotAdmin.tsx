import styles from './NotAdmin.module.css'
import { useBackend } from './BackendContext.tsx'

// Shown to anyone who successfully logs in (a real account, real password)
// but whose email is not in the backend's ADMIN_EMAILS allowlist. This is
// the one screen that must never be confused with "the admin back office is
// loading" or "something failed" — it's an explicit, unambiguous "you don't
// have access" so a non-admin business owner who stumbles onto this app's
// URL doesn't think it's broken or half-working.
export function NotAdmin() {
  const { session, logout } = useBackend()

  return (
    <div className={styles.page}>
      <div className={styles.card}>
        <h1 className={styles.title}>你沒有管理員權限</h1>
        <p className={styles.body}>
          {session?.email} 已登入，但這個帳號不在管理員名單中。
          <br />
          如果你認為這是錯誤，請聯絡平台負責人將你的信箱加入 ADMIN_EMAILS。
        </p>
        <button className={styles.logout} type="button" onClick={() => void logout()}>
          登出
        </button>
      </div>
    </div>
  )
}
