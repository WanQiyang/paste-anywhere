const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const ts = require(path.join(process.argv[2], 'tools/ohpm/node_modules/typescript/lib/typescript.js'));
let active = true, writes = 0, uploads = 0, released = 0;
let resolvePixels, signalDecode, resolveShare, signalShare;
const decodeStarted = new Promise(resolve => { signalDecode = resolve; });
const shareStarted = new Promise(resolve => { signalShare = resolve; });
const imports = {
  '@kit.ShareKit': { systemShare: { getSharedData: () => new Promise(resolve => { resolveShare = resolve; signalShare(); }) } },
  '@kit.ArkData': { uniformTypeDescriptor: {
    UniformDataType: { IMAGE: 'image', TEXT: 'text' },
    getTypeDescriptor: () => ({ belongsTo: kind => kind === 'text' })
  } },
  '@kit.CoreFileKit': { fileIo: { OpenMode: { READ_ONLY: 0 }, open: async () => ({ fd: 1 }), close: async () => {} } },
  '@kit.ImageKit': { image: { createImageSource: () => ({
    createPixelMap: () => new Promise(resolve => { resolvePixels = resolve; signalDecode(); }), release: async () => {}
  }) } },
  '@kit.BasicServicesKit': { pasteboard: { createData: () => ({}), getSystemPasteboard: () => ({ setData: async () => { writes++; } }) } },
  '../services/RemoteClipboard': { RemoteClipboard: {
    sessionAvailable: () => () => active, incoming() {}, upload: async () => { uploads++; }
  } }
};
const exportsObject = {};
vm.runInNewContext(ts.transpileModule(fs.readFileSync(path.resolve(__dirname,
  '../../clients/harmony/entry/src/main/ets/platform/ClipboardAccess.ets'), 'utf8'), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 }
}).outputText, { exports: exportsObject, require: name => {
  if (!(name in imports)) throw Error(`Unexpected import ${name}`);
  return imports[name];
}, AppStorage: { setOrCreate() {} } });
const { ClipboardAccess } = exportsObject;
(async () => {
  const copying = ClipboardAccess.copy('', 'image', () => active);
  await decodeStarted;
  active = false;
  resolvePixels({ release: async () => { released++; } });
  await assert.rejects(() => copying);
  assert.equal(writes, 0, 'Clear/logout during image decoding must prevent clipboard writes');
  assert.equal(released, 1);
  await assert.rejects(() => ClipboardAccess.copy('old', '', () => false));
  assert.equal(writes, 0);
  active = true;
  const sharing = ClipboardAccess.receive({}, { action: 'ohos.want.action.sendData' });
  await shareStarted;
  active = false;
  resolveShare({ getRecords: () => [{ utd: 'text', content: 'old session' }] });
  await sharing;
  assert.equal(uploads, 0, 'Share data arriving after logout must not upload');
  await ClipboardAccess.copy('current', '', () => true);
  assert.equal(writes, 1);
  console.log('PASS: async clipboard write and incoming-share session guards.');
})().catch(error => { console.error(error); process.exitCode = 1; });
