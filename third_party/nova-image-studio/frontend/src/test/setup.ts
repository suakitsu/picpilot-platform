import '@testing-library/jest-dom/vitest'
import { vi } from 'vitest'
import { setStorageScope } from '../../../../../canvas-lab/bridge/storage'
setStorageScope('test-user', 'nova')
globalThis.fetch = vi.fn(async () => { throw new Error('Unexpected network request: tests must mock fetch') })
