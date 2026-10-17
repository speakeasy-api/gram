import assert from "node:assert/strict";
import { test } from "node:test";

import { LONG_NAME, mutators } from "./mutations.ts";

const byName = Object.fromEntries(mutators.map((m) => [m.name, m]));
const real = JSON.stringify({
  mcp_servers: [
    { id: "1", name: "jira", slug: "jira", tools: [{ name: "create_issue" }] },
  ],
  total: 1,
});

test("mutators that need the original skip cleanly without one or on non-JSON", () => {
  for (const m of mutators.filter((m) => m.needsOriginal)) {
    assert.equal(m.apply(undefined).kind, "skip", m.name);
    assert.equal(m.apply("<html>").kind, "skip", m.name);
  }
});

test("empty-arrays empties every array at any depth and skips when there are none", () => {
  const r = byName["empty-arrays"]?.apply(real);
  assert.equal(r?.kind, "respond");
  if (r?.kind !== "respond") return;
  assert.deepEqual(JSON.parse(r.response.body), { mcp_servers: [], total: 1 });
  assert.equal(
    byName["empty-arrays"]?.apply(JSON.stringify({ total: 1 })).kind,
    "skip",
  );
});

test("long-names stretches label-like strings only, at any depth", () => {
  const r = byName["long-names"]?.apply(real);
  assert.equal(r?.kind, "respond");
  if (r?.kind !== "respond") return;
  const body = JSON.parse(r.response.body) as {
    mcp_servers: {
      name: string;
      slug: string;
      id: string;
      tools: { name: string }[];
    }[];
  };
  const s = body.mcp_servers[0]!;
  assert.equal(s.name, LONG_NAME);
  assert.equal(s.tools[0]?.name, LONG_NAME);
  assert.equal(s.slug, "jira");
  assert.equal(s.id, "1");
  assert.equal(LONG_NAME.length, 5000);
  assert.equal(
    byName["long-names"]?.apply(JSON.stringify({ id: "x" })).kind,
    "skip",
  );
});

test("server-error and forbidden answer with a goa error envelope", () => {
  for (const [name, status] of [
    ["server-error", 500],
    ["forbidden", 403],
  ] as const) {
    const r = byName[name]?.apply(undefined);
    assert.equal(r?.kind, "respond", name);
    if (r?.kind !== "respond") continue;
    assert.equal(r.response.status, status);
    const body = JSON.parse(r.response.body) as {
      name: string;
      message: string;
    };
    assert.ok(body.name && body.message, name);
  }
});
