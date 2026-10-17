import assert from "node:assert/strict";
import { test } from "node:test";

import type { Answer } from "./jev.ts";
import { judge, summarize, toLine, type CaseRecord } from "./record.ts";

function answers(
  choice: string,
  p: number,
  broken: number,
): Record<string, Answer> {
  return {
    state: { type: "choice", choice, probabilities: { [choice]: p } },
    broken: { type: "noul", noul: broken },
  };
}

test("judge passes an expected confident state with no defect", () => {
  assert.equal(
    judge(new Set(["empty"]), answers("empty", 0.9, 0.1)).verdict,
    "pass",
  );
});

test("judge fails a confident unexpected state", () => {
  const j = judge(new Set(["error"]), answers("broken", 0.8, 0.2));
  assert.equal(j.verdict, "fail");
  assert.match(j.reason, /saw broken/);
});

test("judge fails on an unexpected defect even when the state matches", () => {
  const j = judge(new Set(["loaded"]), answers("loaded", 0.95, 0.7));
  assert.equal(j.verdict, "fail");
  assert.match(j.reason, /broken=0.70/);
});

test("judge is uncertain below the confidence margin", () => {
  assert.equal(
    judge(new Set(["loaded"]), answers("loaded", 0.55, 0.1)).verdict,
    "uncertain",
  );
  assert.equal(
    judge(new Set(["loaded"]), answers("empty", 0.5, 0.1)).verdict,
    "uncertain",
  );
  assert.equal(
    judge(new Set(["loaded"]), { state: { type: "choice" } }).verdict,
    "uncertain",
  );
});

test("records serialize one JSON object per line and summarize by verdict", () => {
  const base: CaseRecord = {
    run_id: "r",
    commit: "c",
    at: "t",
    screen: "/mcp",
    mutation: "empty",
    expected: ["empty"],
    verdict: "pass",
    reason: "",
  };
  const line = toLine(base);
  assert.ok(line.endsWith("\n"));
  assert.equal(line.split("\n").length, 2);
  assert.deepEqual(
    summarize([
      base,
      { ...base, verdict: "fail" },
      { ...base, verdict: "skipped" },
    ]),
    {
      pass: 1,
      fail: 1,
      uncertain: 0,
      skipped: 1,
    },
  );
});
