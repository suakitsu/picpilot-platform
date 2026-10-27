// Read source only; mechanically regenerate the inventory within this directory.
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const root = path.resolve(process.argv[2] || 'E:/picpilot/workspace/openImage');
const out = __dirname;
const ts = require(path.join(root, 'openImage-ts/node_modules/typescript/lib/typescript.js'));
const rel = p => path.relative(root, p).replaceAll('\\', '/');
const read = p => fs.readFileSync(p, 'utf8');
const line = (s, p) => s.slice(0, p).split('\n').length;
const compact = s => s.replace(/\s+/g, ' ').trim();
const sha = s => crypto.createHash('sha256').update(s).digest('hex');
const uniq = xs => [...new Set(xs)];
function walk(dir, extensions) {
  if (!fs.existsSync(dir)) return [];
  return fs.readdirSync(dir, {withFileTypes: true}).sort((a,b)=>a.name.localeCompare(b.name)).flatMap(e => {
    if (/^(node_modules|target|dist|build|\.git|artifacts|output|uploads|data|secrets)$/i.test(e.name) || e.name.startsWith('.env') || e.isSymbolicLink()) return [];
    const p = path.join(dir, e.name);
    return e.isDirectory() ? walk(p, extensions) : extensions.includes(path.extname(p)) ? [p] : [];
  });
}
// Lexical masking preserves offsets and balanced delimiters in Java source.
function mask(s) {
  return s.replace(/\/\*[\s\S]*?\*\/|\/\/[^\r\n]*|"""[\s\S]*?"""|"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'/g, m => m.replace(/[^\r\n]/g, ' '));
}
function end(s, start, open='(', close=')') {
  let depth = 0;
  for (let i=start; i<s.length; i++) {
    if (s[i] === open) depth++;
    if (s[i] === close && --depth === 0) return i;
  }
  throw new Error('Unbalanced source at ' + start);
}
function split(s) {
  const m = mask(s); let start=0, depth=0, result=[];
  for (let i=0;i<m.length;i++) {
    if ('(<[{'.includes(m[i])) depth++;
    if (')>]}'.includes(m[i])) depth--;
    if (m[i]===',' && depth===0) {result.push(s.slice(start,i).trim()); start=i+1;}
  }
  result.push(s.slice(start).trim()); return result.filter(Boolean);
}
function methods(s) {
  const m=mask(s), result=[];
  const rx=/\b(public|private|protected)\s+(?:(?:static|final|synchronized)\s+)*([\w.$<>?,\s\[\]]+?)\s+(\w+)\s*\(/g;
  for (let x; (x=rx.exec(m));) {
    if (/^(class|record|interface|enum)\b/.test(x[2])) continue;
    const a=rx.lastIndex-1,b=end(m,a), c=m.indexOf('{',b);
    if(c<0 || /;/.test(m.slice(b,c))) continue;
    const d=end(m,c,'{','}');
    result.push({name:x[3],javaType:compact(s.slice(x.index+x[1].length,a).replace(/\b(static|final|synchronized)\s+/g,'').replace(/\s+\w+\s*$/,'')), start:x.index, bodyStart:c, stop:d, params:compact(s.slice(a+1,b)), body:s.slice(c+1,d)});
    rx.lastIndex=d+1;
  }
  return result;
}
const javaFiles=[...walk(path.join(root,'openImage/src/main/java'),['.java']),...walk(path.join(root,'src/main'),['.java'])];
const sources=javaFiles.map(file=>({file,s:read(file)}));
const shapes={};
for(const {file,s} of sources) {
  const m=mask(s), owner=path.basename(file,'.java');
  for(const x of m.matchAll(/\b(?:public\s+)?record\s+(\w+)(?:<[^>]+>)?\s*\(/g)) {
    const a=x.index+x[0].length-1,b=end(m,a);
    shapes[owner+'.'+x[1]]={source:rel(file),line:line(s,x.index),kind:'record',declaration:compact(s.slice(x.index,b+1)),fields:split(s.slice(a+1,b)).map(f=>{
      const q=f.match(/@JsonProperty\("([^"]+)"\)/),clean=f.replace(/@\w+(?:\([^)]*\))?\s*/g,'').trim(),p=clean.match(/^(.*)\s+(\w+)$/);
      return {name:q?q[1]:p?.[2]?.replace(/[A-Z]/g,c=>'_'+c.toLowerCase()),javaName:p?.[2],javaType:p?.[1]};
    })};
  }
  if(/Entity\.java$/.test(file)) {
    const fields=[...s.matchAll(/^\s*private\s+([\w<>?,. \[\]]+)\s+(\w+)\s*;/gm)].map(x=>({name:x[2].replace(/[A-Z]/g,c=>'_'+c.toLowerCase()),javaName:x[2],javaType:x[1]}));
    shapes[owner]={source:rel(file),line:1,kind:'entity',fields,notes:'Default application snake_case; annotations and runtime filters remain authoritative.'};
  }
}
const frontendFiles=[...walk(path.join(root,'openImage-ts/src'),['.ts','.tsx','.js']),...walk(path.join(root,'openImage-admin-ts/src'),['.ts','.tsx','.js']),...walk(path.join(root,'canvas-lab'),['.js']),...walk(path.join(root,'third_party/nova-image-studio/frontend/src'),['.ts','.tsx','.js']).filter(p=>!/[\\/]__tests__[\\/]|\.test\./.test(p))];
const frontendShapes={},callers=[],frontendTrees=[],unresolved=[];
function ownerName(n) {let variable;for(let p=n.parent;p;p=p.parent) {if(ts.isFunctionDeclaration(p)&&p.name)return p.name.text;if(!variable&&ts.isVariableDeclaration(p)&&p.name)variable=p.name.getText();}return variable||'<module>';}
function urlTemplate(n) {
  if(ts.isStringLiteralLike(n))return n.text;
  if(ts.isTemplateExpression(n))return n.head.text+n.templateSpans.map(p=>{
    const e=p.expression.getText();
    return (/query|Query\(|analyticsQuery\(/.test(e)||e.includes('?'))?'': '{'+e+'}';
  }).map((s,i)=>s+n.templateSpans[i].literal.text).join('');
  if(ts.isBinaryExpression(n)&&n.operatorToken.kind===ts.SyntaxKind.PlusToken) return (urlTemplate(n.left)||'{dynamic}')+(urlTemplate(n.right)||'{dynamic}');
  return null;
}
for(const file of frontendFiles) {
  const s=read(file),tree=ts.createSourceFile(file,s,ts.ScriptTarget.Latest,true);frontendTrees.push({file,s,tree});
  const constants=new Map();
  function constant(n){if(ts.isVariableDeclaration(n)&&ts.isIdentifier(n.name)&&n.initializer){const u=urlTemplate(n.initializer);if(u)constants.set(n.name.text,u);}ts.forEachChild(n,constant);}constant(tree);
  function visit(n) {
    if((ts.isInterfaceDeclaration(n)||ts.isTypeAliasDeclaration(n))&&/lib[\\/](api|ccode-task-client|ccode-task-socket)\.ts$/.test(file))frontendShapes[rel(file)+'#'+n.name.text]={source:rel(file),line:line(s,n.getStart()),declaration:n.getText()};
    if(ts.isCallExpression(n)&&/^(apiFetch|fetch|fetchWithTimeout)$/.test(n.expression.getText())) {
      const url=n.arguments[0]&&(urlTemplate(n.arguments[0])||(ts.isIdentifier(n.arguments[0])?constants.get(n.arguments[0].text):null));
      const options=n.arguments[1],method=options&&ts.isObjectLiteralExpression(options)?options.properties.find(p=>p.name?.getText()==='method'):null;
      const entry={source:rel(file),line:line(s,n.getStart()),symbol:ownerName(n),transport:n.expression.getText(),method:method?.initializer?.text||'GET',url:url||n.arguments[0]?.getText()||'',responseType:n.typeArguments?.map(t=>t.getText()).join(', ')||'untyped fetch',uiConsumers:[]};
      if(url?.includes('/api/')) {entry.path=url.slice(url.indexOf('/api/')).split('?')[0];callers.push(entry);}
      else unresolved.push(entry);
    }
    ts.forEachChild(n,visit);
  }visit(tree);
}
for(const c of callers) {
  for(const {file,s,tree} of frontendTrees) {
    if(rel(file)===c.source) continue;
    const aliases=new Set();
    for(const n of tree.statements) if(ts.isImportDeclaration(n)&&n.importClause?.namedBindings&&ts.isNamedImports(n.importClause.namedBindings)) {
      const module=n.moduleSpecifier.text;
      const resolved=module.startsWith('@/')?rel(file).split('/src/')[0]+'/src/'+module.slice(2):module.startsWith('.')?rel(path.resolve(path.dirname(file),module)):null;
      if(!resolved||![resolved,resolved+'.ts',resolved+'.tsx',resolved+'/index.ts'].includes(c.source))continue;
      for(const el of n.importClause.namedBindings.elements)if((el.propertyName?.text||el.name.text)===c.symbol)aliases.add(el.name.text);
    }
    if(!aliases.size)continue;
    function visit(n){if(ts.isCallExpression(n)&&aliases.has(n.expression.getText()))c.uiConsumers.push({source:rel(file),line:line(s,n.getStart()),symbol:ownerName(n)});ts.forEachChild(n,visit);}visit(tree);
  }
}
const normalize=p=>p.replace(/\{[^{}]*\}/g,'{}').replace(/\/+$/,'');
const entries=[],controllers=[];
for(const {file,s} of sources.filter(x=>/@(?:RestController|Controller)\b/.test(x.s))) {
  const ms=methods(s),first=s.indexOf('public class'),baseMatch=s.slice(0,first).match(/@RequestMapping\(([^)]*)\)/),base=baseMatch?.[1].includes('AiWorkerAuthenticationFilter.PREFIX')?'/api/internal/picpilot-worker/v1':baseMatch?.[1].match(/"([^"]*)"/)?.[1]||'';
  const maps=[...s.matchAll(/@(Get|Post|Put|Patch|Delete|Request)Mapping\b/g)].filter(x=>x.index>first);
  controllers.push({source:rel(file),sha256:sha(s),endpointMethods:maps.length});
  for(const a of maps) {
    const mappingEnd=s[a.index+a[0].length]==='('?end(mask(s),a.index+a[0].length):a.index+a[0].length;
    const annotation=s.slice(a.index,mappingEnd+1),method=ms.find(m=>m.start>mappingEnd);
    if(!method)throw new Error('Missing method '+rel(file)+':'+line(s,a.index));
    const paths=annotation.match(/(?:value|path)\s*=\s*\{([^}]+)\}/)?.[1].match(/"([^"]*)"/g)?.map(v=>v.slice(1,-1))||[annotation.match(/"([^\"]*)"/)?.[1]||''];
    const verbs=a[1]==='Request'?[...annotation.matchAll(/RequestMethod\.(\w+)/g)].map(x=>x[1]):[a[1].toUpperCase()];
    if(!verbs.length)throw new Error('Unresolved verbs: '+annotation);
    const consumes=annotation.match(/consumes\s*=\s*([^,)]+)/)?.[1]?.replaceAll('"','').replace('MediaType.APPLICATION_JSON_VALUE','application/json').replace('MediaType.MULTIPART_FORM_DATA_VALUE','multipart/form-data')||'unspecified';
    const producers=annotation.match(/produces\s*=\s*([^,)]+)/)?.[1]||'application/json (default)';
    const shapeEvidence=[];
    function evidence(method,seen=new Set(),depth=0) {
      if(seen.has(method.name)||depth>3)return;seen.add(method.name);
      const keys=uniq([...method.body.matchAll(/\.put\(\s*"([^"]+)"/g)].map(x=>x[1]));
      const expressions=[...method.body.matchAll(/\breturn\s+([^;]+);/g)].map(x=>compact(x[1]));
      for(const x of method.body.matchAll(/Map\.of\s*\(/g)){const begin=x.index+x[0].length-1;const args=split(method.body.slice(begin+1,end(mask(method.body),begin)));for(let i=0;i<args.length;i+=2){const k=args[i].match(/^"([^"]+)"$/);if(k)keys.push(k[1]);}}
      shapeEvidence.push({source:rel(file),line:line(s,method.start),method:method.name,observedMapKeys:uniq(keys),returnExpressions:expressions});
      for(const called of ms)if(new RegExp('\\b'+called.name+'\\s*\\(').test(method.body))evidence(called,seen,depth+1);
    }evidence(method);
    for(const verb of verbs)for(const suffix of paths){
      const p=base+suffix,matching=callers.filter(c=>c.method===verb&&normalize(c.path)===normalize(p));
      const worker=base.includes('/picpilot-worker/');
      let responseKind=method.javaType.startsWith('ApiResponse<')?'ApiResponse':method.javaType==='ResponseEntity<Resource>'?'binary':method.javaType==='ResponseEntity<Void>'?'redirect':method.javaType==='String'?'text/plain':'raw JSON';
      const wireSchema=worker?({heartbeat:'heartbeatResponse',claim:'claimResponse',renew:'leaseResponse',uploadArtifact:'artifactResponse',complete:'terminalResponse',fail:'terminalResponse'}[method.name]||null):null;
      entries.push({id:path.basename(file,'.java')+'.'+method.name+':'+verb,method:verb,path:p,consumes,produces:producers,controller:path.basename(file,'.java'),handler:method.name,source:rel(file),line:line(s,a.index),request:{javaParameters:method.params},response:{kind:responseKind,javaType:method.javaType,envelope:responseKind==='ApiResponse'?{success:'boolean',code:'string',message:'string',data:'see Java type and client schemas',request_id:'string'}:null,protocolSchema:wireSchema,javaShapeRefs:Object.keys(shapes).filter(k=>method.javaType.includes(k)||method.javaType.includes(k.split('.').pop())),frontendTypes:matching.map(c=>({source:c.source,type:c.responseType})),sourceEvidence:shapeEvidence,completeness:wireSchema?'normative schema':matching.length?'Java declaration + static shape evidence + frontend declaration; runtime verification pending':'Java declaration + static shape evidence; runtime verification pending'},frontendCallers:matching,callerStatus:matching.length?'static caller found':'no direct typed browser caller found; not evidence of dead API',editions:{platform:'required',personal:/^\/api\/(admin|billing)|^\/api\/auth\/admin|benefit-config|benefits/.test(p)?'platform-only; personal policy must disable/remove dependency':'required or explicit personal adaptation'},goStatus:'unimplemented',acceptance:'HTTP/auth/persistence/wire and workflow acceptance not verified; pure library does not implement this endpoint'});
    }
  }
}
entries.sort((a,b)=>a.path.localeCompare(b.path)||a.method.localeCompare(b.method)||a.id.localeCompare(b.id));
const unmatched=callers.filter(c=>!entries.some(e=>e.frontendCallers.includes(c)));
const goRoutes=[];
for(const file of walk(path.resolve(out,'../../core/server'),['.go'])) {
  if(file.endsWith('_test.go'))continue;
  const s=read(file);
  for(const m of s.matchAll(/\.HandleFunc\("(GET|POST|PUT|PATCH|DELETE) ([^\"]+)"/g))goRoutes.push({method:m[1],path:m[2],source:path.relative(path.resolve(out,'../..'),file).replaceAll('\\','/'),line:line(s,m.index),status:'handler_present_parity_unverified'});
  if(path.basename(file)==='workers.go') {
    const source='core/server/workers.go';
    for(const m of s.matchAll(/\{\s*"(GET|POST) "\s*\+\s*wp\.BasePath\s*\+\s*"([^"]+)"/g))goRoutes.push({method:m[1],path:'/api/internal/picpilot-worker/v1'+m[2],source,line:line(s,m.index),status:'handler_present_parity_unverified'});
    for(const m of s.matchAll(/\.HandleFunc\("(GET|POST) "\s*\+\s*root\s*\+\s*"([^"]+)"/g))for(const root of ['/api/admin/ai-config','/api/me/ai-config'])goRoutes.push({method:m[1],path:root+m[2],source,line:line(s,m.index),status:'handler_present_parity_unverified'});
  }
}
for(const e of entries) {
  e.goEvidence=goRoutes.filter(r=>r.method===e.method&&normalize(r.path)===normalize(e.path));
  if(e.goEvidence.length){e.goStatus='handler_present_parity_unverified';e.acceptance='Handler registration source exists; mounted routing and original request/response/business-workflow parity still require acceptance. Worker adapter verification is documented separately.';}
}
const runtimeFile=path.join(out,'canvas-runtime-inventory.json');
const canvasRuntime=fs.existsSync(runtimeFile)?JSON.parse(read(runtimeFile)):null;
const nativeCallers=[];
for(const file of [path.join(root,'clients/picpilot-windows-native/src/PicPilot.Core/PicPilotApiClient.cs'),path.join(root,'clients/picpilot-android-native/app/src/main/java/com/suakitsu/picpilot/core/network/PicPilotApi.kt')]) {
  if(!fs.existsSync(file))continue;const s=read(file);
  s.split(/\r?\n/).forEach((v,i)=>{if(/"\/?api\//.test(v)||/@(?:GET|POST|PATCH|PUT|DELETE)\(/.test(v))nativeCallers.push({source:rel(file),line:i+1,declaration:v.trim()});});
}
const inventory={inventoryVersion:1,sourceRoot:root.replaceAll('\\','/'),snapshotDate:'2026-10-26',scope:'All annotated Java controllers in openImage/src/main/java and src/main, web/admin/vendored Nova TypeScript callers and consumers, canvas bridge, native client route references; excludes secrets, data, generated bundles and dependencies.',methodology:'Java lexical balanced mapping/method extraction, TypeScript AST caller/consumer/type extraction. Source evidence is not a promise about runtime branch fields. Unmatched/dynamic callers remain listed; no API is marked implemented by compilation alone.',summary:{controllers:controllers.length,endpointMethods:controllers.reduce((n,c)=>n+c.endpointMethods,0),endpointVariants:entries.length,uniqueMethodPaths:new Set(entries.map(e=>e.method+' '+e.path)).size,browserCallSites:callers.length,unmatchedBrowserCallSites:unmatched.length,goHandlersWithOriginalRoute:entries.filter(e=>e.goEvidence.length).length,verifiedParityHTTP:0},controllers,endpoints:entries,javaShapes:shapes,frontendShapes,unmatchedBrowserCallers:unmatched,dynamicFetchSites:unresolved,nativeCallerReferences:nativeCallers,canvasRuntime,newGoRoutes:goRoutes.filter(r=>!entries.some(e=>r.method===e.method&&normalize(r.path)===normalize(e.path)))};
fs.writeFileSync(path.join(out,'api-inventory.json'),JSON.stringify(inventory,null,2)+'\n');
const lines=['# Migration Parity Checklist','','Status: inventory complete for the declared source roots; all migrated HTTP endpoints are unimplemented/unverified. Pure protocol logic is not end-to-end parity.','','## Provenance','',`- Read-only reference: \`${inventory.sourceRoot}\`.`, '- Controller hashes, source locations, request signatures, response evidence and frontend callers are recorded in `api-inventory.json`.',`- ${inventory.summary.controllers} controllers; ${inventory.summary.endpointMethods} annotated methods; ${entries.length} method/content-type variants; ${inventory.summary.uniqueMethodPaths} unique method/path pairs.`,`- ${callers.length} browser API call sites; ${unmatched.length} unmatched static calls; ${unresolved.length} dynamic fetch sites require contextual review.`, '- Rebuild with `node docs/migration/build-inventory.cjs E:/picpilot/workspace/openImage`; reads source only and writes these two inventory files. Uses the original installed TypeScript parser without installing dependencies.', '- Source-level shapes are not executable acceptance results. Entity/record and frontend declarations are retained alongside map-return evidence; optional/dynamic branches still need mock contract tests.', '', '## Full Acceptance Scope','', '- [x] Two new GitHub repositories created PRIVATE, reported by parent/user on 2026-10-26. Source not pushed yet; no publication authorized.', '- [ ] Independently cloneable platform and personal repositories, verified core snapshot/version synchronization, clean-clone commands and private pushes.', '- [ ] Immutable edition policies. Personal has no platform billing/admin UI or API dependency; platform retains all account, credit, payment and administrator workflows.', '- [ ] PostgreSQL migrations, configuration, transactional isolation, ownership authorization, JWT/password/email-code/OAuth flows, rate limits, audit and safe error contracts.', '- [ ] Personal per-user encrypted image/text/video provider settings, credential isolation, validation and no plaintext credential response/logging.', '- [ ] Image/text/video adapters and routing: HTTP and pull worker; mock gateways, priority, disabled-provider behavior, failure/fallback, submission ambiguity and no duplicate paid jobs.', '- [ ] Durable queues: atomic FIFO claims, global provider and per-worker concurrency, multi-instance row locks, restart recovery, terminal CAS, cancellation, expired queue attempts and late callback rejection.', '- [ ] Local Worker v1 HTTP/TLS/auth adapters, one-time administrator token issuance/regeneration/revocation, persisted hash-only leases, signed input redirect and cross-origin Authorization stripping.', '- [ ] Worker artifacts: MIME/magic decode, normalized orientation/metadata, SHA-256 of source and stored bytes, byte limit, 50 MP limit, deduplicated ownership and storage cleanup.', '- [ ] Storage: local/object storage, ownership of references, upload/HEIC normalization, signed reads, download safety, uncropped full-resolution images and deletion/retention.', '- [ ] Complete project/listing copy/fact confirmation/image generation/listing-package/A+ and linked-A+ flows; history, deletion, retries and exact-once settlement.', '- [ ] Commerce/domestic suites: catalogs, slots, templates, estimates, references, generation, retries and administrator catalog editing.', '- [ ] Creative chat: messages, versions, estimates, ordered multiple references, generated artifacts and continuity; image translation and assistant workflows.', '- [ ] Video: configuration/model variants, script preview, queue/submit/poll/webhook reconciliation, delete/cancel, safe retry, output storage and refunds.', '- [ ] Platform billing: plans/channels/orders, Afdian/Ykmcn webhook validation, reconciliation, ledger, redeem codes, benefits, idempotent credits and refunds.', '- [ ] Platform administration: dashboard/trends/costs, users, credit adjustments, password/status, risk keywords/events, prompts/configuration, worker status/tokens, audit/text-call/image/video logs and messages.', '- [ ] Public files/avatars/benefit assets/video files and canvas worker input serving, correct cache/redirect/content headers, no sensitive path exposure.', '- [ ] Nova workspace AND infinite canvas: rebuild from corresponding source, not only copied dist; preserve projects/state, nodes/edges, upload/reference order, text/image/video generation, task polling and export.', '- [ ] Canvas bridge and runtime-only routes: postMessage origin/session checks, image.submit/image.status/image.generate/text.generate/video.generate; /api/nova/proxy/text, /api/nova/proxy/models, /api/nova/queue-status, /api/nova/tasks compatibility. These are not Java controller endpoints.', '- [ ] Native Windows/Android callers and embedded canvas compatibility, token/session isolation, media downloads and history; native route references are inventoried, not ported.', '- [ ] First-run wizard, upgrades, backups/restores, independently deployable platform/personal packages; README.md/README.en.md/README.ja.md commands verified.', '- [ ] Source/license/history/secret review and mock full-flow desktop/mobile browser acceptance. No live paid model calls or production data for verification.', '', '## Integration Boundary','', 'This work owns only `docs/migration` and `core/workerprotocol`. Router, database, provider adapters, image decoder/storage, authentication and billing integration belong to the parent. See `workerprotocol-integration.md` for pure library coverage and remaining tests. No endpoint below becomes implemented merely because its DTO or transition exists.', '', '## Endpoint Checklist','', '| Status | HTTP | Path | Handler | Response | Browser callers |', '| --- | --- | --- | --- | --- | --- |'];
lines[2]='Status: all original Java mappings enumerated; each endpoint remains unimplemented or handler-present/parity-unverified as recorded below. Pure protocol logic is not end-to-end parity.';
lines[lines.indexOf('## Integration Boundary')+2]='Initial scope was `docs/migration` and `core/workerprotocol`. The authorized second stage adds `core/store/worker*.go`, migration `0002_worker.sql`, and `core/server/workers.go` with tests. See `workerprotocol-integration.md` and `worker-http-integration.md`. Parent still owns route mounting, business dispatch/settlement, general auth/storage/provider APIs and full original parity. Source handler presence alone does not close an endpoint gate.';
lines.splice(lines.indexOf('## Full Acceptance Scope'),0,'## Parent Delegation','',`- ${inventory.summary.goHandlersWithOriginalRoute} original routes currently have Go handlers; presence is not a verified matching request/response/workflow contract. New Go setup/provider routes are listed separately.`, '- See `canvas-runtime-parity.md` and `canvas-runtime-inventory.json` for the high-priority Nova routes, bridge mapping, clean-source gaps, and full-scope backend work packages.', '');
for(const e of entries)lines.push(`| [ ] ${e.goStatus==='unimplemented'?'unimplemented':'handler present; parity unverified'} | ${e.method} | \`${e.path}\`${e.consumes!=='unspecified'?' ('+e.consumes+')':''} | ${e.controller}.${e.handler} | ${e.response.javaType.replaceAll('|','\\|')} | ${uniq(e.frontendCallers.map(c=>c.symbol)).join(', ')||'none found; see inventory'} |`);
lines.push('','## Known Evidence Gaps','','- Dynamic fetches and runtime-generated asset URLs are recorded separately; no-caller does not mean unused. Payment callbacks, OAuth redirects, worker routes and storage routes have non-browser consumers.', '- Static response keys are evidence, not a generated OpenAPI schema. Controller return expressions, referenced DTO/entity fields and exact frontend interface declarations must be checked against each runtime branch.', '- Personal-specific provider-settings and bootstrap APIs are new requirements and are not implied to exist in the original Java route list.', '- Canvas runtime APIs are implemented/intercepted outside Java; migration must retain both runtime behavior and Go business API compatibility.', '- Pure transition tests cannot establish database atomicity, provider concurrency, token revocation, image decoding or one-time billing. Parent-owned isolated integration tests remain acceptance gates.','');
fs.writeFileSync(path.join(out,'parity-checklist.md'),lines.join('\n'));
console.log(JSON.stringify(inventory.summary));
