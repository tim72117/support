import { useEffect, useRef, useState, type FormEvent, type KeyboardEvent } from 'react'
import styles from './App.module.css'
import { Mascot } from './Mascot.tsx'
import { ApiError, fetchBusiness, type PublicBusiness } from './api.ts'
import { brandingFor, type Branding } from './branding.ts'
import { useChat } from './useChat.ts'

function slugFromLocation(): string | null {
  // Expected path shape: /support/<slug>.
  const match = window.location.pathname.match(/\/support\/([^/]+)/)
  return match ? decodeURIComponent(match[1]) : null
}

type BusinessState =
  | { status: 'loading' }
  | { status: 'notFound' }
  | { status: 'error' }
  | { status: 'ready'; business: PublicBusiness }

// Looks the business up in the ai-support backend. Only fetches page data —
// no onagent connection is made here.
function useBusiness(slug: string | null): BusinessState {
  const [state, setState] = useState<BusinessState>(slug ? { status: 'loading' } : { status: 'notFound' })
  useEffect(() => {
    if (!slug) return
    let cancelled = false
    setState({ status: 'loading' })
    fetchBusiness(slug).then(
      (business) => !cancelled && setState({ status: 'ready', business }),
      (err) => {
        if (cancelled) return
        setState(err instanceof ApiError && err.status === 404 ? { status: 'notFound' } : { status: 'error' })
      },
    )
    return () => {
      cancelled = true
    }
  }, [slug])
  return state
}

export function App() {
  const [slug] = useState(slugFromLocation)
  const state = useBusiness(slug)

  if (state.status === 'loading') {
    return (
      <div className={styles.notFound}>
        <p>載入中……</p>
      </div>
    )
  }
  if (state.status === 'notFound') {
    return (
      <div className={styles.notFound}>
        <p>找不到這個服務頁面，請確認連結是否正確。</p>
      </div>
    )
  }
  if (state.status === 'error') {
    return (
      <div className={styles.notFound}>
        <p>目前無法載入這個服務頁面，請稍後再試。</p>
      </div>
    )
  }

  return <BusinessChatPage business={state.business} />
}

function BusinessChatPage({ business }: { business: PublicBusiness }) {
  const [branding] = useState(() => brandingFor(business))
  const { started, messages, isTyping, notice, start, send } = useChat(business, branding.greeting)
  const [input, setInput] = useState('')
  const listRef = useRef<HTMLDivElement>(null)
  const unavailable = !business.chat.available || notice?.kind === 'unavailable'

  useEffect(() => {
    const list = listRef.current
    if (!list) return
    // Only auto-scroll when the user is already at (or near) the bottom —
    // otherwise a reply arriving while they're reading older messages would
    // yank the view back down. The last message being the user's own is
    // always a "they just sent it" case, so that always scrolls too.
    const NEAR_BOTTOM_PX = 100
    const distanceFromBottom = list.scrollHeight - list.scrollTop - list.clientHeight
    const lastIsOwnMessage = messages[messages.length - 1]?.role === 'user'
    if (distanceFromBottom <= NEAR_BOTTOM_PX || lastIsOwnMessage) {
      // Optional chaining: jsdom (the test environment) doesn't implement
      // scrollTo, so this would otherwise throw on every render in tests.
      list.scrollTo?.({ top: list.scrollHeight, behavior: 'smooth' })
    }
  }, [messages, isTyping])

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    const text = input.trim()
    if (!text || isTyping || unavailable) return
    void send(text)
    setInput('')
  }

  function handleSuggestion(question: string) {
    if (!started) start()
    void send(question)
  }

  const chatArea = !started ? (
    <div className={styles.welcomeCard}>
      <h2 className={styles.welcomeTitle}>有問題想問我們嗎？</h2>
      <span className={styles.welcomeText}>
        點擊下方按鈕開始對話，AI 小幫手會依照 {business.name} 提供的資訊回答你的問題。
      </span>
      {unavailable ? (
        <span className={styles.welcomeText}>目前無法服務，請稍後再試。</span>
      ) : (
        <button type="button" className={styles.startButton} onClick={start}>
          開始對話
        </button>
      )}
      {!unavailable && (
        <div className={styles.suggestions}>
          {branding.suggestedQuestions.map((q) => (
            <button key={q} type="button" className={styles.suggestionChip} onClick={() => handleSuggestion(q)}>
              {q}
            </button>
          ))}
        </div>
      )}
    </div>
  ) : (
    <ChatPanel
      business={business}
      branding={branding}
      messages={messages}
      isTyping={isTyping}
      notice={notice}
      unavailable={unavailable}
      listRef={listRef}
      input={input}
      onInputChange={setInput}
      onSubmit={handleSubmit}
    />
  )

  if (branding.layout === 'split') {
    return (
      <div className={styles.splitPage} style={{ '--theme-color': branding.themeColor } as React.CSSProperties}>
        <div className={styles.splitSide} style={{ background: `${branding.themeColor}1a` }}>
          <div className={styles.splitSideInner}>
            <Mascot id={branding.mascot} color={branding.themeColor} size={72} animated />
            <h1 className={styles.splitBusinessName}>{business.name}</h1>
            {branding.tagline && <span className={styles.splitTagline}>{branding.tagline}</span>}
            <span className={styles.splitSidePlaceholder}>之後可以在這裡放上你的圖片，讓顧客一眼認出你的服務。</span>
          </div>
        </div>
        <div className={styles.splitMain}>{chatArea}</div>
      </div>
    )
  }

  return (
    <div className={styles.page} style={{ '--theme-color': branding.themeColor } as React.CSSProperties}>
      <div className={styles.brandPanel} style={{ background: branding.themeColor }}>
        <div className={styles.brandBlobTop} />
        <div className={styles.brandBlobBottom} />
        <div className={styles.mascotStage}>
          <Mascot id={branding.mascot} color="#ffffff" size={104} animated />
        </div>
        <h1 className={styles.businessName}>{business.name}</h1>
        {branding.tagline && <span className={styles.tagline}>{branding.tagline}</span>}
      </div>

      <div className={styles.body}>{chatArea}</div>
    </div>
  )
}

