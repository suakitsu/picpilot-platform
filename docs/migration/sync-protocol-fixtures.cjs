// Mechanical copies only: preserve the authority document and its normative schema.
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const root = path.resolve(__dirname, '../..');
const dest = path.resolve(__dirname, '../../core/workerprotocol/testdata');
const source = fs.readFileSync(path.join(root, 'docs/local-worker-protocol-v1.md'));
const document = source.toString('utf8');
const block = [...document.matchAll(/```json\r?\n([\s\S]*?)\r?\n```/g)].map(m => m[1]).find(s => JSON.parse(s).$schema);
if (!block) throw new Error('Normative JSON schema missing');
fs.mkdirSync(dest, {recursive:true});
fs.writeFileSync(path.join(dest,'schema.json'),JSON.stringify(JSON.parse(block),null,2)+'\n');
console.log('Authority SHA-256: '+crypto.createHash('sha256').update(source).digest('hex'));
