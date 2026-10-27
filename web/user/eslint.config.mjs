import nextVitals from 'eslint-config-next/core-web-vitals'

export default [
  ...nextVitals,
  {
    // Compiler adoption is separate from this runtime upgrade; keep its legacy diagnostics visible.
    rules: {
      'react-hooks/set-state-in-effect': 'warn',
      'react-hooks/purity': 'warn',
      'react-hooks/immutability': 'warn'
    }
  },
  { ignores: ['.next/**', '.cache/**', '.tmp/**', 'output/**', 'node_modules/**'] }
]