function ChatPanel({
  business,
  branding,
  messages,
  isTyping,
  notice,
  unavailable,
  listRef,
  input,
  onInputChange,
  onSubmit,
}: {
  business: PublicBusiness
  branding: Branding
  messages: ReturnType<typeof useChat>['messages']
  isTyping: boolean
  notice: ReturnType<typeof useChat>['notice']
  unavailable: boolean
  listRef: React.RefObject<HTMLDivElement>
  input: string
  onInputChange: (v: string) => void
  onSubmit: (e: FormEvent) => void
}) {
  function handleKeyDown(e: KeyboardEvent<HTMLTextAreaElement>) {
    // isComposing / keyCode 229 guards against IME (注音/拼音/日文) candidate
    // selection: the browser fires a real Enter keydown when the user picks
    // a candidate, which would otherwise submit the half-typed message.
    if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing && e.keyCode !== 229) {
      e.preventDefault()
      onSubmit(e as unknown as FormEvent)
    }
  }

  return (
    <div className={styles.chatPanel}>
      <div className={styles.chatHeader}>
        <Mascot id={branding.mascot} color={branding.themeColor} size={36} />
        <div>
          <div className={styles.chatHeaderName}>{business.name}</div>
          <span className={styles.chatHeaderStatus}>● 線上</span>
        </div>
      </div>

      <div className={styles.messageList} ref={listRef}>
        {messages.map((m) => (
          <div
            key={m.id}
            className={`${styles.bubbleRow} ${m.role === 'user' ? styles.bubbleRowUser : ''}`}
          >
            <div className={`${styles.bubble} ${m.role === 'user' ? styles.bubbleUser : styles.bubbleAssistant}`}>
              {m.text}
            </div>
          </div>
        ))}
        {isTyping && (
          <div className={styles.bubbleRow}>
            <div className={`${styles.bubble} ${styles.bubbleAssistant}`}>
              <span className={styles.typingBubble}>
                <span className={styles.typingDot} />
                <span className={styles.typingDot} />
                <span className={styles.typingDot} />
              </span>
            </div>
          </div>
        )}
      </div>

      {notice && (
        <span className={styles.notice} role="alert">
          {notice.text}
        </span>
      )}

      <form className={styles.composer} onSubmit={onSubmit}>
        <textarea
          className={styles.composerInput}
          rows={1}
          placeholder="輸入訊息……"
          aria-label="輸入訊息"
          value={input}
          onChange={(e) => onInputChange(e.target.value)}
          onKeyDown={handleKeyDown}
          disabled={unavailable}
          maxLength={business.maxMessageLength}
        />
        <button
          type="submit"
          className={styles.sendButton}
          disabled={!input.trim() || isTyping || unavailable}
          aria-label="送出"
        >
          ➤
        </button>
      </form>
      <span className={styles.footerNote}>由 {business.name} 提供的 AI 小幫手回答</span>
    </div>
  )
}
