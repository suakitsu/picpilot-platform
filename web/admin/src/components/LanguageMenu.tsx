'use client'

import { useEffect, useRef, useState } from 'react'
import { Check, Languages } from 'lucide-react'
import { getStoredUser, saveStoredUser, updateMe } from '@/lib/api'
import { useI18n } from '@/components/i18n'

const languages = [
  { value: 'zh-CN', short: 'CN', labelKey: 'languageChinese' },
  { value: 'en-US', short: 'US', labelKey: 'languageEnglish' },
  { value: 'ja-JP', short: 'JP', labelKey: 'languageJapanese' }
]

export function LanguageMenu() {
  const [open, setOpen] = useState(false)
  const [value, setValue] = useState('zh-CN')
  const menuRef = useRef<HTMLDivElement | null>(null)
  const t = useI18n()

  useEffect(() => {
    const sync = () => {
      const next = localStorage.getItem('openimage-language') || getStoredUser()?.ui_language || 'zh-CN'
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
      // keep local preference even if the profile request fails
    }
  }

  const current = languages.find(item => item.value === value) || languages[0]
  return (
    <div className="quick-menu" ref={menuRef}>
      <button className="icon-button labeled-icon-button language-trigger" type="button" onClick={() => setOpen(!open)} aria-label={t('switchLanguage')} title={t('switchLanguage')}>
        <Languages size={16} /><span className="tool-label">{t('language')}</span><span className="tool-code">{current.short}</span>
      </button>
      {open ? (
        <div className="quick-menu-popover">
          {languages.map(item => (
            <button key={item.value} className="quick-menu-item" type="button" onClick={() => selectLanguage(item.value)}>
              <span>{item.short}</span><strong>{t(item.labelKey)}</strong>{value === item.value ? <Check size={14} /> : null}
            </button>
          ))}
        </div>
      ) : null}
    </div>
  )
}
