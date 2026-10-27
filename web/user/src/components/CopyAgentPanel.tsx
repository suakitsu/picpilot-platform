'use client'

import { FormEvent, useEffect, useMemo, useRef, useState } from 'react'
import { ExternalLink, Send, Sparkles, Trash2 } from 'lucide-react'
import { currentLanguage, useI18n } from '@/components/i18n'
import { chatWithAssistant, getAssistantStatus, getToken, type AssistantChatMessage, type AssistantStatus } from '@/lib/api'
import { copyTemplateSeed, getTemplateById } from '@/lib/templateCatalog'

type CopyAgentMessage = {
  id: string
  role: 'user' | 'assistant'
  content: string
  pending?: boolean
  actionLabel?: string
  actionHref?: string
}

const STORAGE_KEY = 'picpilot_copy_agent_messages_v1'
const MODEL_STORAGE_KEY = 'picpilot_copy_agent_model_v1'
const MAX_MESSAGES = 30
const MAX_SEND_MESSAGES = 12

function uid() {
  return `copy_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`
}

function safeInternalHref(href?: string) {
  const value = (href || '').trim()
  if (!value || !value.startsWith('/') || value.startsWith('//')) return ''
  return value
}

function modelLabel(model: string) {
  return model
}

export function CopyAgentPanel() {
  const t = useI18n()
  const [status, setStatus] = useState<AssistantStatus | null>(null)
  const [messages, setMessages] = useState<CopyAgentMessage[]>([])
  const [selectedModel, setSelectedModel] = useState('')
  const [input, setInput] = useState('')
  const [loading, setLoading] = useState(false)
  const [message, setMessage] = useState('')
  const listRef = useRef<HTMLDivElement | null>(null)

  const availableModels = (status?.models?.length ? status.models : [status?.model || '']).filter(Boolean)
  const enabled = Boolean(status?.enabled) && availableModels.includes(selectedModel)
  const visibleMessages = messages.length ? messages : [{
    id: 'welcome',
    role: 'assistant' as const,
    content: '我是文案 Agent。你可以让我写 Amazon Listing、国内电商卖点、小红书种草文案，或者只生成可复制的生图提示词。'
  }]
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
        const parsed = JSON.parse(raw) as CopyAgentMessage[]
        setMessages(Array.isArray(parsed) ? parsed.slice(-MAX_MESSAGES) : [])
      }
    } catch {
      setMessages([])
    }
    const template = getTemplateById(new URLSearchParams(window.location.search).get('templateId'))
    if (template?.routeType === 'copy') {
      const seed = copyTemplateSeed(template)
      if (seed) {
        setInput(seed)
        setMessage(`已套用模板：${template.title}。补全商品信息后发送即可。`)
      }
    }
  }, [])

  useEffect(() => {
    let active = true
    getAssistantStatus().then(next => {
      if (!active) return
      setStatus(next)
      const models = (next.models?.length ? next.models : [next.model]).filter(Boolean)
      const savedModel = window.localStorage.getItem(MODEL_STORAGE_KEY) || ''
      setSelectedModel(models.includes(savedModel) ? savedModel : models[0] || '')
    }).catch(cause => {
      if (!active) return
      setStatus({ enabled: false, model: '' })
      setMessage(cause instanceof Error ? cause.message : t('providersLoadFailed'))
    })
    return () => { active = false }
  }, [t])

  useEffect(() => {
    if (selectedModel) window.localStorage.setItem(MODEL_STORAGE_KEY, selectedModel)
  }, [selectedModel])

  useEffect(() => {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(messages.filter(item => !item.pending).slice(-MAX_MESSAGES)))
    window.requestAnimationFrame(() => {
      const el = listRef.current
      if (el) el.scrollTo({ top: el.scrollHeight, behavior: 'smooth' })
    })
  }, [messages])

  async function send(event?: FormEvent) {
    event?.preventDefault()
    const text = input.trim().slice(0, 3000)
    if (!text || loading) return
    if (!getToken()) {
      const next = `${window.location.pathname}${window.location.search}`
      window.location.href = `/login?next=${encodeURIComponent(next)}`
      return
    }
    if (!enabled) {
      setMessage(t('assistantUnavailable'))
      return
    }
    const userMessage: CopyAgentMessage = { id: uid(), role: 'user', content: text }
    const pendingMessage: CopyAgentMessage = { id: uid(), role: 'assistant', content: '思考中', pending: true }
    setMessages(rows => [...rows.filter(item => !item.pending), userMessage, pendingMessage].slice(-MAX_MESSAGES))
    setInput('')
    setMessage('')
    setLoading(true)
    try {
      const response = await chatWithAssistant({
        messages: [...apiMessages, { role: 'user', content: text }],
        locale: currentLanguage(),
        model: selectedModel,
        pageContext: {
          pathname: '/ai-create',
          tab: 'copy',
          task: 'copywriting_agent',
          logged_in: true
        }
      })
      const actionHref = safeInternalHref(response.action_href)
      const assistantMessage: CopyAgentMessage = {
        id: uid(),
        role: 'assistant',
        content: response.reply || '可以，继续把商品信息发我。',
        actionLabel: actionHref ? (response.action_label || '打开') : '',
        actionHref
      }
      setMessages(rows => [...rows.filter(item => !item.pending), assistantMessage].slice(-MAX_MESSAGES))
    } catch (error) {
      const errorMessage: CopyAgentMessage = {
        id: uid(),
        role: 'assistant',
        content: error instanceof Error ? error.message : '文案 Agent 暂时不可用，请稍后再试。'
      }
      setMessages(rows => [...rows.filter(item => !item.pending), errorMessage].slice(-MAX_MESSAGES))
    } finally {
      setLoading(false)
    }
  }

  function clearMessages() {
    setMessages([])
    window.localStorage.removeItem(STORAGE_KEY)
  }

  return (
    <section className="panel copy-agent-panel">
      <div className="panel-head">
        <div>
          <h2>文案 Agent</h2>
          <p>{t('copyAgentDescription')}</p>
        </div>
        <div className="copy-agent-head-actions">
          <label className="copy-agent-model">
            <span>模型</span>
            <select value={selectedModel} disabled={loading || !availableModels.length} onChange={event => setSelectedModel(event.target.value)}>
              {!availableModels.length ? <option value="">{t('providersEmpty')}</option> : null}
              {availableModels.map(model => <option key={model} value={model}>{modelLabel(model)}</option>)}
            </select>
          </label>
          <button className="btn btn-secondary" type="button" onClick={clearMessages}><Trash2 size={15} />清空对话</button>
        </div>
      </div>
      {message ? <div className="notice notice-error">{message}</div> : null}
      <div className="copy-agent-body">
        <div className="copy-agent-thread" ref={listRef}>
          {visibleMessages.map(item => (
            <article className={`copy-agent-message copy-agent-message-${item.role}`} key={item.id}>
              {item.role === 'assistant' ? <span className="copy-agent-avatar"><Sparkles size={16} /></span> : null}
              <div className={item.pending ? 'copy-agent-bubble copy-agent-thinking' : 'copy-agent-bubble'}>
                {item.pending ? <span className="copy-agent-dots"><i /><i /><i /></span> : <p>{item.content}</p>}
                {item.actionHref ? <a className="btn btn-primary copy-agent-action" href={item.actionHref}>{item.actionLabel || '打开'}<ExternalLink size={14} /></a> : null}
              </div>
            </article>
          ))}
        </div>
        <form className="copy-agent-composer" onSubmit={send}>
          <textarea
            value={input}
            maxLength={3000}
            disabled={loading || !enabled}
            onChange={event => setInput(event.target.value)}
            onKeyDown={event => {
              if (event.key === 'Enter' && !event.shiftKey) {
                event.preventDefault()
                send()
              }
            }}
            placeholder="直接输入：商品信息、平台、目标用户、想要的文案类型，或让它生成生图提示词。"
          />
          <button className="btn btn-primary" type="submit" disabled={!input.trim() || loading || !enabled}><Send size={16} />{loading ? '思考中...' : '发送'}</button>
        </form>
      </div>
    </section>
  )
}
