'use client'

import { type MutableRefObject, useEffect, useRef, useState } from 'react'
import { LoaderCircle } from 'lucide-react'

const SHOW_DELAY_MS = 140
const MIN_VISIBLE_MS = 260

export function AdminRequestProgress() {
  const [visible, setVisible] = useState(false)
  const activeRequests = useRef(new Set<string>())
  const visibleRef = useRef(false)
  const shownAt = useRef(0)
  const showTimer = useRef<number | null>(null)
  const hideTimer = useRef<number | null>(null)

  useEffect(() => {
    function clearTimer(ref: MutableRefObject<number | null>) {
      if (ref.current !== null) window.clearTimeout(ref.current)
      ref.current = null
    }

    function show() {
      if (!activeRequests.current.size || visibleRef.current) return
      visibleRef.current = true
      shownAt.current = Date.now()
      setVisible(true)
    }

    function hide() {
      if (activeRequests.current.size || !visibleRef.current) return
      visibleRef.current = false
      setVisible(false)
    }

    function onStart(event: Event) {
      const id = (event as CustomEvent<{ id?: string }>).detail?.id
      if (!id) return
      activeRequests.current.add(id)
      clearTimer(hideTimer)
      if (!visibleRef.current && showTimer.current === null) {
        showTimer.current = window.setTimeout(() => {
          showTimer.current = null
          show()
        }, SHOW_DELAY_MS)
      }
    }

    function onEnd(event: Event) {
      const id = (event as CustomEvent<{ id?: string }>).detail?.id
      if (id) activeRequests.current.delete(id)
      if (activeRequests.current.size) return
      clearTimer(showTimer)
      if (!visibleRef.current) return
      const remaining = Math.max(0, MIN_VISIBLE_MS - (Date.now() - shownAt.current))
      clearTimer(hideTimer)
      hideTimer.current = window.setTimeout(() => {
        hideTimer.current = null
        hide()
      }, remaining)
    }

    window.addEventListener('picpilot:admin-request-start', onStart)
    window.addEventListener('picpilot:admin-request-end', onEnd)
    return () => {
      clearTimer(showTimer)
      clearTimer(hideTimer)
      window.removeEventListener('picpilot:admin-request-start', onStart)
      window.removeEventListener('picpilot:admin-request-end', onEnd)
    }
  }, [])

  return (
    <div className={visible ? 'admin-request-progress admin-request-progress-visible' : 'admin-request-progress'} aria-hidden={!visible} aria-live="polite">
      <span className="admin-request-progress-bar" />
      <span className="admin-request-progress-label"><span className="admin-request-progress-spinner"><LoaderCircle size={14} /></span>正在同步</span>
    </div>
  )
}
