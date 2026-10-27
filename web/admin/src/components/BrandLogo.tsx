type BrandLogoProps = {
  className?: string
  subtitle?: string
}

export function BrandLogo({ className = '', subtitle }: BrandLogoProps) {
  return (
    <span className={['picpilot-brand', className].filter(Boolean).join(' ')}>
      <img className="picpilot-brand-mark" src="/brand-picpilot-mark.png" alt="" aria-hidden="true" />
      <span className="picpilot-brand-copy">
        <strong>PicPilot</strong>
        {subtitle ? <small>{subtitle}</small> : null}
      </span>
    </span>
  )
}
