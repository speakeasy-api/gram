import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";

import type { CaseRecord } from "./record.ts";
import { mergeRun, render } from "./summarize.ts";

const rec = (
  screen: string,
  mutation: string,
  verdict: CaseRecord["verdict"],
): CaseRecord => ({
  run_id: "r",
  commit: "c",
  at: "t",
  screen,
  mutation,
  expected: ["loaded"],
  verdict,
  reason: verdict === "fail" ? "saw error@0.90" : "",
});

test("mergeRun joins per-worker files in a stable order and render lists failures", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "ui-fuzz-"));
  fs.writeFileSync(
    path.join(dir, "cases-w1.jsonl"),
    JSON.stringify(rec("mcp", "baseline", "pass")) + "\n",
  );
  fs.writeFileSync(
    path.join(dir, "cases-w0.jsonl"),
    [
      rec("assistants", "x#server-error", "fail"),
      rec("mcp", "a#empty-arrays", "skipped"),
    ]
      .map((r) => JSON.stringify(r))
      .join("\n") + "\n",
  );
  fs.writeFileSync(path.join(dir, "deps-w0.json"), "[]");
  const records = mergeRun(dir);
  assert.deepEqual(
    records.map((r) => `${r.screen} ${r.mutation}`),
    ["assistants x#server-error", "mcp a#empty-arrays", "mcp baseline"],
  );
  const text = render(records);
  assert.match(text, /3 cases/);
  assert.match(text, /fail\s+1/);
  assert.match(text, /assistants x#server-error: saw error@0.90/);
});
