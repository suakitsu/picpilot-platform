'use client';

import { lazy, Suspense } from 'react';
import { PicPilotEmbed } from '@/components/PicPilotEmbed';
const WorkspaceShell = lazy(() => import('@/components/workspace/WorkspaceShell').then(module => ({ default: module.WorkspaceShell })));

export default function Home() {
  return (
    <PicPilotEmbed>
      <Suspense fallback={null}><WorkspaceShell /></Suspense>
    </PicPilotEmbed>
  );
}
