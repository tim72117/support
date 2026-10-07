import { useEffect, useState } from 'react'
import styles from './BusinessEditor.module.css'
import { useBackend, type SyncStatus } from './BackendContext.tsx'
import { ConversationsTab } from './ConversationsTab.tsx'
import { Mascot } from './Mascot.tsx'
import {
  buildContentText,
  LAYOUT_LABELS,
  MASCOT_LABELS,
  THEME_COLORS,
  type Business,
  type BusinessPatch,
  type ContentSection,
  type LayoutId,
  type MascotId,
} from './model.ts'

// Where the consumer-facing pages are served (apps/support). Overridable per
// deployment; the default matches the local dev server.
const SUPPORT_URL = ((import.meta.env.VITE_SUPPORT_URL as string | undefined) ?? 'http://localhost:5178').replace(
  /\/+$/,
  '',
)

interface BusinessEditorProps {
  businessId: number
  onBack: () => void
}

type Tab = 'content' | 'conversations' | 'branding'
type ContentLoad = 'loading' | 'ready' | 'error'

interface LookDraft {
  name: string
  tagline: string
  mascot: MascotId
  themeColor: string
  layout: LayoutId
}

function lookOf(b: Business): LookDraft {
  return { name: b.name, tagline: b.tagline, mascot: b.mascot, themeColor: b.themeColor, layout: b.layout }
}

