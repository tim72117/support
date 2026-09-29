import { useEffect, useRef, useState, type FormEvent, type KeyboardEvent } from 'react'
import styles from './App.module.css'
import { Mascot } from './Mascot.tsx'
import { findBusinessBySlug, DEFAULT_DEMO_SLUG, type DemoBusiness } from './mockData.ts'
import { useDemoChat } from './useDemoChat.ts'

function slugFromLocation(): string {
  // Expected path shape: /support/<slug>. Falls back to the demo slug so
  // opening this app at "/" during local dev still shows something,
  // instead of an empty not-found screen.
  const match = window.location.pathname.match(/\/support\/([^/]+)/)
  return match?.[1] ?? DEFAULT_DEMO_SLUG
}

export function App() {
  const [slug] = useState(slugFromLocation)
  const business = findBusinessBySlug(slug)

  if (!business) {
    return (
      <div className={styles.notFound}>
        <p>找不到這個服務頁面，請確認連結是否正確。</p>
      </div>
    )
  }

  return <BusinessChatPage business={business} />
}

function BusinessChatPage({ business }: { business: DemoBusiness }) {
  const { started, messages, isTyping, start, send } = useDemoChat(business.greeting)
  const [input, setInput] = useState('')
  const listRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    listRef.current?.scrollTo({ top: listRef.current.scrollHeight, behavior: 'smooth' })
  }, [messages, isTyping])

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    if (!input.trim()) return
    send(input)
    setInput('')
  }

  function handleSuggestion(question: string) {
    if (!started) start()
    // Send on the next tick so the greeting message from start() is
    // already in the list before the user's question appears after it.
    setTimeout(() => send(question), 0)
  }

  return (
    <div className={styles.page}>
      <div className={styles.brandPanel} style={{ background: business.themeColor }}>
        <div className={styles.brandBlobTop} />
        <div className={styles.brandBlobBottom} />
        <div className={styles.mascotStage}>
          <Mascot id={business.mascot} color="#ffffff" size={104} animated />
        </div>
        <h1 className={styles.businessName}>{business.name}</h1>
        <span className={styles.tagline}>{business.tagline}</span>
      </div>

      <div className={styles.body}>
        {!started ? (
          <div className={styles.welcomeCard}>
            <h2 className={styles.welcomeTitle}>有問題想問我們嗎？</h2>
            <span className={styles.welcomeText}>
              點擊下方按鈕開始對話，AI 小幫手會依照 {business.name} 提供的資訊回答你的問題。
            </span>
            <button
              type="button"
              className={styles.startButton}
              style={{ background: business.themeColor }}
              onClick={start}
            >
              開始對話
            </button>
            <div className={styles.suggestions}>
              {business.suggestedQuestions.map((q) => (
                <button
                  key={q}
                  type="button"
                  className={styles.suggestionChip}
                  onClick={() => handleSuggestion(q)}
                >
                  {q}
                </button>
              ))}
            </div>
          </div>
        ) : (
          <ChatPanel
            business={business}
            messages={messages}
            isTyping={isTyping}
            listRef={listRef}
            input={input}
            onInputChange={setInput}
            onSubmit={handleSubmit}
          />
        )}
      </div>
    </div>
  )
}

function ChatPanel({
  business,
  messages,
  isTyping,
  listRef,
  input,
  onInputChange,
  onSubmit,
}: {
  business: DemoBusiness
  messages: ReturnType<typeof useDemoChat>['messages']
  isTyping: boolean
  listRef: React.RefObject<HTMLDivElement>
  input: string
  onInputChange: (v: string) => void
  onSubmit: (e: FormEvent) => void
}) {
  function handleKeyDown(e: KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      onSubmit(e as unknown as FormEvent)
    }
  }

  return (
    <div className={styles.chatPanel}>
      <div className={styles.chatHeader}>
        <Mascot id={business.mascot} color={business.themeColor} size={36} />
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
            <div
              className={`${styles.bubble} ${
                m.role === 'user' ? styles.bubbleUser : styles.bubbleAssistant
              }`}
              style={m.role === 'user' ? { background: business.themeColor } : undefined}
            >
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

      <form className={styles.composer} onSubmit={onSubmit}>
        <textarea
          className={styles.composerInput}
          rows={1}
          placeholder="輸入訊息……"
          value={input}
          onChange={(e) => onInputChange(e.target.value)}
          onKeyDown={handleKeyDown}
        />
        <button
          type="submit"
          className={styles.sendButton}
          style={{ background: business.themeColor }}
          disabled={!input.trim()}
        >
          ➤
        </button>
      </form>
      <span className={styles.footerNote}>由 {business.name} 提供的 AI 小幫手回答</span>
    </div>
  )
}
