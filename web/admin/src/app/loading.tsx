export default function Loading() {
  return (
    <div className="admin-route-loading" role="status" aria-label="正在加载页面">
      <span className="route-loading-sr-only">正在加载页面…</span>
      <div className="admin-route-loading-head">
        <span className="admin-route-loading-kicker" />
        <span className="admin-route-loading-title" />
        <span className="admin-route-loading-copy" />
      </div>
      <div className="admin-route-loading-grid" aria-hidden="true">
        <span />
        <span />
        <span />
      </div>
      <div className="admin-route-loading-panel" aria-hidden="true">
        <span className="admin-route-loading-line admin-route-loading-line-long" />
        <span className="admin-route-loading-line" />
        <span className="admin-route-loading-line admin-route-loading-line-short" />
        <span className="admin-route-loading-line" />
        <span className="admin-route-loading-line admin-route-loading-line-medium" />
      </div>
    </div>
  )
}
