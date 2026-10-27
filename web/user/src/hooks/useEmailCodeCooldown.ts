'use client'

import { useEffect, useMemo, useState } from 'react'

interface CooldownState {
  email: string
  expiresAt: number
}

function normalizeEmail(email: string) {
  return email.trim().toLowerCase()
}

export function useEmailCodeCooldown(email: string) {
  const [cooldown, setCooldown] = useState<CooldownState | null>(null)
  const [now, setNow] = useState(() => Date.now())
  const normalizedEmail = normalizeEmail(email)
  const remainingSeconds = useMemo(() => {
    if (!cooldown || cooldown.email !== normalizedEmail) return 0
    return Math.max(0, Math.ceil((cooldown.expiresAt - now) / 1000))
  }, [cooldown, normalizedEmail, now])

  useEffect(() => {
    if (!cooldown) return
    const expiresAt = cooldown.expiresAt
    function tick() {
      const currentTime = Date.now()
      setNow(currentTime)
      if (currentTime >= expiresAt) setCooldown(null)
    }
    tick()
    const timer = window.setInterval(tick, 250)
    return () => window.clearInterval(timer)
  }, [cooldown])

  function startCooldown(seconds: number) {
    const safeSeconds = Number.isFinite(seconds) && seconds > 0 ? Math.ceil(seconds) : 60
    setNow(Date.now())
    setCooldown({
      email: normalizedEmail,
      expiresAt: Date.now() + safeSeconds * 1000
    })
  }

  return {
    cooldownActive: remainingSeconds > 0,
    remainingSeconds,
    startCooldown
  }
}
