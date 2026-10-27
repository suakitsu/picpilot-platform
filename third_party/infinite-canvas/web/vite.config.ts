import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';
import { parseChangelog } from './src/lib/release';

const root = dirname(fileURLToPath(import.meta.url));
export default defineConfig({
  base: '/canvas/infinite/',
  plugins: [react()],
  resolve: { alias: { '@': resolve(root, 'src') } },
  define: {
    __APP_VERSION__: JSON.stringify(readFileSync(resolve(root, '../VERSION'), 'utf8').trim()),
    __APP_RELEASES__: JSON.stringify(parseChangelog(readFileSync(resolve(root, '../CHANGELOG.md'), 'utf8'))),
  },
});
