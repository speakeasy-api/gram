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

func TestConsentScriptRequiresExplicitUnfreezeWhenReviewIsUnavailable(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	require.NoError(t, err)
	cmd := exec.CommandContext(t.Context(), node, "-e", `
const assert = require('node:assert/strict');
const vm = require('node:vm');
const script = require('node:fs').readFileSync(0, 'utf8');
function render(ready) {
 const events = {};
 const button = { disabled: false, value: 'approve', getAttribute: name => name === 'data-consent-self-ready' ? String(ready) : null };
 const frozen = { checked: true, disabled: false, hasAttribute: name => name === 'data-review-unavailable', addEventListener: (name, listener) => { events[name] = listener; } };
 const form = { elements: { state: { value: 'review-state' } }, querySelector: () => button, addEventListener() {} };
 const document = { body: { hasAttribute: () => false }, readyState: 'complete', querySelector: selector => selector === 'form[data-approve-form]' ? form : selector === 'input[name="gateway_freeze"]' ? frozen : null, querySelectorAll: () => [], addEventListener() {} };
 vm.runInNewContext(script, { document, sessionStorage: { getItem: () => null, setItem() {} }, setTimeout() {}, clearTimeout() {}, console });
 assert.equal(button.disabled, true);
 assert.equal(button.value, 'approve_frozen');
 frozen.checked = false; events.change();
 assert.equal(button.value, 'approve');
 assert.equal(button.disabled, !ready);
 frozen.checked = true; events.change();
 assert.equal(button.disabled, true);
}
render(true);
render(false);
`)
	cmd.Stdin = bytes.NewReader(consentScriptData)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "consent unavailable frozen review: %s", out)
}
