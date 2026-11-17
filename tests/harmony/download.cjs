const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const ts = require(path.join(process.argv[2], 'tools/ohpm/node_modules/typescript/lib/typescript.js'));
const root = path.resolve(__dirname, '../..');
const files = new Map();
const modified = new Map();
let mode = 'ok', requests = 0, destroyed = 0;
const fileIo = {
  OpenMode: { CREATE: 1, READ_WRITE: 2, TRUNC: 4 },
  async stat(path) { if (!files.has(path)) throw Error('missing'); return { size: files.get(path).length, mtime: modified.get(path) ?? Date.now() / 1000 }; },
  async listFile(directory) { return [...files.keys()].filter(file => file.startsWith(directory + '/')).map(file => path.basename(file)); },
  async unlink(path) { files.delete(path); },
  async open(path) { files.set(path, []); return { fd: path }; },
  async write(fd, bytes) {
    if (mode === 'write-failure' && files.get(fd).length > 0) throw Error('disk full');
    const chunk = [...new Uint8Array(bytes)].slice(0, 2); files.get(fd).push(...chunk); return chunk.length;
  },
  async close() {}
};
const http = {
  RequestMethod: { GET: 'GET' },
  createHttp() {
    requests++;
    const events = {};
    return {
      on(name, callback) { events[name] = callback; },
      destroy() { destroyed++; },
      async requestInStream() {
        if (mode === 'hang') return new Promise(() => {});
        if (mode === 'http') return 404;
        events.dataReceive(Uint8Array.from([1, 2]).buffer);
        if (mode !== 'short') events.dataReceive(Uint8Array.from(mode === 'large' ? [3, 4, 5] : [3, 4]).buffer);
        events.dataEnd();
        return 200;
      }
    };
  }
};
const exportsObject = {};
vm.runInNewContext(ts.transpileModule(fs.readFileSync(path.join(root, 'clients/harmony/entry/src/main/ets/platform/ImageFiles.ets'), 'utf8'), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 }
}).outputText, {
  exports: exportsObject, ArrayBuffer, Uint8Array, Map, Date, Promise, console,
  setTimeout(callback) { if (mode === 'hang') queueMicrotask(callback); return 1; }, clearTimeout() {},
  require(name) {
    if (name === '@kit.CoreFileKit') return { fileIo, fileUri: { getUriFromPath: path => path } };
    if (name === '@kit.NetworkKit') return { http };
    if (name === '@kit.ImageKit') return {};
    if (name.endsWith('ClientMetadata')) return { ClientMetadata: { version: () => 'test' } };
    throw Error(name);
  }
});
const { ImageFiles } = exportsObject;
const settings = { server: 'https://test', connection: { token: 'session-one' } };
const entry = id => ({ id, attachment: { size: 4, width: 1, height: 1, mime: 'image/png' } });
(async () => {
  const progress = [];
  const uri = await ImageFiles.download(settings, entry('1'), '/cache', (n, total) => progress.push([n, total]));
  assert.deepEqual(files.get(uri), [1, 2, 3, 4]);
  assert.deepEqual(progress, [[2, 4], [4, 4]]);
  assert.equal(await ImageFiles.download(settings, entry('1'), '/cache'), uri);
  assert.equal(requests, 1);
  files.delete(uri);
  await ImageFiles.download(settings, entry('1'), '/cache');
  assert.equal(requests, 2);
  await ImageFiles.download(settings, entry('1'), '/cache', () => {}, true);
  assert.equal(requests, 3);
  await ImageFiles.download({ ...settings, connection: { token: 'session-two' } }, entry('1'), '/cache');
  assert.equal(requests, 4);
  for (const [index, failure] of ['short', 'large', 'http', 'hang'].entries()) {
    mode = failure;
    await assert.rejects(() => ImageFiles.download(settings, entry(String(index + 10)), '/cache'));
  }
  assert.equal(destroyed, requests);
  mode = 'ok';
  await ImageFiles.download(settings, entry('10'), '/cache');
  assert.equal(requests, 9);
  const fileCount = files.size;
  mode = 'write-failure';
  await assert.rejects(() => ImageFiles.download(settings, entry('99'), '/cache'));
  assert.equal(files.size, fileCount, 'Failed cache writes must remove partial files');
  mode = 'ok';
  const old = await ImageFiles.download(settings, entry('20'), '/cache');
  modified.set(old, Date.now() / 1000 - 8 * 86400);
  ImageFiles.retain(old);
  ImageFiles.retain(old);
  await ImageFiles.prune('/cache');
  assert(files.has(old));
  ImageFiles.release(old);
  await ImageFiles.prune('/cache');
  assert(files.has(old));
  ImageFiles.release(old);
  await ImageFiles.prune('/cache');
  assert(!files.has(old));
  console.log('PASS: stream integrity/progress, partial writes, cache reuse/session isolation, failure/timeout cleanup, active-file retention/pruning.');
})().catch(error => { console.error(error); process.exitCode = 1; });
