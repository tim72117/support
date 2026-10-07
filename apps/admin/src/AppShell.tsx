import type { ReactNode } from 'react'
import styles from './AppShell.module.css'
import { useBackend } from './BackendContext.tsx'

export function AppShell({ children }: { children: ReactNode }) {
  const { session, logout } = useBackend()

  return (
    <div className={styles.shell}>
      <header className={styles.topBar}>
        <span className={styles.brand}>平台管理後台</span>
        <div className={styles.right}>
          {session && <span className={styles.email}>{session.email}</span>}
          <button className={styles.logout} type="button" onClick={() => void logout()}>
            登出
          </button>
        </div>
      </header>
      <main className={styles.main}>{children}</main>
    </div>
  )
}
