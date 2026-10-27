import type { NextConfig } from 'next';
import path from 'node:path';
import pkg from './package.json';

const config: NextConfig = {
  output: 'export',
  outputFileTracingRoot: path.join(__dirname),
  basePath: '/canvas/nova',
  trailingSlash: true,
  poweredByHeader: false,
  images: { unoptimized: true },
  experimental: { cpus: 2 },
  env: { NEXT_PUBLIC_APP_VERSION: pkg.version, NEXT_PUBLIC_PICPILOT_EMBEDDED: 'true' },
};

export default config;
