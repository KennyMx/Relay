// Security regression checks execute the shipped JS with a minimal DOM.
// Node is test tooling only; the gateway embeds plain HTML/CSS/JS.
const {test} = require('node:test');
const assert = require('node:assert/strict');
const {readFileSync} = require('node:fs');
const {join} = require('node:path');
const vm = require('node:vm');

function consoleHarness(fetcher) {
  const elements = new Map();
  function element(id) {
    if (!elements.has(id)) {
      const hidden = new Set();
      elements.set(id, {
        value: '', textContent: '', innerHTML: '', type: 'password', dataset: {}, events: {},
        classList: {toggle(name, on) { on ? hidden.add(name) : hidden.delete(name); }, add() {}, remove() {}},
        addEventListener(name, fn) { this.events[name] = fn; }, setAttribute() {}, appendChild() {},
        replaceChildren() { this.innerHTML = ''; this.textContent = ''; }, close() {}, showModal() {}, focus() {},
      });
    }
    return elements.get(id);
  }
  const removed = [];
  const context = {
    document: {getElementById: element, querySelectorAll: () => [], createElement: () => element(Symbol())},
    sessionStorage: {removeItem: (name) => removed.push(name), setItem() { throw Error('credential storage forbidden'); }},
    window: {clearTimeout() {}, setTimeout() {}, setInterval() {}, addEventListener() {}},
    fetch: fetcher, Headers, AbortController, Intl, Date,
    location: {origin: 'http://localhost:8080'},
  };
  vm.runInNewContext(readFileSync(join(__dirname, 'ui/app.js'), 'utf8'), context);
  return {element, removed};
}

const reply = (data) => ({ok: true, status: 200, json: async () => data});
const trigger = async (element, type = 'click') => element.events[type]({preventDefault() {}, submitter: {dataset: {}, innerHTML: 'Submit'}});

test('disconnect clears credentials, prompts, response content, and prior session storage', async () => {
  const raw = 'rl_live_' + 'b'.repeat(64);
  const calls = [];
  const {element, removed} = consoleHarness(async (path, options) => {
    calls.push({path, options});
    if (path === '/v1/keys') return reply({id: 'key-id', api_key: raw});
    if (path === '/v1/routes') return reply({routes: [], key: {id: 'key-id', name: 'private key'}});
    if (path.startsWith('/v1/requests')) return reply({data: []});
    return reply({status: 'ok'});
  });
  element('adminTokenInput').value = 'private-admin';
  await trigger(element('createForm'), 'submit');
  assert.equal(element('createdKeyValue').textContent, raw);
  assert.equal(element('adminTokenInput').value, '');
  await trigger(element('useKeyButton'));
  assert.equal(element('createdKeyValue').textContent, '');
  assert.equal(calls.find((c) => c.path === '/v1/routes').options.headers.get('Authorization'), `Bearer ${raw}`);
  for (const id of ['requestRows', 'requestDetail', 'completionText']) element(id).textContent = 'private content';
  element('promptInput').value = 'private prompt';
  element('revokeAdminInput').value = 'private-admin';
  await trigger(element('disconnectButton'));
  for (const id of ['createdKeyValue', 'requestRows', 'requestDetail', 'completionText']) assert.equal(element(id).textContent, '');
  assert.equal(element('promptInput').value, '');
  assert.equal(element('revokeAdminInput').value, '');
  assert.deepEqual(removed, ['relay_api_key', 'relay_key_id']);
});

test('a late response cannot restore request data after disconnect', async () => {
  let release;
  const {element} = consoleHarness(async (path) => {
    if (path === '/v1/routes') return reply({routes: [], key: {id: 'key-id'}});
    if (path.startsWith('/v1/requests')) return new Promise((resolve) => { release = resolve; });
    return reply({status: 'ok'});
  });
  element('apiKeyInput').value = 'rl_live_' + 'c'.repeat(64);
  const connecting = trigger(element('connectForm'), 'submit');
  await trigger(element('disconnectButton'));
  release(reply({data: [{id: 'private-request', status: 'success', usage: {total_tokens: 123}}]}));
  await connecting;
  assert.equal(element('requestRows').innerHTML, '');
  assert.equal(element('requestMetric').textContent, '—');
});

test('credential-bearing browser requests refuse redirects', async () => {
  const calls = [];
  const {element} = consoleHarness(async (path, options) => {
    calls.push({path, options});
    return reply({routes: [], key: {id: 'key-id'}, data: []});
  });
  element('apiKeyInput').value = 'rl_live_' + 'd'.repeat(64);
  await trigger(element('connectForm'), 'submit');
  for (const call of calls.filter((c) => c.options.headers.has('Authorization'))) {
    assert.equal(call.options.redirect, 'error');
    assert.equal(call.options.credentials, 'omit');
    assert.ok(call.options.signal);
  }
});

