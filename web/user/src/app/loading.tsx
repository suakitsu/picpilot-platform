export default function Loading() {
  return (
    <div className="user-route-loading" role="status" aria-label="正在加载页面">
      <span className="route-loading-sr-only">正在加载页面…</span>
      <div className="user-route-loading-head">
        <span className="user-route-loading-kicker" />
        <span className="user-route-loading-title" />
        <span className="user-route-loading-copy" />
      </div>
      <div className="user-route-loading-grid" aria-hidden="true">
        <span />
        <span />
        <span />
      </div>
      <div className="user-route-loading-panel" aria-hidden="true">
        <span className="user-route-loading-line user-route-loading-line-long" />
        <span className="user-route-loading-line" />
        <span className="user-route-loading-line user-route-loading-line-short" />
        <span className="user-route-loading-line" />
        <span className="user-route-loading-line user-route-loading-line-medium" />
      </div>
    </div>
  )
}
