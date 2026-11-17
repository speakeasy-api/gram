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
        addEventListener() {},
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
const fallbackClass = 'text-foreground interact:underline';
const cards = {};
const fallbacks = [];
const statuses = ['connected', 'rejected', 'ready', 'error', 'offline', 'wide'].map((id) => {
  const status = element({ 'data-chain-check': 'pending' });
  const fallback = element({ 'data-connect-fallback': '' });
  fallback.className = fallbackClass;
  const card = element({ 'data-remote-client': id, 'data-remote-display': id === 'wide' ? 'A Very Long Service Name Workspace' : 'Example' });
  card.offsetWidth = id === 'wide' ? 400 : 600;
  // The label's width follows its text, as it does on the page.
  Object.defineProperty(fallback, 'offsetWidth', { get() { return this.textContent.length * 8; } });
  card.querySelector = (selector) => ({
    'input[name="state"]': { value: 'state-1' },
    'input[name="csrf_token"]': { value: 'csrf-1' },
    'button[data-connect-fallback]': fallback,
  })[selector] || null;
  status.closest = (selector) => (selector === '[data-remote-client]' ? card : null);
  fallback.closest = status.closest;
  fallbacks.push(fallback);
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
    querySelectorAll: (selector) => (selector === '[data-chain-check="pending"]' ? statuses : selector === 'button[data-connect-fallback]' ? fallbacks : []),
  },
  window: { setTimeout: () => {}, addEventListener() {}, location: { reload: () => {} } },
});

const label = (fallback) => fallback.textContent + ' ' + fallback.children[fallback.children.length - 1].textContent;
for (const id of ['connected', 'rejected', 'ready', 'error', 'offline']) {
  assert.equal(label(cards[id].fallback), 'Use your own Example account \u203a', id + ': one label in every state');
  assert.equal(cards[id].fallback.children[0].attrs['aria-hidden'], 'true', id + ': the chevron is decoration');
}
assert.equal(label(cards.wide.fallback), 'Use your own account \u203a', 'a label wider than half the card drops the service name');

for (const status of statuses) {
  assert.equal(status.children.length, 2, 'checking shows a spinner and label');
  assert.equal(status.children[1].text, 'Connecting through your identity provider…');
}
assert.deepEqual(posts.map((p) => p.body), ['connected', 'rejected', 'ready', 'error', 'offline', 'wide'].map((id) => ({
  state: 'state-1', csrf_token: 'csrf-1', action: 'identity_chaining_check', client_id: id,
})));
assert.ok(posts.every((p) => p.url === '/mcp/x/connect/remote-session' && p.method === 'POST'));

