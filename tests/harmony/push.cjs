const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const ts = require(path.join(process.argv[2], 'tools/ohpm/node_modules/typescript/lib/typescript.js'));
const root = path.resolve(__dirname, '../../clients/harmony/entry/src/main/ets');
function load(file, imports) {
  const exports = {};
  vm.runInNewContext(ts.transpileModule(fs.readFileSync(path.join(root, file), 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 }
  }).outputText, { exports, require: name => {
    if (!(name in imports)) throw Error(`Unexpected import ${name}`);
    return imports[name];
  }, console: { error() {}, warn() {} }, AppStorage: { setOrCreate() {}, get() {} } });
  return exports;
}
let enabled = false, consent = false, prompts = 0, tokenCalls = 0, tokenError = false;
const { HarmonyPush } = load('platform/HarmonyPush.ets', {
  '@kit.NotificationKit': { notificationManager: {
    isNotificationEnabled: async () => enabled,
    requestEnableNotification: async () => { prompts++; enabled = consent; }
  } },
  '@kit.PushKit': { pushService: { getToken: async () => { tokenCalls++; if (tokenError) throw Error('SDK failure'); return 'push-token'; } } }
});
const saved = new Map();
const requests = [];
let getToken = async () => 'push-token', failRequest = false;
const { RemoteClipboard } = load('services/RemoteClipboard.ets', {
  '@kit.ArkData': {},
  '@kit.NetworkKit': { http: {
    RequestMethod: { PUT: 'PUT' }, HttpDataType: { STRING: 'STRING' },
    createHttp: () => ({ destroy() {}, request: async (url, options) => {
      requests.push({ url, options });
      if (failRequest) throw Error('network error');
      return { responseCode: 204, result: '' };
    } })
  } },
  '../networking/RemoteTypes': {}, '../platform/ImageFiles': {}, '../networking/ClientMetadata': { ClientMetadata: { version: () => 'test' } },
  '../platform/HarmonyPush': { HarmonyPush: { token: (...args) => getToken(...args) } }
});
(async () => {
  assert.equal(await HarmonyPush.token({}, true), '');
  assert.equal(prompts, 1);
  assert.equal(tokenCalls, 0);
  assert.equal(await HarmonyPush.token({}, false), '');
  assert.equal(prompts, 1, 'Do not prompt again after denial');
  enabled = true;
  assert.equal(await HarmonyPush.token({}, false), 'push-token', 'Settings authorization should allow token retrieval');
  tokenError = true;
  await assert.rejects(() => HarmonyPush.token({}, false), /无法获取推送令牌/);

  const settings = { server: 'https://test', connection: { token: 'device-credential' } };
  RemoteClipboard.settings = settings;
  RemoteClipboard.context = {};
  RemoteClipboard.active = true;
  RemoteClipboard.prefs = { get: async (key, fallback) => saved.get(key) ?? fallback,
    put: async (key, value) => saved.set(key, value), flush: async () => {} };
  const register = async () => { RemoteClipboard.pushNext = 0; await RemoteClipboard.registerPush(); };
  await register();
  assert.equal(requests[0].url, 'https://test/v1/push-token');
  assert.equal(requests[0].options.header.Authorization, 'Bearer device-credential');
  assert.equal(JSON.parse(requests[0].options.extraData).token, 'push-token');
  assert.equal(saved.get('pushPermissionRequested'), true);
  getToken = async (_context, prompt) => { assert.equal(prompt, false); return ''; };
  await register();
  assert.equal(JSON.parse(requests.at(-1).options.extraData).token, '', 'Disabled notifications unregister');
  failRequest = true;
  await register();
  assert.equal(RemoteClipboard.pushBusy, false);
  assert.ok(RemoteClipboard.pushNext - Date.now() < 61000, 'Failure retains short retry schedule');
  failRequest = false;
  await register();
  assert.ok(RemoteClipboard.pushNext - Date.now() > 290000, 'Success uses periodic refresh');

  let resolveToken, markStarted;
  const started = new Promise(resolve => { markStarted = resolve; });
  getToken = () => new Promise(resolve => { resolveToken = resolve; markStarted(); });
  const pending = register();
  await started;
  const count = requests.length;
  RemoteClipboard.settings = undefined;
  resolveToken('late-token');
  await pending;
  assert.equal(requests.length, count, 'Late token after logout must not register');
  assert.equal(RemoteClipboard.pushBusy, false);
  console.log('PASS: notification consent, disabled registration, retry schedule and stale-session rejection.');
})().catch(error => { console.error(error); process.exitCode = 1; });
