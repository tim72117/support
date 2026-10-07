import styles from './Dashboard.module.css'
import { useBackend } from './BackendContext.tsx'

// The basic, no-frills admin view: every business owner (email, how many
// businesses they have, current plan tier/usage, billing status) and every
// business across every owner. Read-only — no "operate payment on someone's
// behalf" controls here (see backend/internal/admin's package doc comment
// for why that was deliberately left out of this first version).

function statusBadgeClass(status: string | undefined): string {
  switch (status) {
    case 'active':
      return styles.badgeActive
    case 'past_due':
      return styles.badgePastDue
    case 'trialing':
      return styles.badgeTrialing
    case 'canceled':
      return styles.badgeCanceled
    case 'expired':
      return styles.badgeExpired
    default:
      return styles.badgeNone
  }
}

export function Dashboard() {
  const { owners, businesses, listError, reload } = useBackend()

  return (
    <div>
      <div className={styles.refreshRow}>
        <button className={styles.refreshButton} type="button" onClick={() => void reload()}>
          重新整理
        </button>
      </div>
      {listError && <div className={styles.error}>{listError}</div>}

      <section className={styles.section}>
        <h2 className={styles.heading}>業主（共 {owners.length} 位）</h2>
        <div className={styles.card}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th>Email</th>
                <th>Business 數量</th>
                <th>方案</th>
                <th>用量</th>
                <th>訂閱狀態</th>
                <th>目前週期結束</th>
              </tr>
            </thead>
            <tbody>
              {owners.map((o) => (
                <tr key={o.id}>
                  <td>{o.email}</td>
                  <td>{o.businessCount}</td>
                  <td>{o.planName || <span className={styles.muted}>（無訂閱）</span>}</td>
                  <td>
                    {o.tier ? (
                      <>
                        {o.used.toLocaleString()} / {o.limit.toLocaleString()}
                      </>
                    ) : (
                      <span className={styles.muted}>—</span>
                    )}
                  </td>
                  <td>
                    <span className={`${styles.badge} ${statusBadgeClass(o.billingStatus)}`}>
                      {o.billingStatus || '無'}
                    </span>
                  </td>
                  <td>{o.currentPeriodEnd || <span className={styles.muted}>—</span>}</td>
                </tr>
              ))}
              {owners.length === 0 && (
                <tr>
                  <td colSpan={6} className={styles.muted}>
                    目前沒有任何業主帳號。
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </section>

      <section className={styles.section}>
        <h2 className={styles.heading}>Business（共 {businesses.length} 個）</h2>
        <div className={styles.card}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th>名稱</th>
                <th>URL</th>
                <th>Owner ID</th>
                <th>已連接 AI</th>
              </tr>
            </thead>
            <tbody>
              {businesses.map((b) => (
                <tr key={b.ID}>
                  <td>{b.Name}</td>
                  <td>/support/{b.Slug}</td>
                  <td>{b.OwnerID}</td>
                  <td>{b.Connected ? '是' : '否'}</td>
                </tr>
              ))}
              {businesses.length === 0 && (
                <tr>
                  <td colSpan={4} className={styles.muted}>
                    目前沒有任何 business。
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  )
}
