'use client'

import { type ClipboardEvent, useRef, useState } from 'react'
import { Image as ImageIcon, X, Plus } from 'lucide-react'
import { IMAGE_FILE_ACCEPT, imageUploadErrorMessage, imageUploadMessage, validateImageFiles } from '@/lib/imageUpload'

interface ImageReferenceInputProps {
  label: string
  hint: string
  disabled?: boolean
  file?: File | null
  files?: File[]
  previewUrls?: string[]
  url?: string
  previewUrl?: string
  previewProcessing?: boolean
  onFile: (file: File) => void | Promise<void>
  onFiles?: (files: File[]) => void | Promise<void>
  onUrl: (url: string) => void
  onClearFile?: () => void
  onRemoveFile?: (index: number) => void
  onUpload?: (file: File) => void | Promise<void>
  multiple?: boolean
  uploadLabel?: string
  removeLabel?: string
  linkLabel?: string
  placeholder?: string
}

export function ImageReferenceInput({
  label,
  hint,
  disabled,
  file,
  files = [],
  previewUrls = [],
  url = '',
  previewUrl,
  previewProcessing = false,
  onFile,
  onFiles,
  onUrl,
  onClearFile,
  onRemoveFile,
  onUpload,
  multiple = false,
  uploadLabel = '选择图片',
  removeLabel = '移除',
  linkLabel = '使用链接',
  placeholder = 'https://example.com/product.jpg'
}: ImageReferenceInputProps) {
  const inputRef = useRef<HTMLInputElement | null>(null)
  const [dragging, setDragging] = useState(false)
  const [processing, setProcessing] = useState(false)
  const [uploadError, setUploadError] = useState('')

  async function pickFiles(nextFiles?: File[] | null) {
    const candidates = (nextFiles || []).filter(Boolean)
    if (!candidates.length || disabled || processing) return
    setProcessing(true)
    setUploadError('')
    try {
      const detected = await validateImageFiles(multiple ? candidates : candidates.slice(0, 1))
      const images = detected.map(item => item.file)
      if (multiple && onFiles) {
        await onFiles(images)
        for (const image of images) await onUpload?.(image)
      } else {
        const nextFile = images[0]
        await onFile(nextFile)
        await onUpload?.(nextFile)
      }
    } catch (error) {
      setUploadError(imageUploadErrorMessage(error))
    } finally {
      setProcessing(false)
      if (inputRef.current) inputRef.current.value = ''
    }
  }

  function pasteFile(event: ClipboardEvent<HTMLDivElement>) {
    if (disabled) return
    const files = Array.from(event.clipboardData.files || [])
    if (!files.length) return
    event.preventDefault()
    void pickFiles(multiple ? files : files.slice(0, 1))
  }

  const busy = processing || previewProcessing
  const unavailable = Boolean(disabled || busy)
  const previewItems = multiple
    ? Array.from({ length: Math.max(files.length, previewUrls.length) }, (_, index) => ({
        file: files[index],
        url: previewUrls[index] || ''
      }))
    : []
  const stackedPreviews = previewUrls.filter(Boolean).slice(0, 3)

  return (
    <div
      className={dragging ? 'reference-dropzone reference-dropzone-active' : 'reference-dropzone'}
      tabIndex={unavailable ? -1 : 0}
      onPaste={pasteFile}
      onDragEnter={event => {
        event.preventDefault()
        if (!unavailable) setDragging(true)
      }}
      onDragOver={event => event.preventDefault()}
      onDragLeave={() => setDragging(false)}
      onDrop={event => {
        event.preventDefault()
        setDragging(false)
        const dropped = Array.from(event.dataTransfer.files || [])
        pickFiles(multiple ? dropped : dropped.slice(0, 1))
      }}
      aria-disabled={unavailable}
    >
      <input ref={inputRef} type="file" accept={IMAGE_FILE_ACCEPT} multiple={multiple} hidden disabled={unavailable} onChange={event => void pickFiles(Array.from(event.target.files || []))} />
      <div className="reference-dropzone-copy">
        <strong>{label}</strong>
        <span>{busy ? imageUploadMessage('PROCESSING') : ((multiple ? (files.length || previewUrls.length) : 0) ? `${files.length || previewUrls.length} 张参考图` : (file ? file.name : hint))}</span>
      </div>
      <div className="reference-preview-stack">
        {stackedPreviews.length ? (
          <>
            {stackedPreviews.map((item, index) => <img className="reference-preview reference-preview-layer" src={item} alt={`${label} ${index + 1}`} decoding="async" key={`${item}-${index}`} />)}
          </>
        ) : previewUrl ? <img className="reference-preview" src={previewUrl} alt={label} decoding="async" /> : files.length ? (
          <span className="reference-preview reference-preview-placeholder"><ImageIcon size={24} /><small>{busy ? '处理中' : '待上传'}</small></span>
        ) : null}
        {multiple && (files.length || previewUrls.length) > 1 ? <div className="reference-preview-count">{files.length || previewUrls.length}</div> : null}
      </div>
      <div className="reference-actions">
        <button className="btn" type="button" disabled={unavailable} onClick={() => inputRef.current?.click()}><Plus size={16} />{busy ? imageUploadMessage('PROCESSING') : uploadLabel}</button>
        {file && onClearFile ? <button className="btn" type="button" disabled={unavailable} onClick={onClearFile}><X size={16} />{removeLabel}</button> : null}
      </div>
      <div className="reference-link-row">
        <input className="input" disabled={unavailable} value={url} onChange={event => onUrl(event.target.value)} placeholder={placeholder} />
        <span className="reference-link-label">{linkLabel}</span>
      </div>
      {uploadError ? <p className="reference-upload-error" role="alert">{uploadError}</p> : null}
      {multiple && previewItems.length > 0 ? (
        <div className="reference-thumb-grid">
          {previewItems.map((item, index) => (
            <div className="reference-thumb-item" key={`${item.url || item.file?.name || 'image'}-${index}`}>
              {item.url ? <img src={item.url} alt={`${label} ${index + 1}`} loading="lazy" decoding="async" /> : (
                <span className="reference-thumb-placeholder"><ImageIcon size={20} /><small>{item.file?.name || `图片 ${index + 1}`}</small></span>
              )}
              <button type="button" className="reference-thumb-remove" disabled={unavailable} onClick={() => onRemoveFile?.(index)}>
                <X size={14} />
              </button>
            </div>
          ))}
        </div>
      ) : null}
    </div>
  )
}
