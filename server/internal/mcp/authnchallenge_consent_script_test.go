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
const statuses = ['connected', 'rejected', 'error', 'offline'].map((id) => {
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
const posts = [];
vm.runInNewContext(source, {
  URLSearchParams,
  Number,
  String,
  fetch(url, init) {
    const body = new URLSearchParams(init.body);
    posts.push({ url, method: init.method, body: Object.fromEntries(body) });
    const id = body.get('client_id');
    if (id === 'offline') return Promise.reject(new Error('offline'));
    if (id === 'error') return Promise.resolve({ ok: false, json: () => Promise.reject(new Error('no body')) });
    const result = id === 'connected' ? { status: 'connected', message: 'ignored' } : { status: 'rejected', message: 'Example refused.' };
    return Promise.resolve({ ok: true, json: () => Promise.resolve(result) });
  },
  document: {
    body: { hasAttribute: () => false },
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
assert.deepEqual(posts.map((p) => p.body), ['connected', 'rejected', 'error', 'offline'].map((id) => ({
  state: 'state-1', csrf_token: 'csrf-1', action: 'identity_chaining_check', client_id: id,
})));
assert.ok(posts.every((p) => p.url === '/mcp/x/connect/remote-session' && p.method === 'POST'));

setTimeout(() => {
  const ok = cards.connected;
  assert.equal(ok.status.getAttribute('data-chain-check'), 'connected');
  assert.equal(ok.status.className, 'text-xs text-default-success');
  assert.equal(ok.status.textContent, 'Connected through your identity provider');
  assert.equal(ok.fallback.className, fallbackClass, 'connected keeps the quiet fallback');
  assert.equal(summary.textContent, '2 of 5 connected');

  const no = cards.rejected;
  assert.equal(no.status.getAttribute('data-chain-check'), 'rejected');
  assert.equal(no.status.className, 'text-xs text-default-warning');
  assert.equal(no.status.textContent, 'Example refused.');
  assert.match(no.fallback.className, /\bbg-primary\b/);
  assert.equal(no.fallback.textContent, 'Connect');
  assert.equal(no.fallback.getAttribute('data-connect-fallback'), null);

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
