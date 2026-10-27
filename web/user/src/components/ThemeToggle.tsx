'use client'

import { useEffect, useRef, useState } from 'react'
import { Check, Laptop, Moon, Palette, Sparkles, Sun, Waves } from 'lucide-react'
import { useI18n } from '@/components/i18n'

type ThemeMode = 'light' | 'dark' | 'system'
type GlassMode = 'soft' | 'standard' | 'clear'
type MotionMode = 'on' | 'off'

const STORAGE_KEY = 'openimage-theme'
const ACCENT_KEY = 'openimage-accent'
const GLASS_KEY = 'openimage-glass'
const MOTION_KEY = 'openimage-motion'

const modes: Array<{ value: ThemeMode; labelKey: string; Icon: typeof Sun }> = [
  { value: 'light', labelKey: 'themeLight', Icon: Sun },
  { value: 'dark', labelKey: 'themeDark', Icon: Moon },
  { value: 'system', labelKey: 'themeSystem', Icon: Laptop }
]

const glassModes: Array<{ value: GlassMode; labelKey: string }> = [
  { value: 'soft', labelKey: 'glassSoft' },
  { value: 'standard', labelKey: 'glassStandard' },
  { value: 'clear', labelKey: 'glassClear' }
]

const accentPresets = [
  { nameKey: 'colorBlue', value: '#2563eb' },
  { nameKey: 'colorTeal', value: '#14b8a6' },
  { nameKey: 'colorPurple', value: '#8b5cf6' },
  { nameKey: 'colorOrange', value: '#f97316' },
  { nameKey: 'colorRose', value: '#ef4444' }
]

function resolveTheme(mode: ThemeMode) {
  if (mode === 'system') return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  return mode
}

function hexToRgb(hex: string) {
  const value = hex.replace('#', '')
  if (!/^[0-9a-fA-F]{6}$/.test(value)) return null
  return { r: parseInt(value.slice(0, 2), 16), g: parseInt(value.slice(2, 4), 16), b: parseInt(value.slice(4, 6), 16) }
}

function mixWithWhite(rgb: { r: number; g: number; b: number }, amount: number) {
  return {
    r: Math.round(rgb.r + (255 - rgb.r) * amount),
    g: Math.round(rgb.g + (255 - rgb.g) * amount),
    b: Math.round(rgb.b + (255 - rgb.b) * amount)
  }
}

function darken(rgb: { r: number; g: number; b: number }, amount: number) {
  return {
    r: Math.max(0, Math.round(rgb.r * (1 - amount))),
    g: Math.max(0, Math.round(rgb.g * (1 - amount))),
    b: Math.max(0, Math.round(rgb.b * (1 - amount)))
  }
}

function luminance(rgb: { r: number; g: number; b: number }) {
  const channel = (value: number) => {
    const normalized = value / 255
    return normalized <= 0.03928 ? normalized / 12.92 : Math.pow((normalized + 0.055) / 1.055, 2.4)
  }
  return 0.2126 * channel(rgb.r) + 0.7152 * channel(rgb.g) + 0.0722 * channel(rgb.b)
}

function readableActionColor(rgb: { r: number; g: number; b: number }) {
  const value = luminance(rgb)
  if (value > 0.62) return darken(rgb, .46)
  if (value < 0.08) return mixWithWhite(rgb, .18)
  return rgb
}

export function applyAccentColor(hex: string) {
  const rgb = hexToRgb(hex)
  if (!rgb) return
  const action = readableActionColor(rgb)
  const hover = darken(action, .14)
  const tint = mixWithWhite(rgb, .92)
  const contrast = luminance(action) > 0.5 ? '#111827' : '#ffffff'
  const root = document.documentElement
  root.style.setProperty('--accent-rgb', `${rgb.r},${rgb.g},${rgb.b}`)
  root.style.setProperty('--primary', `rgb(${action.r},${action.g},${action.b})`)
  root.style.setProperty('--primary-hover', `rgb(${hover.r},${hover.g},${hover.b})`)
  root.style.setProperty('--primary-contrast', contrast)
  root.style.setProperty('--primary-soft', `rgba(${rgb.r},${rgb.g},${rgb.b},.10)`)
  root.style.setProperty('--accent-tint', `rgb(${tint.r},${tint.g},${tint.b})`)
  localStorage.setItem(ACCENT_KEY, hex)
}

export function applyGlassPreference(mode: GlassMode) {
  document.documentElement.dataset.glass = mode
  localStorage.setItem(GLASS_KEY, mode)
}

export function applyMotionPreference(mode: MotionMode) {
  document.documentElement.dataset.motion = mode
  localStorage.setItem(MOTION_KEY, mode)
}

