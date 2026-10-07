import { useEffect, useRef, useState } from 'react'
import styles from './AccountMenu.module.css'

interface AccountMenuProps {
  email: string
  onLogout: () => void
}

// Avatar + dropdown (email, logout) — replaces the old flat "email text +
// logout button" pair in AppShell's top bar with a single, less noisy
// account affordance, matching the common SaaS pattern (Gmail/Notion-style
// initial-letter avatar that opens a small menu on click).
export function AccountMenu({ email, onLogout }: AccountMenuProps) {
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)
  const initial = email.trim().charAt(0).toUpperCase() || '?'

  useEffect(() => {
    if (!open) return

    function handlePointerDown(e: PointerEvent) {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        setOpen(false)
      }
    }
    document.addEventListener('pointerdown', handlePointerDown)
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('pointerdown', handlePointerDown)
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [open])

  return (
    <div className={styles.root} ref={rootRef}>
      <button
        type="button"
        className={styles.avatarButton}
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label="帳號選單"
      >
        {initial}
      </button>
      {open && (
        <div className={styles.menu} role="menu">
          <span className={styles.menuEmail}>{email}</span>
          <button
            type="button"
            className={styles.menuLogout}
            role="menuitem"
            onClick={() => {
              setOpen(false)
              onLogout()
            }}
          >
            登出
          </button>
        </div>
      )}
    </div>
  )
}
