'use client';

import { useEffect, useState } from 'react';
import { createPortal } from 'react-dom';
import { Button } from '@/components/ui/button';

export function ConfirmDialog({
  title,
  message,
  confirmText = '确认',
  cancelText = '取消',
  onConfirm,
  onCancel,
  variant = 'destructive',
  hideCancel = false,
}: {
  title: string;
  message: React.ReactNode;
  confirmText?: string;
  cancelText?: string;
  onConfirm: () => void;
  onCancel: () => void;
  variant?: 'destructive' | 'default';
  hideCancel?: boolean;
}) {
  const [isClosing, setIsClosing] = useState(false);
  const [isMounted, setIsMounted] = useState(false);

  useEffect(() => {
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';

    requestAnimationFrame(() => {
      setIsMounted(true);
    });

    return () => {
      document.body.style.overflow = previousOverflow;
    };
  }, []);

  const handleClose = () => {
    setIsClosing(true);
    setTimeout(onCancel, 200);
  };

  const handleConfirm = () => {
    setIsClosing(true);
    setTimeout(onConfirm, 200);
  };

  if (typeof document === 'undefined') return null;

  return createPortal(
    <div
      className={`fixed z-50 flex items-center justify-center overflow-hidden bg-black/50 p-3 transition-opacity duration-200 sm:p-4 ${
        isMounted && !isClosing ? 'opacity-100' : 'opacity-0'
      }`}
      style={{
        top: 'var(--picpilot-visual-viewport-top)',
        left: 'var(--picpilot-visual-viewport-left)',
        width: 'var(--picpilot-visual-viewport-width)',
        height: 'var(--picpilot-visual-viewport-height)',
      }}
      onClick={handleClose}
      onWheel={event => event.stopPropagation()}
      onTouchMove={event => event.stopPropagation()}
    >
      <div
        className={`flex max-h-[calc(var(--picpilot-visual-viewport-height)-1.5rem)] w-full max-w-md flex-col overflow-y-auto overscroll-contain rounded-xl border border-border bg-card p-5 pb-[max(1.25rem,env(safe-area-inset-bottom))] shadow-lg transition-all duration-200 sm:max-h-[calc(var(--picpilot-visual-viewport-height)-2rem)] sm:p-6 ${
          isMounted && !isClosing ? 'scale-100 opacity-100' : 'scale-95 opacity-0'
        }`}
        onClick={event => event.stopPropagation()}
      >
        <h3 className="mb-2 text-lg font-semibold">{title}</h3>
        <div className="mb-4 text-sm text-muted-foreground">{message}</div>
        <div className="sticky bottom-0 mt-auto flex justify-end gap-2 border-t border-border bg-card/95 pt-3 backdrop-blur">
          {!hideCancel && (
            <Button variant="outline" size="sm" onClick={handleClose}>
              {cancelText}
            </Button>
          )}
          <Button variant={variant} size="sm" onClick={handleConfirm}>
            {confirmText}
          </Button>
        </div>
      </div>
    </div>,
    document.body,
  );
}
