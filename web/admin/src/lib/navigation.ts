export function safeNextPath(value: string | null | undefined, fallback = '/workspace') {
  if (!value || !value.startsWith('/') || value.startsWith('//') || /[\\\u0000-\u0020\u007f]/.test(value)) return fallback
  try {
    const destination = new URL(value, 'https://picpilot.invalid')
    const decodedPath = decodeURIComponent(destination.pathname)
    if (destination.origin !== 'https://picpilot.invalid' || decodedPath.startsWith('//')
      || /[\\\u0000-\u0020\u007f]/.test(decodedPath) || /^\/api(?:\/|$)/.test(decodedPath)) return fallback
    return destination.pathname + destination.search + destination.hash
  } catch { return fallback }
}
