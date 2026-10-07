import { useState } from 'react'
import styles from './BusinessList.module.css'
import { ApiError } from './api.ts'
import { useBackend } from './BackendContext.tsx'
import { Mascot } from './Mascot.tsx'
import {
  isValidSlug,
  MASCOT_LABELS,
  SLUG_MAX_LENGTH,
  suggestSlug,
  THEME_COLORS,
  type Business,
  type MascotId,
} from './model.ts'

interface NewBusinessModalProps {
  onCancel: () => void
  onCreated: (business: Business) => void
}

export function NewBusinessModal({ onCancel, onCreated }: NewBusinessModalProps) {
  const { createBusiness } = useBackend()
  const [name, setName] = useState('')
  const [slug, setSlug] = useState('')
  // Until the owner edits the URL themselves, it follows the name.
  const [slugTouched, setSlugTouched] = useState(false)
  const [mascot, setMascot] = useState<MascotId>('fox')
  const [themeColor, setThemeColor] = useState<string>(THEME_COLORS[0])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  // The same trimmed values are used for the "can create" check and for the
  // request itself — deliberately not two separate checks, so this can't
  // drift into "looked valid, saved with trailing whitespace anyway".
  const trimmedName = name.trim()
  const trimmedSlug = slug.trim()
  const slugOk = isValidSlug(trimmedSlug)
  const canCreate = trimmedName.length > 0 && slugOk && !busy

  function onNameChange(value: string) {
    setName(value)
    if (!slugTouched) setSlug(suggestSlug(value))
  }

  async function handleCreate() {
    if (!canCreate) return
    setBusy(true)
    setError('')
    try {
      const created = await createBusiness({ slug: trimmedSlug, name: trimmedName, mascot, themeColor })
      onCreated(created)
    } catch (err) {
      setError(
        err instanceof ApiError && err.status === 409
          ? '這個網址已經被使用了，請換一個。'
          : err instanceof Error
            ? err.message
            : '建立失敗，請稍後再試。',
      )
      setBusy(false)
    }
  }

  return (
    <div className={styles.overlay} onClick={busy ? undefined : onCancel}>
      <div className={styles.modal} onClick={(e) => e.stopPropagation()}>
        <h2 className={styles.modalTitle}>建立新的服務</h2>

        <div className={styles.field}>
          <label className={styles.label} htmlFor="biz-name">
            服務名稱
          </label>
          <input
            id="biz-name"
            className={styles.input}
            placeholder="例如：晨光烘焙坊"
            value={name}
            onChange={(e) => onNameChange(e.target.value)}
            autoFocus
          />
        </div>

        <div className={styles.field}>
          <label className={styles.label} htmlFor="biz-slug">
            對外網址
          </label>
          <input
            id="biz-slug"
            className={styles.input}
            placeholder="例如：chenguang-bakery"
            value={slug}
            maxLength={SLUG_MAX_LENGTH}
            autoCapitalize="none"
            spellCheck={false}
            onChange={(e) => {
              setSlugTouched(true)
              setSlug(e.target.value.toLowerCase())
            }}
          />
          <span className={`${styles.hintLine} ${trimmedSlug && !slugOk ? styles.hintError : ''}`}>
            {trimmedSlug && !slugOk
              ? '只能使用小寫英文字母、數字與連字號（-），並以英文字母或數字開頭。'
              : `顧客會從 /support/${trimmedSlug || '你的網址'} 進來，建立後無法修改。`}
          </span>
        </div>

        <div className={styles.field}>
          <span className={styles.label}>選一個吉祥物</span>
          <div className={styles.mascotPicker}>
            {(Object.keys(MASCOT_LABELS) as MascotId[]).map((id) => (
              <button
                key={id}
                type="button"
                className={`${styles.mascotOption} ${mascot === id ? styles.mascotOptionSelected : ''}`}
                onClick={() => setMascot(id)}
                aria-label={MASCOT_LABELS[id]}
                title={MASCOT_LABELS[id]}
              >
                <Mascot id={id} color={themeColor} size={40} />
              </button>
            ))}
          </div>
        </div>

        <div className={styles.field}>
          <span className={styles.label}>選一個主題色</span>
          <div className={styles.colorPicker}>
            {THEME_COLORS.map((color) => (
              <button
                key={color}
                type="button"
                className={`${styles.colorOption} ${themeColor === color ? styles.colorOptionSelected : ''}`}
                style={{ background: color }}
                onClick={() => setThemeColor(color)}
                aria-label={color}
              />
            ))}
          </div>
        </div>

        {error && (
          <span className={styles.modalError} role="alert">
            {error}
          </span>
        )}

        <div className={styles.modalActions}>
          <button type="button" className={styles.cancelButton} onClick={onCancel} disabled={busy}>
            取消
          </button>
          <button type="button" className={styles.confirmButton} onClick={handleCreate} disabled={!canCreate}>
            {busy ? '建立中…' : '建立'}
          </button>
        </div>
      </div>
    </div>
  )
}
