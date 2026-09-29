import { useCallback, useRef, useState } from 'react'

export interface ChatMessage {
  id: string
  role: 'user' | 'assistant'
  text: string
}

// Fake conversation engine for this demo page. Real integration replaces
// this hook's guts with @onagent/bridge (see docs/refactor-initial-
// scaffold-plan-2026-09-27.md item 3) — the important behavior to keep is
// that "connecting" only happens in response to the user's own action
// (start()), never on mount, per the known pitfall about not opening a
// WebSocket before the user has actually asked to chat.
export function useDemoChat(greeting: string) {
  const [started, setStarted] = useState(false)
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [isTyping, setIsTyping] = useState(false)
  const nextId = useRef(0)

  const start = useCallback(() => {
    setStarted(true)
    setMessages([{ id: `m${nextId.current++}`, role: 'assistant', text: greeting }])
  }, [greeting])

  const send = useCallback((text: string) => {
    const trimmed = text.trim()
    if (!trimmed) return
    setMessages((prev) => [...prev, { id: `m${nextId.current++}`, role: 'user', text: trimmed }])
    setIsTyping(true)
    const replyDelay = 700 + Math.random() * 500
    setTimeout(() => {
      setIsTyping(false)
      setMessages((prev) => [
        ...prev,
        {
          id: `m${nextId.current++}`,
          role: 'assistant',
          text: '這是示範頁面，還沒有連上真正的 AI —— 正式上線後，這裡會由業主設定的內容來回答你的問題。',
        },
      ])
    }, replyDelay)
  }, [])

  return { started, messages, isTyping, start, send }
}
