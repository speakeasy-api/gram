package mcp

import (
	"bytes"
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConsentScriptVerificationPolling(t *testing.T) {
	t.Parallel()

	node, err := exec.LookPath("node")
	require.NoError(t, err, "node is required to exercise the consent script")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "-e", `
const assert = require('node:assert/strict');
const vm = require('node:vm');
const source = require('node:fs').readFileSync(0, 'utf8');
const serverNow = 1700000000000;
for (const skew of [-60000, 0, 60000]) {
  for (const [hasDeadline, pending] of [[true, true], [false, true], [true, false]]) {
    const timers = [];
    let reloads = 0;
    vm.runInNewContext(source, {
      Date: { now: () => serverNow + skew },
      document: {
        body: { hasAttribute: () => false },
        querySelector(selector) {
          if (selector === '[data-verify-deadline-ms]' && hasDeadline) {
            return { getAttribute: () => String(serverNow + 12000) };
          }
          if (selector === '[data-validation="pending"]' && pending) return {};
          return null;
        },
        querySelectorAll: () => [],
      },
      window: {
        setTimeout: (callback, delay) => timers.push({ callback, delay }),
        location: { reload: () => reloads++ },
      },
    });
    const expected = hasDeadline && pending ? 1 : 0;
    assert.equal(timers.length, expected, JSON.stringify({ skew, hasDeadline, pending }));
    if (expected) {
      assert.equal(timers[0].delay, 2000);
      timers[0].callback();
      assert.equal(reloads, 1);
    }
  }
}
`)
	cmd.Stdin = bytes.NewReader(consentScriptData)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "consent script polling: %s", out)
}

func TestConsentScriptSettlesIdentityChainChecks(t *testing.T) {
	t.Parallel()

	node, err := exec.LookPath("node")
	require.NoError(t, err, "node is required to exercise the consent script")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "-e", `
const assert = require('node:assert/strict');
const vm = require('node:vm');
const source = require('node:fs').readFileSync(0, 'utf8');
const element = (attrs = {}) => ({
  attrs, className: '', textContent: '', children: [],
  getAttribute(name) { return name in this.attrs ? this.attrs[name] : null; },
  setAttribute(name, value) { this.attrs[name] = value; },
  removeAttribute(name) { delete this.attrs[name]; },
  appendChild(child) { this.children.push(child); },
});
const fallbackClass = 'text-muted-foreground interact:text-foreground';
const cards = {};
const statuses = ['connected', 'rejected', 'ready', 'error', 'offline'].map((id) => {
  const status = element({ 'data-chain-check': 'pending' });
  const fallback = element({ 'data-connect-fallback': '' });
  fallback.className = fallbackClass;
  const card = element({ 'data-remote-client': id });
  card.querySelector = (selector) => ({
    'input[name="state"]': { value: 'state-1' },
    'input[name="csrf_token"]': { value: 'csrf-1' },
    'button[data-connect-fallback]': fallback,
  })[selector] || null;
  status.closest = (selector) => (selector === '[data-remote-client]' ? card : null);
  cards[id] = { status, fallback };
  return status;
});
const summary = element({ 'data-connected-count': '1', 'data-connected-total': '5' });
const root = element();
const dispatched = [];
const posts = [];
vm.runInNewContext(source, {
  URLSearchParams,
  Number,
  String,
  CustomEvent: class { constructor(type) { this.type = type; } },
  fetch(url, init) {
    const body = new URLSearchParams(init.body);
    posts.push({ url, method: init.method, body: Object.fromEntries(body) });
    const id = body.get('client_id');
    if (id === 'offline') return Promise.reject(new Error('offline'));
    if (id === 'error') return Promise.resolve({ ok: false, json: () => Promise.reject(new Error('no body')) });
    const result = id === 'connected' ? { status: 'connected', message: 'ignored' } : id === 'ready' ? { status: 'ready', message: 'ignored' } : { status: 'rejected', message: 'Example refused.' };
    return Promise.resolve({ ok: true, json: () => Promise.resolve(result) });
  },
  document: {
    body: { hasAttribute: () => false },
    documentElement: root,
    dispatchEvent(event) { dispatched.push(event.type); },
    createElement: () => element(),
    createTextNode: (text) => ({ text }),
    querySelector(selector) {
      if (selector === '[data-connected-summary]') return summary;
      if (selector === '[data-service-connections]') return element({ 'data-action-url': '/mcp/x/connect/remote-session' });
      return null;
    },
    querySelectorAll: (selector) => (selector === '[data-chain-check="pending"]' ? statuses : []),
  },
  window: { setTimeout: () => {}, location: { reload: () => {} } },
});

for (const status of statuses) {
  assert.equal(status.children.length, 2, 'checking shows a spinner and label');
  assert.equal(status.children[1].text, 'Connecting through your identity provider…');
}
assert.deepEqual(posts.map((p) => p.body), ['connected', 'rejected', 'ready', 'error', 'offline'].map((id) => ({
  state: 'state-1', csrf_token: 'csrf-1', action: 'identity_chaining_check', client_id: id,
})));
assert.ok(posts.every((p) => p.url === '/mcp/x/connect/remote-session' && p.method === 'POST'));

setTimeout(() => {
  const ok = cards.connected;
  assert.equal(ok.status.getAttribute('data-chain-check'), 'connected');
  assert.equal(ok.status.className, 'text-xs text-default-success');
  assert.equal(ok.status.textContent, 'Service connected through your identity provider');
  assert.equal(ok.fallback.className, fallbackClass, 'connected keeps the quiet fallback');
  assert.equal(summary.textContent, '2 of 5 connected');

  const ready = cards.ready;
  assert.equal(ready.status.getAttribute('data-chain-check'), 'unknown', 'an unrecognized status reads neutral');
  assert.equal(ready.status.textContent, 'Managed by your identity provider');
  assert.equal(ready.fallback.className, fallbackClass, 'a neutral card keeps separate OAuth optional');
  assert.equal(summary.textContent, '2 of 5 connected', 'a neutral card does not claim a downstream token');

  const no = cards.rejected;
  assert.equal(no.status.getAttribute('data-chain-check'), 'rejected');
  assert.equal(no.status.className, 'text-xs text-default-warning');
  assert.equal(no.status.textContent, 'Example refused.');
  assert.match(no.fallback.className, /\bbg-primary\b/);
  assert.equal(no.fallback.textContent, 'Connect');
  assert.equal(no.fallback.getAttribute('data-connect-fallback'), null);
  assert.equal(root.getAttribute('data-consent-chain-rejected'), 'true', 'one refusal signals the tool access island');
  assert.deepEqual(dispatched, ['gram:consent-chain-rejected']);

  for (const id of ['error', 'offline']) {
    const c = cards[id];
    assert.equal(c.status.getAttribute('data-chain-check'), 'unknown', id);
    assert.equal(c.status.className, 'text-xs text-muted-foreground', id);
    assert.equal(c.status.textContent, 'Managed by your identity provider', id);
    assert.equal(c.fallback.className, fallbackClass, id);
  }
}, 50);
`)
	cmd.Stdin = bytes.NewReader(consentScriptData)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "consent script chain checks: %s", out)
}

