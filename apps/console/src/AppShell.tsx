import type { ReactNode } from 'react'
import styles from './AppShell.module.css'
import { useBackend } from './BackendContext.tsx'

interface AppShellProps {
  children: ReactNode
  onNavigateHome: () => void
  showBackButton: boolean
}

export function AppShell({ children, onNavigateHome, showBackButton }: AppShellProps) {
  const { session, logout } = useBackend()

  return (
    <div className={styles.shell}>
      <header className={styles.topBar}>
        <div className={styles.left}>
          {showBackButton && (
            <button className={styles.backButton} onClick={onNavigateHome} type="button">
              ← 返回列表
            </button>
          )}
          <button
            className={styles.brand}
            onClick={onNavigateHome}
            type="button"
            style={{ border: 'none', background: 'none', cursor: 'pointer', padding: 0 }}
          >
            <span className={styles.brandDot} />
            小幫手管理後台
          </button>
        </div>
        <div className={styles.right}>
          {session && <span className={styles.ownerName}>{session.email}</span>}
          <button className={styles.logout} onClick={logout} type="button">
            登出
          </button>
        </div>
      </header>
      <main className={styles.main}>{children}</main>
    </div>
  )
}
