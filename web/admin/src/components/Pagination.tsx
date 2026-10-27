'use client'

import { FormEvent, useEffect, useMemo, useState } from 'react'
import { ChevronLeft, ChevronRight, ChevronsLeft, ChevronsRight } from 'lucide-react'

interface PaginationProps {
  page: number
  total: number
  pageSize: number
  totalPages: number
  onPageChange: (page: number) => void
  labels: {
    previous: string
    next: string
    page: string
    total: string
    jump?: string
  }
}

export function Pagination({ page, total, pageSize, totalPages, onPageChange, labels }: PaginationProps) {
  const normalizedTotalPages = Math.max(totalPages, 1)
  const normalizedPage = Math.min(Math.max(page, 1), normalizedTotalPages)
  const [jumpPage, setJumpPage] = useState(String(normalizedPage))
  const start = total ? (page - 1) * pageSize + 1 : 0
  const end = Math.min(total, page * pageSize)

  useEffect(() => setJumpPage(String(normalizedPage)), [normalizedPage])

  const pageItems = useMemo(() => {
    if (normalizedTotalPages <= 7) return Array.from({ length: normalizedTotalPages }, (_, index) => index + 1)
    const selected = new Set([1, normalizedTotalPages])
    for (let value = normalizedPage - 2; value <= normalizedPage + 2; value += 1) {
      if (value > 1 && value < normalizedTotalPages) selected.add(value)
    }
    const pages = Array.from(selected).sort((left, right) => left - right)
    const items: Array<number | string> = []
    pages.forEach((value, index) => {
      const previous = pages[index - 1]
      if (previous && value - previous > 1) items.push(`ellipsis-${previous}`)
      items.push(value)
    })
    return items
  }, [normalizedPage, normalizedTotalPages])

  function changePage(nextPage: number) {
    const target = Math.min(Math.max(nextPage, 1), normalizedTotalPages)
    if (target !== normalizedPage) onPageChange(target)
  }

  function submitJump(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const target = Number.parseInt(jumpPage, 10)
    if (!Number.isFinite(target)) {
      setJumpPage(String(normalizedPage))
      return
    }
    changePage(target)
  }

  return (
    <div className="pagination-bar">
      <span className="pagination-summary">{labels.total} {total} · {start}-{end}</span>
      <div className="pagination-controls">
        <button className="btn btn-compact pagination-icon-button" type="button" disabled={normalizedPage <= 1} onClick={() => changePage(1)} aria-label={`${labels.previous} 1`} title={`${labels.previous} 1`}><ChevronsLeft size={15} /></button>
        <button className="btn btn-compact pagination-icon-button" type="button" disabled={normalizedPage <= 1} onClick={() => changePage(normalizedPage - 1)} aria-label={labels.previous} title={labels.previous}><ChevronLeft size={15} /></button>
        <div className="pagination-pages" aria-label={`${labels.page} ${normalizedPage}/${normalizedTotalPages}`}>
          {pageItems.map(item => typeof item === 'number' ? (
            <button className={item === normalizedPage ? 'btn btn-compact pagination-page pagination-page-active' : 'btn btn-compact pagination-page'} type="button" key={item} aria-current={item === normalizedPage ? 'page' : undefined} onClick={() => changePage(item)}>{item}</button>
          ) : <span className="pagination-ellipsis" key={item}>...</span>)}
        </div>
        <button className="btn btn-compact pagination-icon-button" type="button" disabled={normalizedPage >= normalizedTotalPages} onClick={() => changePage(normalizedPage + 1)} aria-label={labels.next} title={labels.next}><ChevronRight size={15} /></button>
        <button className="btn btn-compact pagination-icon-button" type="button" disabled={normalizedPage >= normalizedTotalPages} onClick={() => changePage(normalizedTotalPages)} aria-label={`${labels.next} ${normalizedTotalPages}`} title={`${labels.next} ${normalizedTotalPages}`}><ChevronsRight size={15} /></button>
        <form className="pagination-jump" onSubmit={submitJump}>
          <span>{labels.jump || '跳至'}</span>
          <input className="input" type="number" min={1} max={normalizedTotalPages} value={jumpPage} onChange={event => setJumpPage(event.target.value)} aria-label={labels.jump || '跳转页码'} />
          <span>/ {normalizedTotalPages}</span>
          <button className="btn btn-compact" type="submit">GO</button>
        </form>
      </div>
    </div>
  )
}