func TestConsentScriptPreservesDiscoveryChoice(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "-e", `
const assert = require('node:assert/strict');
const vm = require('node:vm');
const source = require('node:fs').readFileSync(0, 'utf8');
const saved = new Map();
function render(state) {
  const select = { value: '', addEventListener: (name, fn) => { select[name] = fn; } };
  const form = { elements: { state: { value: state } }, querySelector: () => null, addEventListener() {} };
  vm.runInNewContext(source, {
    sessionStorage: { getItem: key => saved.get(key) ?? null, setItem: (key, value) => saved.set(key, value) },
    document: { body: { hasAttribute: () => false }, querySelectorAll: () => [], querySelector: selector => {
      if (selector === 'form[data-approve-form]') return form;
      if (selector === 'select[name="discovery_mode"]') return select;
      return null;
    } },
    window: {},
  });
  return select;
}
const first = render('challenge-one');
assert.equal(first.value, '');
first.value = 'direct'; first.change();
assert.equal(render('challenge-one').value, 'direct');
assert.equal(render('challenge-two').value, '');
const restored = render('challenge-one');
restored.value = ''; restored.change();
assert.equal(render('challenge-one').value, '');
saved.set('gram-consent-discovery-v1:challenge-one', 'unknown');
assert.equal(render('challenge-one').value, '');
`)
	cmd.Stdin = bytes.NewReader(consentScriptData)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "consent discovery persistence: %s", out)
}

func TestConsentScriptAgentSelection(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "-e", `
const assert = require('node:assert/strict');
const vm = require('node:vm');
const source = require('node:fs').readFileSync(0, 'utf8');
const handlers = {};
const master = { checked: true, addEventListener: (_, fn) => handlers.master = fn };
const agents = ['One', 'Two', 'Three'].map(name => ({
  checked: true, disabled: false, value: name,
  dataset: { agentName: name, bindingId: name }
}));
const header = { hidden: false };
const choices = { hidden: false };
const summary = {};
const error = {};
const requests = [];
const approval = { addEventListener: (_, fn) => handlers.submit = fn, requestSubmit: () => {} };
const form = {
  dataset: { actionUrl: '/delegate' },
  hasAttribute: () => true,
  addEventListener: (_, fn) => handlers.change = fn,
  querySelector: selector => ({
    '[data-delegation-enabled]': master,
    '[data-delegation-header]': header,
    '#delegation-agent-choices': choices,
    '[data-delegation-summary]': summary,
    '[data-delegation-error]': error
  })[selector] || null,
  querySelectorAll: selector => selector.includes('agent_id')
    ? agents.filter(a => !a.disabled && (!selector.includes(':checked') || a.checked))
    : []
};
vm.runInNewContext(source, {
  URLSearchParams,
  document: {
    body: { hasAttribute: () => false },
    querySelector: selector => ({
      '[data-delegation-form]': form,
      '[data-approve-form]': approval,
      '#consent-approve-button': {}
    })[selector] || null,
    querySelectorAll: () => []
  },
  window: {},
  fetch: async (_, options) => { requests.push(options.body); return { ok: true }; }
});
assert.equal(master.checked, true);
master.checked = false;
handlers.master(); handlers.change();
assert.ok(agents.every(a => !a.checked));
assert.equal(master.indeterminate, false);
assert.equal(header.hidden, false);
assert.equal(choices.hidden, false);
assert.equal(summary.textContent, 'No agents selected');
master.checked = true;
handlers.master(); handlers.change();
assert.ok(agents.every(a => a.checked));
agents[0].checked = false; handlers.change();
assert.equal(master.checked, false);
assert.equal(master.indeterminate, true);
handlers.submit({ preventDefault() {}, stopImmediatePropagation() {} });
setImmediate(() => {
  assert.deepEqual(requests.filter(b => b.get('action') === 'delegate_grant').map(b => b.get('agent_id')), ['Two', 'Three']);
  assert.equal(requests.at(-1).get('action'), 'finish_delegation');
  assert.equal(header.hidden, false);
});
`)
	cmd.Stdin = bytes.NewReader(consentScriptData)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "consent selection: %s", out)
}
