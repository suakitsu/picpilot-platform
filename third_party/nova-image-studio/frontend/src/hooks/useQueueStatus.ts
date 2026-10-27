import { useEffect, useState } from 'react';
import { novaTaskSocket } from '@/lib/ccode-task-socket';
import type { NovaQueueStatus } from '@/lib/ccode-task-client';

export function useQueueStatus() {
  const [queueStatus, setQueueStatus] = useState<NovaQueueStatus | null>(null);

  useEffect(() => {
    const unsubscribe = novaTaskSocket.subscribeQueue(stats => setQueueStatus(stats));
    return () => {
      unsubscribe();
    };
  }, []);

  return queueStatus;
}