setTimeout(() => {
  const ok = cards.connected;
  assert.equal(ok.status.getAttribute('data-chain-check'), 'connected');
  assert.equal(ok.status.className, 'text-xs text-default-success');
  assert.equal(ok.status.textContent, 'Service connected through your identity provider');
  assert.equal(ok.fallback.className, fallbackClass, 'connected keeps the same link');
  assert.equal(summary.textContent, '2 of 5 connected');

  const ready = cards.ready;
  assert.equal(ready.status.getAttribute('data-chain-check'), 'unknown', 'an unrecognized status reads neutral');
  assert.equal(ready.status.textContent, 'Managed by your identity provider');
  assert.equal(ready.fallback.className, fallbackClass, 'a neutral card keeps the same link');
  assert.equal(summary.textContent, '2 of 5 connected', 'a neutral card does not claim a downstream token');

  const no = cards.rejected;
  assert.equal(no.status.getAttribute('data-chain-check'), 'rejected');
  assert.equal(no.status.className, 'text-xs text-default-warning');
  assert.equal(no.status.textContent, 'Example refused.');
  assert.equal(no.fallback.className, fallbackClass, 'a refusal keeps the same link, not a filled button');
  assert.equal(label(no.fallback), 'Use your own Example account \u203a');
  assert.equal(root.getAttribute('data-consent-chain-rejected'), 'true', 'one refusal signals the tool access island');
  assert.deepEqual(dispatched, ['gram:consent-chain-rejected', 'gram:consent-chain-rejected'], 'each refusal signals it');


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

func TestConsentScriptOffersAccountSwitch(t *testing.T) {
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
  attrs, className: '', textContent: '', children: [], listeners: {}, style: {},
  getAttribute(name) { return name in this.attrs ? this.attrs[name] : null; },
  setAttribute(name, value) { this.attrs[name] = value; },
  removeAttribute(name) { delete this.attrs[name]; },
  appendChild(child) { this.children.push(child); },
  addEventListener(type, listener) { this.listeners[type] = listener; },
});
const rowClass = 'flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between';
const actionsClass = 'flex shrink-0 flex-wrap items-center justify-start gap-1.5 sm:justify-end';
const linkClass = 'text-foreground interact:underline flex h-8 items-center gap-1 whitespace-nowrap text-xs';
const cards = {};
const switches = [];
const fallbacks = [];
const windowListeners = {};
let reloads = 0;
const statuses = ['personal', 'admin'].map((id) => {
  const status = element({ 'data-chain-check': 'pending' });
  const fallback = element({ 'data-connect-fallback': '' });
  fallback.className = linkClass;
  fallback.offsetWidth = 200;
  const switchAccount = element({ 'data-switch-account': '', hidden: '' });
  switches.push(switchAccount);
  const row = element({ 'data-card-row': '' });
  row.className = rowClass;
  const actions = element({ 'data-card-actions': '' });
  actions.className = actionsClass;
  const card = element({ 'data-remote-client': id, 'data-remote-display': 'Example' });
  card.offsetWidth = 730;
  card.querySelector = (selector) => ({
    'input[name="state"]': { value: 'state-1' },
    'input[name="csrf_token"]': { value: 'csrf-1' },
    'button[data-connect-fallback]': fallback,
    'button[data-switch-account]': switchAccount,
    '[data-card-row]': row,
    '[data-card-actions]': actions,
  })[selector] || null;
  status.closest = (selector) => (selector === '[data-remote-client]' ? card : null);
  fallback.closest = status.closest;
  fallbacks.push(fallback);
  cards[id] = { status, fallback, switchAccount, row, actions };
  return status;
});
vm.runInNewContext(source, {
  URLSearchParams, Number, String, Math,
  CustomEvent: class { constructor(type) { this.type = type; } },
  fetch(url, init) {
    const id = new URLSearchParams(init.body).get('client_id');
    const result = id === 'admin'
      ? { status: 'rejected', message: 'Ask your administrator.' }
      : { status: 'rejected', message: 'Personal account.', action: 'switch_account' };
    return Promise.resolve({ ok: true, json: () => Promise.resolve(result) });
  },
  document: {
    body: { hasAttribute: () => false },
    documentElement: element(),
    dispatchEvent() {},
    createElement: () => element(),
    createTextNode: (text) => ({ text }),
    querySelector(selector) {
      if (selector === '[data-service-connections]') return element({ 'data-action-url': '/mcp/x/connect/remote-session' });
      return null;
    },
    querySelectorAll: (selector) => {
      if (selector === '[data-chain-check="pending"]') return statuses;
      if (selector === 'button[data-switch-account]') return switches;
      if (selector === 'button[data-connect-fallback]') return fallbacks;
      return [];
    },
  },
  window: {
    setTimeout: (fn) => fn(),
    getComputedStyle: () => ({ flexDirection: 'row' }),
    addEventListener(type, fn) { windowListeners[type] = fn; },
    location: { reload: () => { reloads++; } },
  },
});

setTimeout(() => {
  const label = (link) => link.textContent + ' ' + link.children[link.children.length - 1].textContent;
  const personal = cards.personal;
  assert.equal(personal.status.textContent, 'Personal account.');
  assert.equal(personal.switchAccount.getAttribute('hidden'), null, 'the company sign-in is shown');
  assert.equal(personal.row.className, rowClass, 'the card keeps the row every card uses');
  assert.equal(personal.actions.className, 'flex flex-col items-start sm:items-end', 'the link sits under the company sign-in');
  assert.equal(personal.fallback.className, linkClass, 'the same row link as every other state');
  assert.equal(label(personal.fallback), 'Use your own Example account \u203a');

  const admin = cards.admin;
  assert.equal(admin.switchAccount.getAttribute('hidden'), '', 'no switch for an administrator refusal');
  assert.equal(admin.actions.className, actionsClass);
  assert.equal(admin.fallback.className, linkClass);
  assert.equal(label(admin.fallback), 'Use your own Example account \u203a');

  personal.switchAccount.listeners.click({ preventDefault() {} });
  assert.equal(personal.switchAccount.getAttribute('aria-disabled'), 'true', 'the first click marks the switch busy');
  let secondPrevented = false;
  personal.switchAccount.listeners.click({ preventDefault() { secondPrevented = true; } });
  assert.equal(secondPrevented, true, 'a second click does nothing');

  windowListeners.pageshow({ persisted: false });
  assert.equal(reloads, 0);
  windowListeners.pageshow({ persisted: true });
  assert.equal(reloads, 1, 'Back from the sign-in reloads a restored page');
}, 50);
`)
	cmd.Stdin = bytes.NewReader(consentScriptData)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "consent script account switch: %s", out)
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
    window: { addEventListener() {} },
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
