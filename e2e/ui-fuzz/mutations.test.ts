import assert from "node:assert/strict";
import { test } from "node:test";

import { LONG_NAME, mcpListMutations } from "./mutations.ts";

const byName = Object.fromEntries(mcpListMutations.map((m) => [m.name, m]));
const real = JSON.stringify({
  mcp_servers: [{ id: "1", name: "jira", slug: "jira" }],
});

test("mutations that need the original skip cleanly without one", () => {
  for (const m of mcpListMutations.filter((m) => m.needsOriginal)) {
    const r = m.apply(undefined);
    assert.equal(r.kind, "skip", m.name);
  }
});

test("empty and server-error never touch the original", () => {
  const empty = byName["empty"]?.apply(undefined);
  assert.equal(empty?.kind, "respond");
  if (empty?.kind === "respond") {
    assert.equal(empty.response.status, 200);
    assert.deepEqual(JSON.parse(empty.response.body), { mcp_servers: [] });
  }
  const err = byName["server-error"]?.apply(undefined);
  assert.equal(err?.kind, "respond");
  if (err?.kind === "respond") assert.equal(err.response.status, 500);
});

test("long-name clones the first server and prepends it", () => {
  const r = byName["long-name"]?.apply(real);
  assert.equal(r?.kind, "respond");
  if (r?.kind !== "respond") return;
  const body = JSON.parse(r.response.body) as {
    mcp_servers: { name: string }[];
  };
  assert.equal(body.mcp_servers.length, 2);
  assert.equal(body.mcp_servers[0]?.name, LONG_NAME);
  assert.equal(body.mcp_servers[1]?.name, "jira");
  assert.equal(LONG_NAME.length, 5000);
  assert.equal(
    byName["long-name"]?.apply(JSON.stringify({ mcp_servers: [] })).kind,
    "skip",
  );
});
