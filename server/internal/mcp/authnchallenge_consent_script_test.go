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
