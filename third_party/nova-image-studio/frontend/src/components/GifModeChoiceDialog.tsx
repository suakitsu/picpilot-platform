'use client';

import { useEffect, useState } from 'react';
import { createPortal } from 'react-dom';
import { Sparkles, SlidersHorizontal, X } from 'lucide-react';

interface GifModeChoiceDialogProps {
  onAuto: () => void;
  onTune: () => void;
  onCancel: () => void;
}

export function GifModeChoiceDialog({ onAuto, onTune, onCancel }: GifModeChoiceDialogProps) {
  const [isClosing, setIsClosing] = useState(false);
  const [isMounted, setIsMounted] = useState(false);

  useEffect(() => {
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';

    requestAnimationFrame(() => setIsMounted(true));

    return () => {
      document.body.style.overflow = previousOverflow;
    };
  }, []);

  const close = (action: () => void) => {
    setIsClosing(true);
    setTimeout(action, 200);
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
      onClick={() => close(onCancel)}
    >
      <div
        className={`flex max-h-[calc(var(--picpilot-visual-viewport-height)-1.5rem)] w-full max-w-md flex-col overflow-y-auto overscroll-contain rounded-xl border border-border bg-card p-5 pb-[max(1.25rem,env(safe-area-inset-bottom))] shadow-lg transition-all duration-200 sm:max-h-[calc(var(--picpilot-visual-viewport-height)-2rem)] sm:p-6 ${
          isMounted && !isClosing ? 'scale-100 opacity-100' : 'scale-95 opacity-0'
        }`}
        onClick={event => event.stopPropagation()}
      >
        <div className="mb-1 flex items-center justify-between">
          <h3 className="text-lg font-semibold">生成 GIF</h3>
          <button
            type="button"
            onClick={() => close(onCancel)}
            className="rounded-md p-1 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
            title="取消"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
        <p className="mb-4 text-sm text-muted-foreground">选择合成方式。</p>

        <div className="grid gap-3">
          <button
            type="button"
            onClick={() => close(onAuto)}
            className="group flex items-start gap-3 rounded-xl border border-border bg-muted/30 p-4 text-left transition-colors hover:border-primary/50 hover:bg-primary/5"
          >
            <span className="mt-0.5 flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
              <Sparkles className="h-5 w-5" />
            </span>
            <span className="min-w-0">
              <span className="block text-sm font-medium text-foreground">自动生成</span>
              <span className="mt-0.5 block text-xs text-muted-foreground">
                按固定网格直接切割 12 帧并立即合成下载。
              </span>
            </span>
          </button>

          <button
            type="button"
            onClick={() => close(onTune)}
            className="group flex items-start gap-3 rounded-xl border border-border bg-muted/30 p-4 text-left transition-colors hover:border-primary/50 hover:bg-primary/5"
          >
            <span className="mt-0.5 flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
              <SlidersHorizontal className="h-5 w-5" />
            </span>
            <span className="min-w-0">
              <span className="block text-sm font-medium text-foreground">微调生成</span>
              <span className="mt-0.5 block text-xs text-muted-foreground">
                打开全屏编辑器，逐帧拖动 / 缩放对齐后再合成。
              </span>
            </span>
          </button>
        </div>
      </div>
    </div>,
    document.body,
  );
}
