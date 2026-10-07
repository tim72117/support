import { useEffect, useState } from 'react'
import styles from './ConversationsTab.module.css'
import { useBackend, type ConversationsState } from './BackendContext.tsx'
import type { Conversation, ConversationDetail } from './model.ts'

// The "對話紀錄" tab in BusinessEditor: a list of the business's consumer-page
// conversations (backend/internal/conversation), drilling into one at a time
// to read the full back-and-forth. Nothing here is fake data; everything
// comes from GET .../conversations and GET .../conversations/{cid}.

function formatTime(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString('zh-TW', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

interface ConversationsTabProps {
  businessId: number
}

export function ConversationsTab({ businessId }: ConversationsTabProps) {
  const { loadConversations } = useBackend()
  const [state, setState] = useState<ConversationsState>('idle')
  const [list, setList] = useState<Conversation[]>([])
  const [error, setError] = useState('')
  const [attempt, setAttempt] = useState(0)
  const [selectedId, setSelectedId] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    setState('loading')
    setSelectedId(null)
    loadConversations(businessId).then((res) => {
      if (cancelled) return
      setState(res.state)
      setList(res.list)
      setError(res.error)
    })
    return () => {
      cancelled = true
    }
  }, [businessId, loadConversations, attempt])

  if (state === 'loading' || state === 'idle') {
    return <div className={styles.loadingBox}>載入對話紀錄中…</div>
  }

  if (state === 'disabled') {
    return <div className={styles.emptyBox}>這台伺服器還沒有啟用對話紀錄功能。</div>
  }

  if (state === 'error') {
    return (
      <div className={styles.loadingBox} role="alert">
        {error || '無法載入對話紀錄。'}
        <button type="button" className={styles.retryButton} onClick={() => setAttempt((n) => n + 1)}>
          重試
        </button>
      </div>
    )
  }

  if (selectedId) {
    return (
      <ConversationDetailView
        businessId={businessId}
        conversationId={selectedId}
        onBack={() => setSelectedId(null)}
      />
    )
  }

  if (list.length === 0) {
    return <div className={styles.emptyBox}>目前還沒有任何對話紀錄。顧客在對外服務頁面提問後，就會出現在這裡。</div>
  }

  return (
    <ul className={styles.list}>
      {list.map((c) => (
        <li key={c.id}>
          <button type="button" className={styles.listItem} onClick={() => setSelectedId(c.id)}>
            <span className={styles.listItemId}>對話 #{c.id.slice(0, 8)}</span>
            <span className={styles.listItemTime}>{formatTime(c.createdAt)}</span>
          </button>
        </li>
      ))}
    </ul>
  )
}

interface ConversationDetailViewProps {
  businessId: number
  conversationId: string
  onBack: () => void
}

type DetailLoad = 'loading' | 'ready' | 'error'

function ConversationDetailView({ businessId, conversationId, onBack }: ConversationDetailViewProps) {
  const { loadConversation } = useBackend()
  const [load, setLoad] = useState<DetailLoad>('loading')
  const [detail, setDetail] = useState<ConversationDetail | null>(null)
  const [error, setError] = useState('')
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    let cancelled = false
    setLoad('loading')
    loadConversation(businessId, conversationId).then(
      (d) => {
        if (cancelled) return
        setDetail(d)
        setLoad('ready')
      },
      (err) => {
        if (cancelled) return
        setError(err instanceof Error ? err.message : '無法載入這則對話。')
        setLoad('error')
      },
    )
    return () => {
      cancelled = true
    }
  }, [businessId, conversationId, loadConversation, attempt])

  return (
    <div>
      <button type="button" className={styles.backButton} onClick={onBack}>
        ← 返回對話列表
      </button>

      {load === 'loading' && <div className={styles.loadingBox}>載入對話內容中…</div>}
      {load === 'error' && (
        <div className={styles.loadingBox} role="alert">
          {error}
          <button type="button" className={styles.retryButton} onClick={() => setAttempt((n) => n + 1)}>
            重試
          </button>
        </div>
      )}
      {load === 'ready' && detail && (
        <div className={styles.messageList}>
          {detail.messages.length === 0 && <div className={styles.emptyBox}>這則對話目前沒有任何訊息。</div>}
          {detail.messages.map((m) => (
            <div
              key={m.id}
              className={`${styles.bubbleRow} ${m.role === 'assistant' ? styles.bubbleRowAssistant : styles.bubbleRowUser}`}
            >
              <div className={`${styles.bubble} ${m.role === 'assistant' ? styles.bubbleAssistant : styles.bubbleUser}`}>
                <span className={styles.bubbleRole}>{m.role === 'assistant' ? 'AI 小幫手' : '顧客'}</span>
                <span className={styles.bubbleContent}>{m.content}</span>
                <span className={styles.bubbleTime}>{formatTime(m.createdAt)}</span>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
