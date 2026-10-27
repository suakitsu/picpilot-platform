const HOP_HEADERS = ['connection', 'keep-alive', 'proxy-authenticate', 'proxy-authorization', 'te', 'trailer', 'transfer-encoding', 'upgrade', 'host', 'content-length']

export async function proxyApi(request: Request): Promise<Response> {
  const requestUrl = new URL(request.url)
  let origin: URL
  try {
    origin = new URL(process.env.GO_API_INTERNAL_URL || process.env.BACKEND_INTERNAL_BASE_URL || 'http://127.0.0.1:8080')
    if (!['http:', 'https:'].includes(origin.protocol) || origin.username || origin.password
      || origin.pathname !== '/' || origin.search || origin.hash) throw new Error()
  } catch {
    return Response.json({ success: false, code: 'PROXY_CONFIG_INVALID', message: 'Invalid Go API origin', data: null, request_id: '' }, { status: 503 })
  }
  const target = new URL(requestUrl.pathname + requestUrl.search, origin)
  const headers = new Headers(request.headers)
  for (const header of HOP_HEADERS) headers.delete(header)
  headers.delete('accept-encoding')
  headers.delete('x-forwarded-for')
  headers.delete('forwarded')
  headers.set('x-forwarded-host', requestUrl.host)
  headers.set('x-forwarded-proto', requestUrl.protocol.slice(0, -1))
  try {
    const init: RequestInit & { duplex?: 'half' } = {
      method: request.method, headers, redirect: 'manual', cache: 'no-store', signal: request.signal
    }
    if (request.method !== 'GET' && request.method !== 'HEAD') {
      init.body = request.body
      init.duplex = 'half'
    }
    const upstream = await fetch(target, init)
    const responseHeaders = new Headers(upstream.headers)
    for (const header of HOP_HEADERS) responseHeaders.delete(header)
    // Fetch decompresses bodies. Do not forward the stale encoding header.
    responseHeaders.delete('content-encoding')
    const location = responseHeaders.get('location')
    if (location) {
      const destination = new URL(location, origin)
      if (destination.origin === origin.origin) responseHeaders.set('location', destination.pathname + destination.search + destination.hash)
    }
    responseHeaders.set('cache-control', 'no-store')
    return new Response(upstream.body, { status: upstream.status, headers: responseHeaders })
  } catch {
    return Response.json({ success: false, code: 'GO_API_UNAVAILABLE', message: 'Go API is unavailable', data: null, request_id: '' }, { status: 502 })
  }
}

