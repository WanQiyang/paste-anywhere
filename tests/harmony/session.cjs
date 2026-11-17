const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const ts = require(path.join(process.argv[2], 'tools/ohpm/node_modules/typescript/lib/typescript.js'));
const root = path.resolve(__dirname, '../../clients/harmony/entry/src/main/ets');
const state = new Map();
const settings = { server: 'https://test', connection: {
  user: { id: '1', name: 'Alice' }, device: { id: 'd', name: 'Phone', userId: '1' }, token: 'a'.repeat(64)
} };
const pending = new Map();
const retained = new Set();
let requests = 0, failUpload = false, nextId = 1, downloadResolve;
function load(file, imports) {
  const exports = {};
  vm.runInNewContext(ts.transpileModule(fs.readFileSync(path.join(root, file), 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 }
  }).outputText, { exports, require: name => {
    if (!(name in imports)) throw Error(`Unexpected import ${name}`);
    return imports[name];
  }, console: { error() {} }, ArrayBuffer, Uint8Array, setTimeout, clearTimeout, clearInterval,
  AppStorage: { setOrCreate: (key, value) => state.set(key, value), get: key => state.get(key) } });
  return exports;
}
const types = load('networking/RemoteTypes.ets', {});
const { RemoteClipboard } = load('services/RemoteClipboard.ets', {
  '@kit.ArkData': { preferences: { getPreferences: async () => ({ get: async () => JSON.stringify(settings), delete: async () => {}, flush: async () => {} }) } },
  '@kit.NetworkKit': { http: {
    RequestMethod: { GET: 'GET', POST: 'POST', DELETE: 'DELETE' }, HttpDataType: { STRING: 'STRING' },
    createHttp: () => ({ destroy() {}, request: async (_url, options) => {
      requests++;
      if (options.method === 'DELETE') return { responseCode: 204, result: '' };
      if (failUpload) throw Error('network failure');
      return { responseCode: 201, result: JSON.stringify({ id: String(nextId++), kind: 'image' }) };
    } })
  } },
  '../networking/RemoteTypes': types,
  '../networking/ClientMetadata': { ClientMetadata: { version: () => 'test' } },
  '../platform/HarmonyPush': { HarmonyPush: { token: async () => '' } },
  '../platform/ImageFiles': { ImageFiles: {
    prune: async () => {}, retain: uri => retained.add(uri), release: uri => retained.delete(uri),
    uploadBytes: uri => new Promise(resolve => pending.set(uri, resolve)),
    download: () => new Promise(resolve => { downloadResolve = resolve; })
  } }
});
(async () => {
  await RemoteClipboard.init({ cacheDir: '/cache' });
  RemoteClipboard.incoming();
  const old = RemoteClipboard.upload('', 'old');
  RemoteClipboard.incoming();
  const current = RemoteClipboard.upload('', 'current');
  assert.equal(state.get('uploading'), true);
  await assert.rejects(() => RemoteClipboard.clearClipboard());
  await assert.rejects(() => RemoteClipboard.logout());
  assert.equal(requests, 0);
  pending.get('old')(new ArrayBuffer(4));
  await old;
  assert.equal(requests, 0, 'Superseded image must not upload');
  assert.equal(state.get('uploading'), true, 'Other upload still active');
  pending.get('current')(new ArrayBuffer(4));
  await current;
  assert.equal(state.get('uploading'), false);
  assert.equal(retained.size, 0);
  assert.equal(state.get('remoteEntries')[0].id, '1');
  failUpload = true;
  const failed = RemoteClipboard.upload('', 'failed');
  pending.get('failed')(new ArrayBuffer(4));
  await assert.rejects(() => failed);
  assert.equal(state.get('uploading'), false);
  assert.equal(retained.size, 0);
  const preview = RemoteClipboard.prepare({ id: '1', kind: 'image' });
  const available = RemoteClipboard.entryAvailable({ id: '1' });
  assert.equal(available(), true);
  await RemoteClipboard.logout();
  assert.equal(available(), false, 'Previously prepared content cannot be used after logout');
  downloadResolve('/cache/image.png');
  await assert.rejects(() => preview, /配对状态已改变/);
  console.log('PASS: overlapping uploads, stale-result rejection, busy guards, failure cleanup and preview session invalidation.');
})().catch(error => { console.error(error); process.exitCode = 1; });
