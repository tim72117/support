import { useState } from 'react'
import styles from './BusinessEditor.module.css'
import { useMockBackend } from './MockBackendContext.tsx'
import { Mascot } from './Mascot.tsx'
import {
  buildContentText,
  MASCOT_LABELS,
  THEME_COLORS,
  type Business,
  type ContentSection,
  type MascotId,
} from './mockData.ts'

interface BusinessEditorProps {
  businessId: string
  onBack: () => void
}

type Tab = 'content' | 'branding'

export function BusinessEditor({ businessId, onBack }: BusinessEditorProps) {
  const { getBusiness, updateBusiness } = useMockBackend()
  const business = getBusiness(businessId)

  // Hooks below must not sit after a conditional return, or the "no
  // business found" early-return path renders fewer hooks than the normal
  // path (see docs/refactor-initial-scaffold-plan-2026-09-27.md pitfall #2).
  // So: no early return above this point, and the body below tolerates
  // `business` being undefined until the final render check.
  const [tab, setTab] = useState<Tab>('content')
  const [draft, setDraft] = useState<Business | null>(business ?? null)
  const [copied, setCopied] = useState(false)

  // Keep the draft in sync if the underlying business identity changes
  // (navigating from one business's editor straight to another's, in a
  // real router that would remount, but we guard anyway since this is a
  // manual state machine in App.tsx).
  if (draft && draft.id !== businessId) {
    setDraft(business ?? null)
  }

  if (!business || !draft) {
    return (
      <div>
        <p>找不到這個服務。</p>
        <button type="button" onClick={onBack}>
          返回列表
        </button>
      </div>
    )
  }

  const isDirty = JSON.stringify(draft) !== JSON.stringify(business)

  function updateSection(id: string, body: string) {
    setDraft((prev) =>
      prev
        ? { ...prev, sections: prev.sections.map((s) => (s.id === id ? { ...s, body } : s)) }
        : prev,
    )
  }

  function handleSave() {
    updateBusiness(businessId, draft!)
  }

  function copyLink() {
    if (!draft) return
    const url = `${window.location.origin.replace('5174', '5175')}/support/${draft.slug}`
    navigator.clipboard?.writeText(url).catch(() => {})
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }

  return (
    <div>
      <div className={styles.topRow}>
        <div className={styles.mascotWrap} style={{ background: `${draft.themeColor}22` }}>
          <Mascot id={draft.mascot} color={draft.themeColor} size={48} />
        </div>
        <div>
          <h1 className={styles.heading}>{draft.name}</h1>
          <div className={styles.publicLinkRow}>
            <span>對外服務頁面：</span>
            <span className={styles.publicLink}>/support/{draft.slug}</span>
            <button type="button" className={styles.copyButton} onClick={copyLink}>
              {copied ? '已複製！' : '複製連結'}
            </button>
          </div>
        </div>
      </div>

      <div className={styles.tabs}>
        <button
          type="button"
          className={`${styles.tab} ${tab === 'content' ? styles.tabActive : ''}`}
          onClick={() => setTab('content')}
        >
          AI 可以回答的內容
        </button>
        <button
          type="button"
          className={`${styles.tab} ${tab === 'branding' ? styles.tabActive : ''}`}
          onClick={() => setTab('branding')}
        >
          形象設定
        </button>
      </div>

      {tab === 'content' && (
        <ContentTab
          sections={draft.sections}
          onChangeSection={updateSection}
        />
      )}

      {tab === 'branding' && (
        <BrandingTab
          draft={draft}
          onChange={(patch) => setDraft((prev) => (prev ? { ...prev, ...patch } : prev))}
        />
      )}

      <div className={styles.footerBar}>
        <span className={`${styles.footerStatus} ${isDirty ? styles.footerStatusDirty : ''}`}>
          {isDirty ? '有尚未儲存的變更' : '所有變更都已儲存'}
        </span>
        <button type="button" className={styles.saveButton} onClick={handleSave} disabled={!isDirty}>
          儲存變更
        </button>
      </div>
    </div>
  )
}

function ContentTab({
  sections,
  onChangeSection,
}: {
  sections: ContentSection[]
  onChangeSection: (id: string, body: string) => void
}) {
  const previewText = buildContentText(sections)

  return (
    <div>
      <div className={styles.sectionGrid}>
        {sections.map((section) => (
          <div key={section.id} className={styles.sectionCard}>
            <div className={styles.sectionHeader}>
              <span className={styles.sectionTitle}>{section.title}</span>
              {section.body.trim().length > 0 && (
                <span className={styles.filledBadge}>已填寫</span>
              )}
            </div>
            <span className={styles.sectionHint}>{section.placeholder}</span>
            <textarea
              className={styles.textarea}
              value={section.body}
              onChange={(e) => onChangeSection(section.id, e.target.value)}
              placeholder="在這裡輸入內容……"
            />
          </div>
        ))}
      </div>

      <div style={{ marginTop: 'var(--space-5)' }}>
        <div className={styles.previewCard}>
          <span className={styles.previewLabel}>AI 實際會看到的內容預覽</span>
          {previewText ? (
            <div className={styles.previewText}>{previewText}</div>
          ) : (
            <div className={styles.previewText}>目前還沒有填寫任何內容。</div>
          )}
        </div>
      </div>
    </div>
  )
}

function BrandingTab({
  draft,
  onChange,
}: {
  draft: Business
  onChange: (patch: Partial<Business>) => void
}) {
  return (
    <div className={styles.brandingGrid}>
      <div className={styles.brandingCard}>
        <span className={styles.sectionTitle}>服務名稱</span>
        <input
          className={styles.input}
          value={draft.name}
          onChange={(e) => onChange({ name: e.target.value })}
        />
      </div>

      <div className={styles.brandingCard}>
        <span className={styles.sectionTitle}>服務簡介</span>
        <input
          className={styles.input}
          value={draft.tagline}
          placeholder="一句話介紹你的服務"
          onChange={(e) => onChange({ tagline: e.target.value })}
        />
      </div>

      <div className={styles.brandingCard}>
        <span className={styles.sectionTitle}>吉祥物</span>
        <div className={styles.mascotPicker}>
          {(Object.keys(MASCOT_LABELS) as MascotId[]).map((id) => (
            <button
              key={id}
              type="button"
              className={`${styles.mascotOption} ${draft.mascot === id ? styles.mascotOptionSelected : ''}`}
              onClick={() => onChange({ mascot: id })}
              title={MASCOT_LABELS[id]}
            >
              <Mascot id={id} color={draft.themeColor} size={32} />
            </button>
          ))}
        </div>
      </div>

      <div className={styles.brandingCard}>
        <span className={styles.sectionTitle}>主題色</span>
        <div className={styles.colorPicker}>
          {THEME_COLORS.map((color) => (
            <button
              key={color}
              type="button"
              className={`${styles.colorOption} ${draft.themeColor === color ? styles.colorOptionSelected : ''}`}
              style={{ background: color }}
              onClick={() => onChange({ themeColor: color })}
              aria-label={color}
            />
          ))}
        </div>
      </div>
    </div>
  )
}
