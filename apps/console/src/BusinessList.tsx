import { useState } from 'react'
import styles from './BusinessList.module.css'
import { useMockBackend } from './MockBackendContext.tsx'
import { Mascot } from './Mascot.tsx'
import { NewBusinessModal } from './NewBusinessModal.tsx'

interface BusinessListProps {
  onOpenBusiness: (id: string) => void
}

export function BusinessList({ onOpenBusiness }: BusinessListProps) {
  const { businesses, createBusiness } = useMockBackend()
  const [showNewModal, setShowNewModal] = useState(false)

  return (
    <div>
      <div className={styles.header}>
        <div>
          <h1 className={styles.heading}>你的服務</h1>
          <span className={styles.subheading}>
            每個服務都有自己的對話頁面與 AI 小幫手，顧客可以直接上去問問題。
          </span>
        </div>
        <button type="button" className={styles.newButton} onClick={() => setShowNewModal(true)}>
          ＋ 新增服務
        </button>
      </div>

      {businesses.length === 0 ? (
        <div className={styles.emptyState}>
          還沒有任何服務，點選右上角「新增服務」開始設定你的第一個 AI 小幫手吧！
        </div>
      ) : (
        <div className={styles.grid}>
          {businesses.map((biz) => (
            <button
              key={biz.id}
              type="button"
              className={styles.card}
              onClick={() => onOpenBusiness(biz.id)}
            >
              <div className={styles.cardTop}>
                <div
                  className={styles.mascotWrap}
                  style={{ background: `${biz.themeColor}22` }}
                >
                  <Mascot id={biz.mascot} color={biz.themeColor} size={40} />
                </div>
                <div>
                  <div className={styles.cardName}>{biz.name}</div>
                  <div className={styles.cardSlug}>/support/{biz.slug}</div>
                </div>
              </div>
              <p className={styles.cardTagline}>{biz.tagline || '尚未填寫服務簡介'}</p>
              <div className={styles.statusRow}>
                <span
                  className={`${styles.statusDot} ${biz.connected ? styles.statusDotOn : styles.statusDotOff}`}
                />
                <span className={styles.statusLabel}>
                  {biz.connected ? 'AI 小幫手已上線' : '尚未啟用'}
                </span>
              </div>
            </button>
          ))}
        </div>
      )}

      {showNewModal && (
        <NewBusinessModal
          onCancel={() => setShowNewModal(false)}
          onCreate={(biz) => {
            createBusiness(biz)
            setShowNewModal(false)
            onOpenBusiness(biz.id)
          }}
        />
      )}
    </div>
  )
}
