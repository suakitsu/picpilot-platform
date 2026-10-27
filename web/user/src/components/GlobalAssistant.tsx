'use client'

import { FormEvent, PointerEvent as ReactPointerEvent, useEffect, useMemo, useRef, useState } from 'react'
import { usePathname, useRouter } from 'next/navigation'
import { Bot, ExternalLink, ImagePlus, MessageCircle, Send, Trash2, X } from 'lucide-react'
import { chatWithAssistant, getAssistantStatus, getToken, type AssistantChatMessage, type AssistantStatus } from '@/lib/api'
import { currentLanguage } from '@/components/i18n'

type AssistantBubble = {
  id: string
  role: 'user' | 'assistant'
  content: string
  actionLabel?: string
  actionHref?: string
  pending?: boolean
}

const STORAGE_KEY = 'picpilot_global_assistant_messages_v1'
const POSITION_STORAGE_KEY = 'picpilot_global_assistant_position_v1'
const MAX_LOCAL_MESSAGES = 30
const MAX_SEND_MESSAGES = 12

type AssistantPosition = {
  x: number
  y: number
}

type AssistantDrag = {
  pointerId: number
  startX: number
  startY: number
  originX: number
  originY: number
  width: number
  height: number
  lastX: number
  lastY: number
  moved: boolean
}

function uid() {
  return `ga_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`
}

function defaultGreeting() {
  return '我在。你可以直接让我写 Listing 文案、卖点或生图提示词；想生成图片时，也可以一键打开生图助手确认并提交。'
}

function safeInternalHref(href?: string) {
  const value = (href || '').trim()
  if (!value || !value.startsWith('/') || value.startsWith('//')) return ''
  return value
}

