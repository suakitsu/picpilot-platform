'use client'

import { useEffect, useState } from 'react'
import { getMe, getStoredUser, saveStoredUser, updateMe, uploadAvatar, type UserProfile } from '@/lib/api'
import { useI18n } from '@/components/i18n'
import { applyThemePreference } from '@/components/ThemeToggle'
import { PasswordChangeForm } from '@/components/PasswordChangeForm'

type SaveState = 'idle' | 'saving' | 'success' | 'error'

function languageLabel(value: string | undefined, t: (key: string) => string) {
  if (value === 'en-US') return t('languageEnglish')
  if (value === 'ja-JP') return t('languageJapanese')
  return t('languageChinese')
}

function themeLabel(value: string | undefined, t: (key: string) => string) {
  if (value === 'light') return t('light')
  if (value === 'dark') return t('dark')
  return t('system')
}

export default function AdminProfilePage() {
  const [user, setUser] = useState<UserProfile | null>(null)
  const [nickname, setNickname] = useState('')
  const [uiLanguage, setUiLanguage] = useState('zh-CN')
  const [theme, setTheme] = useState('system')
  const [saveState, setSaveState] = useState<SaveState>('idle')
  const [message, setMessage] = useState('')
  const t = useI18n()

  useEffect(() => {
    const cached = getStoredUser()
    if (cached) syncUser(cached)
    getMe().then(syncUser).catch(() => {})
  }, [])

  function syncUser(next: UserProfile) {
    setUser(next)
    setNickname(next.nickname || '')
    setUiLanguage(next.ui_language || 'zh-CN')
    setTheme(next.theme || 'system')
    saveStoredUser(next)
  }

  async function handleSave() {
    setSaveState('saving')
    setMessage('')
    try {
      const updated = await updateMe({ nickname, uiLanguage, theme })
      syncUser(updated)
      applyThemePreference(updated.theme as 'light' | 'dark' | 'system')
      setSaveState('success')
      setMessage(t('saved'))
    } catch (error) {
      setSaveState('error')
      setMessage(error instanceof Error ? error.message : t('profileSaveFailed'))
    }
  }

  async function handleAvatar(file?: File) {
    if (!file) return
    setSaveState('saving')
    setMessage('')
    try {
      const updated = await uploadAvatar(file)
      syncUser(updated)
      setSaveState('success')
      setMessage(t('avatarSaved'))
    } catch (error) {
      setSaveState('error')
      setMessage(error instanceof Error ? error.message : t('avatarUploadFailed'))
    }
  }

  return (
    <>
      <div className="page-head"><div><h1>{t('profileTitle')}</h1><p>{t('profileDesc')}</p></div></div>
      <section className="admin-card account-panel">
        <label className="avatar-uploader">
          {user?.avatar_url ? <img className="avatar avatar-large" src={user.avatar_url} alt={t('avatarAlt')} /> : <div className="avatar avatar-large">{user ? user.email_masked.slice(0, 1).toUpperCase() : 'A'}</div>}
          <span>{t('changeAvatar')}</span>
          <input type="file" accept="image/jpeg,image/png,image/webp" disabled={saveState === 'saving'} onChange={event => handleAvatar(event.target.files?.[0])} />
        </label>
        <div className="profile-grid">
          <div><span>{t('email')}</span><strong>{user?.email_masked || '-'}</strong></div>
          <div><span>{t('nickname')}</span><strong>{user?.nickname || '-'}</strong></div>
          <div><span>{t('role')}</span><strong>{user?.role || '-'}</strong></div>
          <div><span>{t('scope')}</span><strong>{t('scopeValue')}</strong></div>
          <div><span>{t('language')}</span><strong>{languageLabel(user?.ui_language, t)}</strong></div>
          <div><span>{t('theme')}</span><strong>{themeLabel(user?.theme, t)}</strong></div>
        </div>
      </section>

      <section className="admin-card">
        <div className="section-head"><div><h2>{t('editProfile')}</h2><p>{t('editProfileDesc')}</p></div></div>
        <div className="form-grid form-panel">
          <label className="field"><span>{t('nickname')}</span><input className="input" value={nickname} maxLength={40} onChange={event => setNickname(event.target.value)} placeholder={t('nickname')} /></label>
          <label className="field"><span>{t('language')}</span><select className="select" value={uiLanguage} onChange={event => setUiLanguage(event.target.value)}><option value="zh-CN">{t('languageChinese')}</option><option value="en-US">{t('languageEnglish')}</option><option value="ja-JP">{t('languageJapanese')}</option></select></label>
          <label className="field"><span>{t('themePreference')}</span><select className="select" value={theme} onChange={event => setTheme(event.target.value)}><option value="system">{t('system')}</option><option value="light">{t('light')}</option><option value="dark">{t('dark')}</option></select></label>
          {message ? <p className={saveState === 'error' ? 'status-bad' : 'status-good'}>{message}</p> : null}
          <div><button className="btn btn-primary" type="button" onClick={handleSave} disabled={saveState === 'saving'}>{saveState === 'saving' ? t('saving') : t('save')}</button></div>
        </div>
      </section>
      <PasswordChangeForm />
    </>
  )
}
