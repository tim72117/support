import { useState } from 'react'
import styles from './BusinessList.module.css'
import { Mascot } from './Mascot.tsx'
import { createBlankBusiness, MASCOT_LABELS, THEME_COLORS, type MascotId } from './mockData.ts'
import type { Business } from './mockData.ts'

interface NewBusinessModalProps {
  onCancel: () => void
  onCreate: (business: Business) => void
}

function slugify(name: string): string {
  return name
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9一-鿿]+/g, '-')
    .replace(/^-+|-+$/g, '')
}

export function NewBusinessModal({ onCancel, onCreate }: NewBusinessModalProps) {
  const [name, setName] = useState('')
  const [mascot, setMascot] = useState<MascotId>('fox')
  const [themeColor, setThemeColor] = useState<string>(THEME_COLORS[0])

  // Same trimmed value used for both the "can submit" check and the actual
  // create below — deliberately not two separate checks, so this can't
  // drift into "looked valid, saved with trailing whitespace anyway"
  // (see docs/refactor-initial-scaffold-plan-2026-09-27.md's known-pitfalls list).
  const trimmedName = name.trim()
  const canCreate = trimmedName.length > 0

  function handleCreate() {
    if (!canCreate) return
    onCreate(
      createBlankBusiness({
        name: trimmedName,
        slug: slugify(trimmedName) || `business-${Date.now()}`,
        tagline: '',
        mascot,
        themeColor,
      }),
    )
  }

  return (
    <div className={styles.overlay} onClick={onCancel}>
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
            onChange={(e) => setName(e.target.value)}
            autoFocus
          />
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
                <Mascot id={id} color={themeColor} size={32} />
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

        <div className={styles.modalActions}>
          <button type="button" className={styles.cancelButton} onClick={onCancel}>
            取消
          </button>
          <button
            type="button"
            className={styles.confirmButton}
            onClick={handleCreate}
            disabled={!canCreate}
          >
            建立
          </button>
        </div>
      </div>
    </div>
  )
}
