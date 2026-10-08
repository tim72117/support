import { useCallback, useEffect, useRef, useState } from 'react'
import { AgentBridge, defineTool } from '@onagent/bridge'
import { ApiError, fetchSection, fetchSections, postChat, postReply, type ChatResult, type PublicBusiness } from './api.ts'
import { trackEvent, trackPageView } from './analytics.ts'

export interface ChatMessage {
  id: string
  role: 'user' | 'assistant'
  text: string
}

/** Why the chat cannot currently go on; shown as a notice above the composer. */
export type ChatNotice = { kind: 'unavailable' | 'error'; text: string }

const UNAVAILABLE_TEXT = '目前無法服務，請稍後再試。'
const REPLY_TIMEOUT_MS = 60_000

interface Pending {
  conversationId: string
  messageId: number
}

// Real conversation engine. One turn is:
//   1. POST the visitor's text to the ai-support backend (it records the
//      message and checks the owner's quota);
//   2. only with what the backend returned, forward it to onagent through
//      @onagent/bridge (the browser's own connection);
//   3. show onagent's reply when it arrives (the bridge delivers it whole —
//      there is no token streaming in its protocol);
//   4. report the reply back to the backend so it is stored and billed.
//
// Nothing touches onagent until the visitor sends their first message: the
// bridge is not even constructed before that (and is built lazyConnect), so
// merely opening the page or pressing "start" opens no WebSocket.
export function useChat(business: PublicBusiness, greeting: string) {
  const [started, setStarted] = useState(false)
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [isTyping, setIsTyping] = useState(false)
  const [notice, setNotice] = useState<ChatNotice | null>(null)

  const nextId = useRef(0)
  const bridgeRef = useRef<AgentBridge | null>(null)
  const conversationIdRef = useRef<string | undefined>(undefined)
  const pendingRef = useRef<Pending | null>(null)
  const busyRef = useRef(false)
  const startedRef = useRef(false)
  const timeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const slug = business.slug

  const addMessage = useCallback((role: ChatMessage['role'], text: string) => {
    setMessages((prev) => [...prev, { id: `m${nextId.current++}`, role, text }])
  }, [])

  const clearReplyTimer = useCallback(() => {
    if (timeoutRef.current) clearTimeout(timeoutRef.current)
    timeoutRef.current = null
  }, [])

  // Ends the in-flight turn (reply, error or timeout) and lets the visitor
  // send again.
  const finishTurn = useCallback(() => {
    clearReplyTimer()
    pendingRef.current = null
    busyRef.current = false
    setIsTyping(false)
  }, [clearReplyTimer])

  const failTurn = useCallback(
    (text: string, kind: ChatNotice['kind'] = 'error') => {
      finishTurn()
      setNotice({ kind, text })
    },
    [finishTurn],
  )

  // Tool handlers for onagent's list_sections / read_section (see
  // backend/onagent-tools/*.yaml for their definitions as registered with
  // onagent, and backend/internal/public/public.go for the two endpoints
  // these call). Tool execution genuinely happens here, in the visitor's
  // browser — onagent's backend only relays the call over the WebSocket —
  // so the business's content never has to pass through onagent as a system
  // prompt; it's fetched from ai-support's own backend on demand, scoped by
  // a business slug exactly like every other /public/* call.
  //
  // Every business's consumer page now shares the same onagent app (see
  // backend/cmd/server/main.go's ONAGENT_APP_ID/ONAGENT_APP_KEY), so the
  // app itself no longer identifies which business a tool call is about —
  // the tool definitions require a businessSlug argument instead (see the
  // YAML files). This page only ever serves one business (the one it was
  // opened for), so businessSlug is validated against that page's own slug
  // rather than trusted blindly: if the model ever passes a different slug
  // (a bug, or a prompt-injection attempt from section content elsewhere),
  // the call is rejected instead of silently fetching another business's
  // data through this page's connection.
  //
  // parseArgs intentionally returns the sectionId with no further
  // validation beyond "is it a string": the backend endpoint is the source
  // of truth for whether an id exists, and a bad id just becomes a normal
  // 404 surfaced as a tool error, same as any other handler failure.
  const requireOwnSlug = useCallback(
    (raw: unknown): void => {
      const businessSlug = (raw as { businessSlug?: unknown } | null)?.businessSlug
      if (typeof businessSlug !== 'string' || !businessSlug) {
        throw new Error('businessSlug is required')
      }
      if (businessSlug !== slug) {
        throw new Error('businessSlug does not match this conversation')
      }
    },
    [slug],
  )

  const makeTools = useCallback(
    () => [
      defineTool(
        'list_sections',
        (raw: unknown) => {
          requireOwnSlug(raw)
          return {}
        },
        () => fetchSections(slug),
      ),
      defineTool(
        'read_section',
        (raw: unknown) => {
          requireOwnSlug(raw)
          const sectionId = (raw as { sectionId?: unknown } | null)?.sectionId
          if (typeof sectionId !== 'string' || !sectionId) {
            throw new Error('sectionId is required')
          }
          return { sectionId }
        },
        ({ sectionId }) => fetchSection(slug, sectionId),
      ),
    ],
    [slug, requireOwnSlug],
  )

  const ensureBridge = useCallback((): AgentBridge => {
    if (bridgeRef.current) return bridgeRef.current
    const { wsUrl, appId, apiKey } = business.chat
    const bridge = new AgentBridge({
      url: wsUrl!,
      appId: appId!,
      apiKey,
      lazyConnect: true,
      tools: makeTools(),
      onAssistantMessage: (text) => {
        const pending = pendingRef.current
        addMessage('assistant', text)
        finishTurn()
        if (pending) {
          // Fire and forget: failing to record must not disturb the visitor.
          postReply(slug, { ...pending, content: text }).catch((err) => {
            console.warn('[support] could not report reply', err instanceof ApiError ? err.code : err)
          })
        }
      },
      onQuotaExceeded: () => failTurn(UNAVAILABLE_TEXT, 'unavailable'),
      onError: (err) => {
        console.warn('[support] onagent error', err.code ?? '', err.message)
        if (err.code === 'prompt_too_long') failTurn('訊息太長了，請縮短後再送出。')
        else failTurn('抱歉，AI 暫時沒辦法回覆，請稍後再試。')
      },
    })
    bridgeRef.current = bridge
    return bridge
  }, [business.chat, makeTools, slug, addMessage, finishTurn, failTurn])

  const start = useCallback(() => {
    if (startedRef.current) return
    startedRef.current = true
    setStarted(true)
    setMessages([{ id: `m${nextId.current++}`, role: 'assistant', text: greeting }])
    trackEvent('chat_start', { business: slug })
    trackPageView(`/support/${slug}/chat`, '對話') // the chat screen has no URL of its own
  }, [greeting, slug])

  const send = useCallback(
    async (text: string) => {
      const trimmed = text.trim()
      if (!trimmed || busyRef.current) return
      busyRef.current = true
      setNotice(null)
      addMessage('user', trimmed)
      setIsTyping(true)

      let result: ChatResult
      try {
        result = await postChat(slug, { conversationId: conversationIdRef.current, content: trimmed })
      } catch (err) {
        if (err instanceof ApiError) {
          switch (err.code) {
            case 'quota_exceeded':
            case 'chat_unavailable':
              return failTurn(UNAVAILABLE_TEXT, 'unavailable')
            case 'rate_limited':
              return failTurn('訊息送得太快了，請稍等一下再試。')
            case 'content_too_long':
              return failTurn('訊息太長了，請縮短後再送出。')
            case 'conversation_full':
              conversationIdRef.current = undefined
              return failTurn('這段對話已達上限，下一則訊息會開啟新的對話。')
            case 'conversation_not_found':
              conversationIdRef.current = undefined
              return failTurn('對話已失效，請再送出一次。')
          }
        }
        return failTurn('訊息沒有送出，請檢查網路後再試一次。')
      }

      conversationIdRef.current = result.conversationId
      trackEvent('chat_message', { business: slug }) // never the text itself
      pendingRef.current = { conversationId: result.conversationId, messageId: result.messageId }
      timeoutRef.current = setTimeout(() => failTurn('等太久了，AI 沒有回覆，請再試一次。'), REPLY_TIMEOUT_MS)
      try {
        // Forward what the backend returned (not the raw input) to onagent.
        ensureBridge().prompt(result.content)
      } catch {
        failTurn('抱歉，AI 暫時沒辦法回覆，請稍後再試。')
      }
    },
    [slug, addMessage, ensureBridge, failTurn],
  )

  // Tear the onagent connection down with the page.
  useEffect(() => {
    return () => {
      if (timeoutRef.current) clearTimeout(timeoutRef.current)
      bridgeRef.current?.close()
      bridgeRef.current = null
    }
  }, [])

  return { started, messages, isTyping, notice, start, send }
}