test('an invalid key is cleared and visibly rejected', async () => {
  const {element} = consoleHarness(async (path) => {
    if (path === '/v1/routes') return {ok: false, status: 401, json: async () => ({error: {code: 'invalid_api_key'}})};
    return reply({data: []});
  });
  element('apiKeyInput').value = 'rl_live_' + 'e'.repeat(64);
  await trigger(element('connectForm'), 'submit');
  assert.equal(element('apiKeyInput').value, '');
  assert.equal(element('accessError').textContent, 'invalid api key');
});

test('history navigates older pages and separates simulated from provider cost', async () => {
  const paths = [];
  const rows = Array.from({length: 25}, (_, i) => ({id: `request-${i}`, status: 'success', usage: {total_tokens: 10, simulated: i !== 0}, cost_nano_usd: 1000000000}));
  const {element} = consoleHarness(async (path) => {
    paths.push(path);
    if (path === '/v1/routes') return reply({routes: [], key: {id: 'key-id'}});
    if (path.includes('offset=25')) return reply({data: []});
    if (path.startsWith('/v1/requests')) return reply({data: rows});
    return reply({status: 'ok'});
  });
  element('apiKeyInput').value = 'rl_live_' + 'f'.repeat(64);
  await trigger(element('connectForm'), 'submit');
  assert.equal(element('costMetric').textContent, '$1.00');
  assert.match(element('costCaption').textContent, /24\.00.*excluded/);
  assert.equal(element('previousPage').disabled, true);
  assert.equal(element('nextPage').disabled, false);
  await trigger(element('nextPage'));
  assert.ok(paths.includes('/v1/requests?limit=25&offset=25'));
  assert.equal(element('nextPage').disabled, true);
  assert.equal(element('previousPage').disabled, false);
  await trigger(element('previousPage'));
  assert.match(element('pageCaption').textContent, /Entries 1–25/);
});

test('failed completion exposes its request ID for inspection and clears stale output', async () => {
  const {element} = consoleHarness(async (path) => {
    if (path === '/v1/chat/completions') return {ok: false, status: 502, headers: new Headers({'X-Request-ID': 'failed-request'}), json: async () => ({error: {code: 'upstream_unavailable'}})};
    if (path === '/v1/requests/failed-request') return reply({id: 'failed-request', route: 'unavailable', status: 'error', attempts: []});
    return reply({routes: [], key: {id: 'key-id'}, data: []});
  });
  element('apiKeyInput').value = 'rl_live_' + 'a'.repeat(64);
  await trigger(element('connectForm'), 'submit');
  element('promptInput').value = 'hello';
  element('maxTokensInput').value = '64';
  await trigger(element('chatForm'), 'submit');
  assert.match(element('chatError').textContent, /upstream unavailable/);
  await trigger(element('inspectLastButton'));
  assert.match(element('requestJSON').textContent, /failed-request/);
  await trigger(element('disconnectButton'));
  assert.equal(element('requestJSON').textContent, '');
});

test('automatic decisions display confidence and escape untrusted metadata', async () => {
  const {element} = consoleHarness(async (path) => {
    if (path === '/v1/routes') return reply({routes: [{name:'auto',targets:[]}], auto_routing:{routes:{simple:'fast',standard:'balanced',complex:'reasoning'},fallback_route:'balanced',timeout_ms:1500},classifier:'jev',key:{id:'key-id'}});
    if (path === '/v1/chat/completions') return reply({id:'request-id',provider:'mock',model:'mock-fast',latency_ms:100,usage:{simulated:true,total_tokens:5},routing:{mode:'auto',route:'fast',reason:'classified',classification:{source:'jev',tier:'<img src=x>',confidence:.9,latency_ms:90,cost_nano_usd:500}}});
    return reply({data:[]});
  });
  element('apiKeyInput').value='rl_live_'+'e'.repeat(64);
  await trigger(element('connectForm'),'submit');
  element('routeSelect').value='auto';
  await trigger(element('routeSelect'),'change');
  assert.match(element('routeFlow').innerHTML,/Jev classification/);
  assert.equal(element('providerSelect').disabled,true);
  element('promptInput').value='hello';element('maxTokensInput').value='64';
  await trigger(element('chatForm'),'submit');
  assert.match(element('completionStats').innerHTML,/90% confidence/);
  assert.match(element('completionStats').innerHTML,/&lt;img src=x&gt;/);
  assert.doesNotMatch(element('completionStats').innerHTML,/<img/);
});
