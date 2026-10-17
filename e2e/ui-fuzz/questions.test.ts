import assert from "node:assert/strict";
import { test } from "node:test";

import { buildRequest } from "./jev.ts";
import {
  MAX_SNAPSHOT_CHARS,
  QUESTION_BROKEN,
  QUESTION_STATE,
  SCREEN_STATES,
  buildStateDocument,
  screenQuestions,
} from "./questions.ts";

test("screen questions are well formed and cover every screen state", () => {
  const qs = screenQuestions();
  assert.doesNotThrow(() => buildRequest({}, qs));
  const state = qs[QUESTION_STATE];
  assert.equal(state?.type, "choice");
  assert.deepEqual(
    Object.keys(state?.criteria ?? {}).sort(),
    [...SCREEN_STATES].sort(),
  );
  assert.equal(qs[QUESTION_BROKEN]?.type, "noul");
});

test("state document truncates oversized snapshots and says so", () => {
  const screen = { route: "/mcp", purpose: "lists servers" };
  const small = buildStateDocument(screen, "- main:\n  - heading");
  assert.equal(small.snapshot_truncated, false);
  const big = buildStateDocument(screen, "x".repeat(MAX_SNAPSHOT_CHARS + 10));
  assert.equal(big.snapshot_truncated, true);
  assert.match(big.snapshot, /snapshot truncated/);
  assert.ok(big.snapshot.length < MAX_SNAPSHOT_CHARS + 100);
});