export function BusinessEditor({ businessId, onBack }: BusinessEditorProps) {
  const { businesses, updateBusiness, deleteBusiness, loadContent, saveContent, syncOnagent } = useBackend()
  const business = businesses.find((b) => b.id === businessId)

  // Hooks below must not sit after a conditional return, or the "no
  // business found" early-return path renders fewer hooks than the normal
  // path (see docs/refactor-initial-scaffold-plan-2026-09-27.md pitfall #2).
  // The parent gives this component a `key` per business, so the drafts below
  // are initialised once per business and never need re-syncing.
  const [tab, setTab] = useState<Tab>('content')
  const [look, setLook] = useState<LookDraft | null>(business ? lookOf(business) : null)
  const [savedSections, setSavedSections] = useState<ContentSection[]>([])
  const [sections, setSections] = useState<ContentSection[]>([])
  const [contentLoad, setContentLoad] = useState<ContentLoad>('loading')
  const [loadAttempt, setLoadAttempt] = useState(0)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState('')
  const [sync, setSync] = useState<SyncStatus | null>(null)
  const [syncing, setSyncing] = useState(false)
  const [copied, setCopied] = useState(false)
  const [confirmingDelete, setConfirmingDelete] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState('')

  useEffect(() => {
    let cancelled = false
    setContentLoad('loading')
    loadContent(businessId).then(
      (loaded) => {
        if (cancelled) return
        setSavedSections(loaded)
        setSections(loaded)
        setContentLoad('ready')
      },
      () => !cancelled && setContentLoad('error'),
    )
    return () => {
      cancelled = true
    }
  }, [businessId, loadContent, loadAttempt])

  if (!business || !look) {
    return (
      <div>
        <p>找不到這個服務。</p>
        <button type="button" onClick={onBack}>
          返回列表
        </button>
      </div>
    )
  }

  // One trimmed value is used both to decide "can save" and for the request.
  const trimmedName = look.name.trim()
  const trimmedTagline = look.tagline.trim()
  const patch: BusinessPatch = {}
  if (trimmedName !== business.name) patch.name = trimmedName
  if (trimmedTagline !== business.tagline) patch.tagline = trimmedTagline
  if (look.mascot !== business.mascot) patch.mascot = look.mascot
  if (look.themeColor !== business.themeColor) patch.themeColor = look.themeColor
  if (look.layout !== business.layout) patch.layout = look.layout
  const lookDirty = Object.keys(patch).length > 0
  const contentDirty = contentLoad === 'ready' && JSON.stringify(sections) !== JSON.stringify(savedSections)
  const isDirty = lookDirty || contentDirty
  const canSave = isDirty && trimmedName.length > 0 && !saving

  function updateSection(id: string, body: string) {
    setSections((prev) => prev.map((s) => (s.id === id ? { ...s, body } : s)))
  }

  async function handleSave() {
    if (!canSave) return
    setSaving(true)
    setSaveError('')
    setSync(null)
    try {
      if (lookDirty) {
        const updated = await updateBusiness(businessId, patch)
        setLook(lookOf(updated))
      }
      if (contentDirty) {
        const status = await saveContent(businessId, sections)
        setSavedSections(sections)
        setSync(status)
      }
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : '儲存失敗，請稍後再試。')
    } finally {
      setSaving(false)
    }
  }

  async function handleResync() {
    setSyncing(true)
    setSync(await syncOnagent(businessId))
    setSyncing(false)
  }

  async function handleDelete() {
    setDeleting(true)
    setDeleteError('')
    try {
      await deleteBusiness(businessId)
      onBack()
    } catch (err) {
      setDeleteError(err instanceof Error ? err.message : '刪除失敗，請稍後再試。')
      setDeleting(false)
    }
  }

  function copyLink() {
    const url = `${SUPPORT_URL}/support/${business!.slug}`
    navigator.clipboard?.writeText(url).catch(() => {})
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }

  const showResync = sync === 'failed' || (!business.connected && sync !== 'disabled')

  return (
    <div>
      <div className={styles.topRow}>
        <div className={styles.mascotWrap} style={{ background: `${look.themeColor}22` }}>
          <Mascot id={look.mascot} color={look.themeColor} size={48} />
        </div>
        <div>
          <h1 className={styles.heading}>{business.name}</h1>
          <div className={styles.publicLinkRow}>
            <span>對外服務頁面：</span>
            <span className={styles.publicLink}>/support/{business.slug}</span>
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
          className={`${styles.tab} ${tab === 'conversations' ? styles.tabActive : ''}`}
          onClick={() => setTab('conversations')}
        >
          對話紀錄
        </button>
        <button
          type="button"
          className={`${styles.tab} ${tab === 'branding' ? styles.tabActive : ''}`}
          onClick={() => setTab('branding')}
        >
          形象設定
        </button>
      </div>

      {tab === 'conversations' && <ConversationsTab businessId={businessId} />}

      {tab === 'content' && contentLoad === 'loading' && <div className={styles.loadingBox}>載入內容中…</div>}
      {tab === 'content' && contentLoad === 'error' && (
        <div className={styles.loadingBox} role="alert">
          無法載入內容。
          <button type="button" className={styles.smallButton} onClick={() => setLoadAttempt((n) => n + 1)}>
            重試
          </button>
        </div>
      )}
      {tab === 'content' && contentLoad === 'ready' && (
        <ContentTab sections={sections} onChangeSection={updateSection} />
      )}

      {tab === 'branding' && (
        <>
          <BrandingTab
            draft={look}
            onChange={(p) => setLook((prev) => (prev ? { ...prev, ...p } : prev))}
          />
          <DangerZone
            name={business.name}
            confirming={confirmingDelete}
            deleting={deleting}
            error={deleteError}
            onAsk={() => setConfirmingDelete(true)}
            onCancel={() => {
              setConfirmingDelete(false)
              setDeleteError('')
            }}
            onConfirm={() => void handleDelete()}
          />
        </>
      )}

      {sync && sync !== 'unknown' && (
        <div
          className={`${styles.notice} ${sync === 'ok' ? styles.noticeOk : sync === 'failed' ? styles.noticeWarn : ''}`}
          role="status"
        >
          {sync === 'ok' && '已儲存，AI 小幫手已經用到最新內容。'}
          {sync === 'failed' && '內容已儲存，但同步給 AI 小幫手失敗了。可以稍後重新同步。'}
          {sync === 'disabled' && '內容已儲存。AI 對話服務目前還沒啟用，啟用後就會自動套用。'}
        </div>
      )}
      {showResync && contentLoad === 'ready' && (
        <div className={styles.notice}>
          {business.connected ? '' : 'AI 小幫手還沒有啟用。'}
          <button type="button" className={styles.smallButton} onClick={() => void handleResync()} disabled={syncing}>
            {syncing ? '同步中…' : '重新同步'}
          </button>
        </div>
      )}

      <div className={styles.footerBar}>
        <span className={`${styles.footerStatus} ${isDirty ? styles.footerStatusDirty : ''}`}>
          {saveError ? <span className={styles.errorText}>{saveError}</span> : isDirty ? '有尚未儲存的變更' : '所有變更都已儲存'}
        </span>
        <button type="button" className={styles.saveButton} onClick={() => void handleSave()} disabled={!canSave}>
          {saving ? '儲存中…' : '儲存變更'}
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
          <span className={styles.previewLabel}>內容預覽（AI 會依需要查詢各章節，不會一次整段提供）</span>
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
  draft: LookDraft
  onChange: (patch: Partial<LookDraft>) => void
}) {
  return (
    <div className={styles.brandingGrid}>
      <div className={styles.brandingCard}>
        <span className={styles.sectionTitle}>服務名稱</span>
        <input
          className={styles.input}
          value={draft.name}
          maxLength={60}
          onChange={(e) => onChange({ name: e.target.value })}
        />
      </div>

      <div className={styles.brandingCard}>
        <span className={styles.sectionTitle}>服務簡介</span>
        <input
          className={styles.input}
          value={draft.tagline}
          maxLength={80}
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
              <Mascot id={id} color={draft.themeColor} size={40} />
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

      <div className={styles.brandingCard}>
        <span className={styles.sectionTitle}>版面排版</span>
        <span className={styles.sectionHint}>決定顧客打開對話頁面時看到的版面配置。</span>
        <div className={styles.layoutPicker}>
          {(Object.keys(LAYOUT_LABELS) as LayoutId[]).map((id) => (
            <button
              key={id}
              type="button"
              className={`${styles.layoutOption} ${draft.layout === id ? styles.layoutOptionSelected : ''}`}
              onClick={() => onChange({ layout: id })}
            >
              {LAYOUT_LABELS[id]}
            </button>
          ))}
        </div>
      </div>
    </div>
  )
}

function DangerZone({
  name,
  confirming,
  deleting,
  error,
  onAsk,
  onCancel,
  onConfirm,
}: {
  name: string
  confirming: boolean
  deleting: boolean
  error: string
  onAsk: () => void
  onCancel: () => void
  onConfirm: () => void
}) {
  return (
    <div className={styles.dangerZone}>
      <span className={styles.sectionTitle}>刪除服務</span>
      {!confirming ? (
        <>
          <span className={styles.sectionHint}>刪除後，對外的服務頁面會立刻失效，內容與對話紀錄都會一併移除。</span>
          <button type="button" className={styles.dangerButton} onClick={onAsk}>
            刪除這個服務
          </button>
        </>
      ) : (
        <div className={styles.confirmBox} role="alertdialog" aria-label="確認刪除">
          <span>確定要刪除「{name}」嗎？這個動作無法復原。</span>
          {error && (
            <span className={styles.errorText} role="alert">
              {error}
            </span>
          )}
          <div className={styles.confirmActions}>
            <button type="button" className={styles.smallButton} onClick={onCancel} disabled={deleting}>
              取消
            </button>
            <button type="button" className={styles.dangerButton} onClick={onConfirm} disabled={deleting}>
              {deleting ? '刪除中…' : '確定刪除'}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
