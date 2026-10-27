'use client';

import { useEffect, useState, type ReactNode } from 'react';
import { RefreshCw } from 'lucide-react';
import { initializePicPilot } from '@/lib/picpilot-runtime';
import { syncDynamicModelExports } from '@/lib/gemini-config';

export function PicPilotEmbed({ children }: { children: ReactNode }) {
  const [ready, setReady] = useState(false);
  const [error, setError] = useState('');
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let active = true;
    initializePicPilot().then(() => {
      syncDynamicModelExports();
      if (active) setReady(true);
    }).catch(reason => { if (active) setError(reason instanceof Error ? reason.message : 'Connection failed'); });
    return () => { active = false; };
  }, [attempt]);
  if (ready) return children;
  return <main className="grid min-h-screen place-items-center bg-background text-foreground">
    <div className="max-w-md p-6 text-center" role="status">
      <p>{error || 'PicPilot'}</p>
      {error && <button className="mt-4 p-2" aria-label="Retry" title="Retry" onClick={() => { setError(''); setAttempt(value => value + 1); }}><RefreshCw size={20} /></button>}
    </div>
  </main>;
}
