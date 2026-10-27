'use client'

import { useEffect, useRef, useState } from 'react'
import { Check, ChevronDown, Languages } from 'lucide-react'
import { getStoredUser, saveStoredUser, updateMe } from '@/lib/api'
import { useI18n } from '@/components/i18n'

const languages = [
  { value: 'zh-CN', short: '中', label: '简体中文', description: 'Chinese' },
  { value: 'en-US', short: 'EN', label: 'English', description: 'English' },
  { value: 'ja-JP', short: '日', label: '日本語', description: 'Japanese' }
]

export function LanguageMenu() {
  const [open, setOpen] = useState(false)
  const [value, setValue] = useState('zh-CN')
  const menuRef = useRef<HTMLDivElement | null>(null)
  const t = useI18n()

  useEffect(() => {
    const sync = () => {
      const saved = localStorage.getItem('openimage-language') || getStoredUser()?.ui_language || 'zh-CN'
      const next = languages.some(item => item.value === saved) ? saved : 'zh-CN'
      setValue(next)
      document.documentElement.lang = next
    }
    sync()
    window.addEventListener('openimage:language-updated', sync)
    window.addEventListener('storage', sync)
    return () => {
      window.removeEventListener('openimage:language-updated', sync)
      window.removeEventListener('storage', sync)
    }
  }, [])

  useEffect(() => {
    if (!open) return
    function closeOnOutside(event: PointerEvent) {
      if (menuRef.current && !menuRef.current.contains(event.target as Node)) setOpen(false)
    }
    function closeOnEsc(event: KeyboardEvent) {
      if (event.key === 'Escape') setOpen(false)
    }
    document.addEventListener('pointerdown', closeOnOutside)
    document.addEventListener('keydown', closeOnEsc)
    return () => {
      document.removeEventListener('pointerdown', closeOnOutside)
      document.removeEventListener('keydown', closeOnEsc)
    }
  }, [open])

  async function selectLanguage(next: string) {
    setValue(next)
    localStorage.setItem('openimage-language', next)
    document.documentElement.lang = next
    window.dispatchEvent(new Event('openimage:language-updated'))
    setOpen(false)
    const user = getStoredUser()
    if (!user) return
    try {
      const updated = await updateMe({ nickname: user.nickname, uiLanguage: next, theme: user.theme || 'system' })
      saveStoredUser(updated)
    } catch {
      // local preference is still useful when the account update fails
    }
  }

  const current = languages.find(item => item.value === value) || languages[0]
  return (
    <div className="quick-menu" ref={menuRef}>
      <button
        className={`icon-button labeled-icon-button language-trigger${open ? ' language-trigger-open' : ''}`}
        type="button"
        onClick={() => setOpen(!open)}
        aria-label={t('switchLanguage')}
        title={t('switchLanguage')}
        aria-haspopup="menu"
        aria-expanded={open}
      >
        <Languages size={16} />
        <span className="language-current">{current.label}</span>
        <ChevronDown className="language-chevron" size={14} />
      </button>
      {open ? (
        <div className="quick-menu-popover language-popover" role="menu" aria-label={t('switchLanguage')}>
          {languages.map(item => (
            <button
              key={item.value}
              className={`quick-menu-item language-option${value === item.value ? ' language-option-active' : ''}`}
              type="button"
              role="menuitemradio"
              aria-checked={value === item.value}
              onClick={() => selectLanguage(item.value)}
            >
              <span className="language-option-code">{item.short}</span>
              <span className="language-option-copy"><strong>{item.label}</strong><small>{item.description}</small></span>
              <span className="language-option-check">{value === item.value ? <Check size={15} /> : null}</span>
            </button>
          ))}
        </div>
      ) : null}
    </div>
  )
}
