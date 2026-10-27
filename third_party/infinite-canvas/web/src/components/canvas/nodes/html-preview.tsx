import { useMemo } from 'react';
import DOMPurify from 'dompurify';
import { CodeXml, Eye } from 'lucide-react';
import type { CanvasNodeContext, CanvasNodeDefinition } from '@/types/canvas-plugin';

export function htmlPreviewDocument(source: string) {
  if (source.length > 1024 * 1024) throw new Error('HTML preview exceeds 1 MiB');
  const html = DOMPurify.sanitize(source, {
    USE_PROFILES: { html: true },
    FORBID_TAGS: ['script', 'iframe', 'object', 'embed', 'form', 'meta', 'base', 'link'],
    FORBID_ATTR: ['srcdoc', 'action', 'formaction'],
  });
  return `<!doctype html><html><head><meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'none'; connect-src 'none'; img-src data: blob:; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'"></head><body>${html}</body></html>`;
}
function HtmlContent({ ctx }: { ctx: CanvasNodeContext }) {
  const content = ctx.node.metadata?.content || '';
  const upstream = ctx.getUpstream().filter(node => node.type === 'text').map(node => node.metadata?.content || '').join('\n');
  const document = useMemo(() => htmlPreviewDocument(content.replace(/\{\{\s*input\s*\}\}/g, upstream)), [content, upstream]);
  if (ctx.node.metadata?.editing) return <textarea className="h-full w-full resize-none bg-transparent p-3 font-mono text-xs" aria-label="HTML" value={content} maxLength={1024 * 1024} onChange={event => ctx.updateMetadata({ content: event.target.value })} onPointerDown={event => event.stopPropagation()} />;
  return <iframe title="HTML preview" sandbox="" referrerPolicy="no-referrer" srcDoc={document} className="block h-full w-full border-0 bg-white" />;
}
export const htmlNode: CanvasNodeDefinition = {
  type: 'html:render', title: 'HTML', icon: <CodeXml size={20} />, defaultSize: { width: 420, height: 320 },
  defaultMetadata: { content: '', editing: true }, Content: HtmlContent, hidePanel: true, interactionToggle: true,
  resource: node => ({ kind: 'text', text: node.metadata?.content || '' }),
  toolbar: ctx => [{ id: 'html-edit', title: 'HTML', label: 'HTML', icon: ctx.node.metadata?.editing ? <Eye size={16} /> : <CodeXml size={16} />, onClick: () => ctx.updateMetadata({ editing: !ctx.node.metadata?.editing }) }],
};