export function applyThemePreference(mode: ThemeMode) {
  const resolved = resolveTheme(mode)
  document.documentElement.dataset.themeMode = mode
  document.documentElement.dataset.theme = resolved
  document.documentElement.style.colorScheme = resolved
  localStorage.setItem(STORAGE_KEY, mode)
}

export function ThemeToggle() {
  const [mode, setMode] = useState<ThemeMode>('system')
  const [glass, setGlass] = useState<GlassMode>('soft')
  const [motion, setMotion] = useState<MotionMode>('on')
  const [open, setOpen] = useState(false)
  const [accent, setAccent] = useState('#2563eb')
  const menuRef = useRef<HTMLDivElement | null>(null)
  const t = useI18n()

  useEffect(() => {
    const saved = localStorage.getItem(STORAGE_KEY) as ThemeMode | null
    const initial = saved && ['light', 'dark', 'system'].includes(saved) ? saved : 'system'
    const savedAccent = localStorage.getItem(ACCENT_KEY) || '#2563eb'
    const savedGlass = localStorage.getItem(GLASS_KEY) as GlassMode | null
    const initialGlass = savedGlass && ['soft', 'standard', 'clear'].includes(savedGlass) ? savedGlass : 'soft'
    const savedMotion = localStorage.getItem(MOTION_KEY) as MotionMode | null
    const initialMotion = savedMotion && ['on', 'off'].includes(savedMotion) ? savedMotion : 'on'
    setMode(initial)
    setAccent(savedAccent)
    setGlass(initialGlass)
    setMotion(initialMotion)
    applyThemePreference(initial)
    applyAccentColor(savedAccent)
    applyGlassPreference(initialGlass)
    applyMotionPreference(initialMotion)

    const media = window.matchMedia('(prefers-color-scheme: dark)')
    const syncSystemTheme = () => {
      const current = (localStorage.getItem(STORAGE_KEY) as ThemeMode | null) || 'system'
      if (current === 'system') applyThemePreference('system')
    }
    media.addEventListener('change', syncSystemTheme)
    return () => media.removeEventListener('change', syncSystemTheme)
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

  const selectMode = (next: ThemeMode) => {
    setMode(next)
    applyThemePreference(next)
  }

  const selectAccent = (next: string) => {
    setAccent(next)
    applyAccentColor(next)
  }

  const selectGlass = (next: GlassMode) => {
    setGlass(next)
    applyGlassPreference(next)
  }

  const toggleMotion = () => {
    const next = motion === 'on' ? 'off' : 'on'
    setMotion(next)
    applyMotionPreference(next)
  }

  const current = modes.find(item => item.value === mode) || modes[2]
  const Icon = current.Icon

  return (
    <div className="quick-menu" ref={menuRef}>
      <button className="icon-button labeled-icon-button theme-icon-button" type="button" onClick={() => setOpen(!open)} aria-label={t('appearanceSettings')} title={t('appearanceSettings')}>
        <Icon size={16} /><span className="tool-label">{t('appearance')}</span>
      </button>
      {open ? (
        <div className="quick-menu-popover theme-popover appearance-popover">
          <div className="appearance-section">
            <div className="appearance-title"><Sparkles size={14} />{t('appearance')}</div>
            {modes.map(item => (
              <button key={item.value} className="quick-menu-item" type="button" onClick={() => selectMode(item.value)}>
                <item.Icon size={15} /><strong>{t(`theme${item.value[0].toUpperCase()}${item.value.slice(1)}`)}</strong>{mode === item.value ? <Check size={14} /> : null}
              </button>
            ))}
          </div>
          <div className="appearance-section">
            <div className="appearance-title"><Palette size={14} />{t('colorTheme')}</div>
            <label className="color-row compact-color-row">
              <span>{t('custom')}</span>
              <input type="color" value={accent} onChange={event => selectAccent(event.target.value)} />
            </label>
            <div className="color-presets named-color-presets">
              {accentPresets.map(color => <button key={color.value} type="button" style={{ background: color.value }} onClick={() => selectAccent(color.value)} aria-label={t(color.nameKey)} title={t(color.nameKey)} className={accent.toLowerCase() === color.value ? 'color-active' : ''} />)}
            </div>
          </div>
          <div className="appearance-section">
            <div className="appearance-title"><Waves size={14} />{t('glassStrength')}</div>
            <div className="segmented-row">
              {glassModes.map(item => <button key={item.value} type="button" className={glass === item.value ? 'segment-active' : ''} onClick={() => selectGlass(item.value)}>{t(`glass${item.value[0].toUpperCase()}${item.value.slice(1)}`)}</button>)}
            </div>
          </div>
          <button className="motion-row" type="button" onClick={toggleMotion}>
            <span>{t('motionBackground')}</span>
            <strong>{motion === 'on' ? t('on') : t('off')}</strong>
          </button>
        </div>
      ) : null}
    </div>
  )
}
