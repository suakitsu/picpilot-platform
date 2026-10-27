'use client'

import Link from 'next/link'
import { useEffect, useState } from 'react'
import { assetUrl, getMe, getStoredUser, requireLogin, saveStoredUser, updateMe, uploadAvatar, type UserProfile } from '@/lib/api'
import { Shell } from '@/components/Shell'
import { UserAuthGate } from '@/components/UserAuthGate'
import { BillingOnly, useRuntime } from '@/components/AppRuntime'
import { PasswordChangeForm } from '@/components/PasswordChangeForm'
import { Settings2 } from 'lucide-react'
import { applyThemePreference } from '@/components/ThemeToggle'
import { useI18n } from '@/components/i18n'
import { IMAGE_FILE_ACCEPT, imageUploadMessage, validateImageFile } from '@/lib/imageUpload'

type SaveState = 'idle' | 'saving' | 'success' | 'error'
const languageLabels: Record<string, string> = { 'zh-CN': '中文', 'en-US': 'English', 'ja-JP': '日本語' }

export default function ProfilePage() {
  const t = useI18n()
  const { config } = useRuntime()
  const [user, setUser] = useState<UserProfile | null>(null)
  const [nickname, setNickname] = useState('')
  const [uiLanguage, setUiLanguage] = useState('zh-CN')
  const [theme, setTheme] = useState('system')
  const [saveState, setSaveState] = useState<SaveState>('idle')
  const [message, setMessage] = useState('')

  useEffect(() => {
    if (!requireLogin()) return
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
      setMessage(t('profileSaved'))
    } catch (error) {
      setSaveState('error')
      setMessage(error instanceof Error ? error.message : t('saveFailed'))
    }
  }

  async function handleAvatar(file?: File) {
    if (!file) return
    setSaveState('saving')
    setMessage(imageUploadMessage('PROCESSING'))
    try {
      await validateImageFile(file, 2 * 1024 * 1024)
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
    <Shell>
      <UserAuthGate title={t('loginProfileTitle')} description={t('loginProfileDesc')}>
        <div className="page-head"><div><h1>{t('profileTitle')}</h1><p>{t('profileDesc')}</p></div>{config.edition === 'personal' ? <Link className="btn" href="/providers"><Settings2 size={16} />{t('providersTitle')}</Link> : null}<BillingOnly><Link className="btn" href="/credits">{t('topUpCredits')}</Link></BillingOnly></div>
        <section className="panel account-panel">
          <label className="avatar-uploader">
            {user?.avatar_url ? <img className="avatar avatar-large" src={assetUrl(user.avatar_url)} alt={t('changeAvatar')} /> : <span className="avatar avatar-large">{user ? (user.email_display || user.email_masked).slice(0, 1).toUpperCase() : 'U'}</span>}
            <input type="file" accept={IMAGE_FILE_ACCEPT} disabled={saveState === 'saving'} onChange={event => void handleAvatar(event.target.files?.[0])} />
            <small>{t('changeAvatar')}</small>
          </label>
          <div className="profile-grid">
            <div><span>{t('accountLabel')}</span><strong>{user?.email_display || user?.email_masked || t('notLoggedIn')}</strong></div>
            <div><span>{t('nickname')}</span><strong>{user?.nickname || t('notSet')}</strong></div>
            <div><span>{t('role')}</span><strong>{user?.role || '-'}</strong></div>
            <BillingOnly><div><span>{t('currentCredits')}</span><strong>{user?.credits ?? '-'}</strong></div></BillingOnly>
            <div><span>{t('language')}</span><strong>{languageLabels[user?.ui_language || 'zh-CN'] || user?.ui_language || '中文'}</strong></div>
            <div><span>{t('themePreference')}</span><strong>{t(`theme${(user?.theme || 'system')[0].toUpperCase()}${(user?.theme || 'system').slice(1)}`)}</strong></div>
          </div>
        </section>

        <section className="panel mt-16">
          <div className="panel-head"><div><h2>{t('editProfile')}</h2><p>{t('editProfileDesc')}</p></div></div>
          <div className="panel-body form-grid">
            <label className="field"><span>{t('nickname')}</span><input className="input" value={nickname} maxLength={40} onChange={event => setNickname(event.target.value)} placeholder={t('nicknamePlaceholder')} /></label>
            <label className="field"><span>{t('language')}</span><select className="select" value={uiLanguage} onChange={event => setUiLanguage(event.target.value)}><option value="zh-CN">中文</option><option value="en-US">English</option><option value="ja-JP">日本語</option></select></label>
            <label className="field"><span>{t('themePreference')}</span><select className="select" value={theme} onChange={event => setTheme(event.target.value)}><option value="system">{t('themeSystem')}</option><option value="light">{t('themeLight')}</option><option value="dark">{t('themeDark')}</option></select></label>
            {message ? <p className={saveState === 'error' ? 'status status-bad' : 'status status-ok'}>{message}</p> : null}
            <div><button className="btn btn-primary" type="button" onClick={handleSave} disabled={saveState === 'saving'}>{saveState === 'saving' ? t('saving') : t('saveProfile')}</button></div>
          </div>
        </section>
        <PasswordChangeForm />
      </UserAuthGate>
    </Shell>
  )
}