export function GlobalAssistant() {
  const router = useRouter()
  const pathname = usePathname()
  const [open, setOpen] = useState(false)
  const [status, setStatus] = useState<AssistantStatus | null>(null)
  const [statusLoaded, setStatusLoaded] = useState(false)
  const [messages, setMessages] = useState<AssistantBubble[]>([])
  const [input, setInput] = useState('')
  const [loading, setLoading] = useState(false)
  const [position, setPosition] = useState<AssistantPosition | null>(null)
  const [dragging, setDragging] = useState(false)
  const listRef = useRef<HTMLDivElement | null>(null)
  const floatRef = useRef<HTMLButtonElement | null>(null)
  const dragRef = useRef<AssistantDrag | null>(null)
  const ignoreClickRef = useRef(false)

  const enabled = Boolean(status?.enabled)
  const hasMessages = messages.length > 0
  const visibleMessages = hasMessages ? messages : [{ id: 'welcome', role: 'assistant' as const, content: defaultGreeting() }]
  const canSend = input.trim().length > 0 && !loading && enabled

  const apiMessages = useMemo<AssistantChatMessage[]>(() => {
    return messages
      .filter(item => !item.pending && item.content.trim())
      .slice(-MAX_SEND_MESSAGES)
      .map(item => ({ role: item.role, content: item.content }))
  }, [messages])

  useEffect(() => {
    try {
      const raw = window.localStorage.getItem(STORAGE_KEY)
      if (raw) {
        const parsed = JSON.parse(raw) as AssistantBubble[]
        setMessages(Array.isArray(parsed) ? parsed.slice(-MAX_LOCAL_MESSAGES) : [])
      }
    } catch {
      setMessages([])
    }
  }, [])

  useEffect(() => {
    getAssistantStatus()
      .then(setStatus)
      .catch(() => setStatus({ enabled: false, model: '' }))
      .finally(() => setStatusLoaded(true))
  }, [])

  useEffect(() => {
    if (!statusLoaded) return
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(messages.filter(item => !item.pending).slice(-MAX_LOCAL_MESSAGES)))
  }, [messages, statusLoaded])

  useEffect(() => {
    if (!open) return
    window.requestAnimationFrame(() => {
      const el = listRef.current
      if (el) el.scrollTo({ top: el.scrollHeight, behavior: 'smooth' })
    })
  }, [messages, open])

  useEffect(() => {
    function clampPosition(nextX: number, nextY: number, width: number, height: number) {
      const sideInset = 10
      const topInset = window.innerWidth <= 960 ? 66 : 10
      const bottomInset = window.innerWidth <= 960 ? 78 : 10
      return {
        x: Math.min(Math.max(nextX, sideInset), Math.max(sideInset, window.innerWidth - width - sideInset)),
        y: Math.min(Math.max(nextY, topInset), Math.max(topInset, window.innerHeight - height - bottomInset))
      }
    }

    function restorePosition() {
      const button = floatRef.current
      if (!button) return
      try {
        const raw = window.localStorage.getItem(POSITION_STORAGE_KEY)
        if (!raw) return
        const saved = JSON.parse(raw) as AssistantPosition & { viewportWidth?: number; viewportHeight?: number }
        if (!Number.isFinite(saved.x) || !Number.isFinite(saved.y)) return
        const scaledX = saved.viewportWidth ? saved.x * window.innerWidth / saved.viewportWidth : saved.x
        const scaledY = saved.viewportHeight ? saved.y * window.innerHeight / saved.viewportHeight : saved.y
        setPosition(clampPosition(scaledX, scaledY, button.offsetWidth, button.offsetHeight))
      } catch {
        window.localStorage.removeItem(POSITION_STORAGE_KEY)
      }
    }

    function keepPositionVisible() {
      const button = floatRef.current
      if (!button) return
      setPosition(current => current ? clampPosition(current.x, current.y, button.offsetWidth, button.offsetHeight) : current)
    }

    restorePosition()
    window.addEventListener('resize', keepPositionVisible)
    return () => window.removeEventListener('resize', keepPositionVisible)
  }, [])

  async function send(event?: FormEvent) {
    event?.preventDefault()
    const text = input.trim().slice(0, 3000)
    if (!text || loading) return
    if (!getToken()) {
      const next = `${window.location.pathname}${window.location.search}`
      router.push(`/login?next=${encodeURIComponent(next)}`)
      return
    }
    if (!enabled) {
      const disabledMessage: AssistantBubble = { id: uid(), role: 'assistant', content: 'AI 助手暂未启用，请稍后再试。' }
      setMessages(prev => [...prev, disabledMessage].slice(-MAX_LOCAL_MESSAGES))
      return
    }
    const userMessage: AssistantBubble = { id: uid(), role: 'user', content: text }
    const pendingMessage: AssistantBubble = { id: uid(), role: 'assistant', content: '思考中', pending: true }
    const nextMessages = [...messages.filter(item => !item.pending), userMessage]
    setMessages([...nextMessages, pendingMessage].slice(-MAX_LOCAL_MESSAGES))
    setInput('')
    setLoading(true)
    try {
      const response = await chatWithAssistant({
        messages: [...apiMessages, { role: 'user', content: text }],
        locale: currentLanguage(),
        pageContext: {
          pathname,
          search: window.location.search,
          title: document.title,
          logged_in: Boolean(getToken())
        }
      })
      const actionHref = safeInternalHref(response.action_href)
      const assistantMessage: AssistantBubble = {
        id: uid(),
        role: 'assistant',
        content: response.reply || '我在，你可以继续说。',
        actionLabel: actionHref ? (response.action_label || '打开') : '',
        actionHref
      }
      setMessages(prev => [
        ...prev.filter(item => !item.pending),
        assistantMessage
      ].slice(-MAX_LOCAL_MESSAGES))
    } catch (error) {
      const errorMessage: AssistantBubble = {
        id: uid(),
        role: 'assistant',
        content: error instanceof Error ? error.message : 'AI 助手暂时不可用，请稍后再试。'
      }
      setMessages(prev => [
        ...prev.filter(item => !item.pending),
        errorMessage
      ].slice(-MAX_LOCAL_MESSAGES))
    } finally {
      setLoading(false)
    }
  }

  function openAction(href: string) {
    const safeHref = safeInternalHref(href)
    if (!safeHref) return
    window.location.href = safeHref
    setOpen(false)
  }

  function openImageAgent() {
    const prompt = input.trim()
    openAction(prompt ? `/ai-create?tab=agent&prompt=${encodeURIComponent(prompt)}` : '/ai-create?tab=agent')
  }

  function clearChat() {
    setMessages([])
    window.localStorage.removeItem(STORAGE_KEY)
  }

  function clampDragPosition(nextX: number, nextY: number, width: number, height: number) {
    const sideInset = 10
    const topInset = window.innerWidth <= 960 ? 66 : 10
    const bottomInset = window.innerWidth <= 960 ? 78 : 10
    return {
      x: Math.min(Math.max(nextX, sideInset), Math.max(sideInset, window.innerWidth - width - sideInset)),
      y: Math.min(Math.max(nextY, topInset), Math.max(topInset, window.innerHeight - height - bottomInset))
    }
  }

  function startDrag(event: ReactPointerEvent<HTMLButtonElement>) {
    if (event.button !== 0) return
    const rect = event.currentTarget.getBoundingClientRect()
    dragRef.current = {
      pointerId: event.pointerId,
      startX: event.clientX,
      startY: event.clientY,
      originX: rect.left,
      originY: rect.top,
      width: rect.width,
      height: rect.height,
      lastX: rect.left,
      lastY: rect.top,
      moved: false
    }
    event.currentTarget.setPointerCapture(event.pointerId)
  }

  function moveDrag(event: ReactPointerEvent<HTMLButtonElement>) {
    const drag = dragRef.current
    if (!drag || drag.pointerId !== event.pointerId) return
    const deltaX = event.clientX - drag.startX
    const deltaY = event.clientY - drag.startY
    if (!drag.moved && Math.hypot(deltaX, deltaY) < 5) return
    drag.moved = true
    const next = clampDragPosition(drag.originX + deltaX, drag.originY + deltaY, drag.width, drag.height)
    drag.lastX = next.x
    drag.lastY = next.y
    setDragging(true)
    setPosition(next)
  }

  function finishDrag(event: ReactPointerEvent<HTMLButtonElement>) {
    const drag = dragRef.current
    if (!drag || drag.pointerId !== event.pointerId) return
    if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId)
    if (drag.moved) {
      ignoreClickRef.current = true
      window.localStorage.setItem(POSITION_STORAGE_KEY, JSON.stringify({
        x: drag.lastX,
        y: drag.lastY,
        viewportWidth: window.innerWidth,
        viewportHeight: window.innerHeight
      }))
      window.setTimeout(() => { ignoreClickRef.current = false }, 0)
    }
    dragRef.current = null
    setDragging(false)
  }

  function toggleAssistant() {
    if (ignoreClickRef.current) return
    setOpen(value => !value)
  }

  const anchorLeft = position ? position.x < window.innerWidth / 2 : false
  const anchorBelow = position ? position.y < window.innerHeight / 2 : false
  const assistantClassName = [
    'global-assistant',
    open ? 'global-assistant-open' : '',
    position ? 'global-assistant-positioned' : '',
    anchorLeft ? 'global-assistant-anchor-left' : '',
    anchorBelow ? 'global-assistant-anchor-below' : '',
    dragging ? 'is-dragging' : ''
  ].filter(Boolean).join(' ')

  return (
    <div className={assistantClassName} style={position ? { left: position.x, top: position.y, right: 'auto', bottom: 'auto' } : undefined}>
      {open ? (
        <section className="global-assistant-panel" aria-label="PicPilot AI 助手">
          <header className="global-assistant-head">
            <div className="global-assistant-title">
              <span className="global-assistant-mark"><Bot size={18} /></span>
              <div>
                <strong>PicPilot 助手</strong>
                <small>{enabled ? '在线' : statusLoaded ? '暂未启用' : '连接中'}</small>
              </div>
            </div>
            <div className="global-assistant-head-actions">
              <button className="icon-button" type="button" aria-label="打开生图助手" title="打开生图助手" onClick={openImageAgent}>
                <ImagePlus size={16} />
              </button>
              <button className="icon-button" type="button" aria-label="清空对话" title="清空对话" onClick={clearChat}>
                <Trash2 size={15} />
              </button>
              <button className="icon-button" type="button" aria-label="关闭助手" onClick={() => setOpen(false)}>
                <X size={17} />
              </button>
            </div>
          </header>
          <div className="global-assistant-messages" ref={listRef}>
            {visibleMessages.map(item => (
              <article className={`global-assistant-bubble-row global-assistant-bubble-${item.role}`} key={item.id}>
                {item.role === 'assistant' ? <span className="global-assistant-avatar"><Bot size={16} /></span> : null}
                <div className={item.pending ? 'global-assistant-bubble global-assistant-thinking' : 'global-assistant-bubble'}>
                  {item.pending ? (
                    <span className="global-assistant-dots"><i /><i /><i /></span>
                  ) : (
                    <>
                      <p>{item.content}</p>
                      {item.actionHref ? (
                        <button className="btn btn-primary global-assistant-action" type="button" onClick={() => openAction(item.actionHref || '')}>
                          {item.actionLabel || '打开'}<ExternalLink size={14} />
                        </button>
                      ) : null}
                    </>
                  )}
                </div>
              </article>
            ))}
          </div>
          {!enabled && statusLoaded ? (
            <div className="global-assistant-disabled">管理员暂未启用 AI 助手。</div>
          ) : null}
          <form className="global-assistant-composer" onSubmit={send}>
            <textarea
              value={input}
              maxLength={3000}
              disabled={!enabled || loading}
              onChange={event => setInput(event.target.value)}
              onKeyDown={event => {
                if (event.key === 'Enter' && !event.shiftKey) {
                  event.preventDefault()
                  send()
                }
              }}
              placeholder="直接写需求：文案、卖点、提示词，或问我怎么操作..."
            />
            <button className="global-assistant-send" type="submit" disabled={!canSend} aria-label="发送">
              <Send size={17} />
            </button>
          </form>
        </section>
      ) : null}
      <button
        ref={floatRef}
        className="global-assistant-float"
        type="button"
        aria-label="打开 PicPilot AI 助手"
        title="拖动调整位置，点击打开 AI 助手"
        onClick={toggleAssistant}
        onPointerDown={startDrag}
        onPointerMove={moveDrag}
        onPointerUp={finishDrag}
        onPointerCancel={finishDrag}
      >
        <MessageCircle size={21} />
        <span>AI 助手</span>
      </button>
    </div>
  )
}
