import assert from "node:assert/strict";
import { test } from "node:test";
import { createPRLookup } from "./worktree-prs.mts";

function pr(
  number: number,
  state: "OPEN" | "MERGED" | "CLOSED",
  branch = "topic",
  isDraft = false,
) {
  return {
    number,
    state,
    headRefName: branch,
    isDraft,
    url: `https://example.com/pull/${number}`,
  };
}

function fixture() {
  const data = {
    time: 0,
    bulk: [] as ReturnType<typeof pr>[],
    history: [] as ReturnType<typeof pr>[],
    calls: [] as { branch?: string; state: string }[],
    failures: new Set<string>(),
  };
  const lookup = createPRLookup(
    async (args) => {
      const branch = args.includes("--head")
        ? args[args.indexOf("--head") + 1]
        : undefined;
      const state = args[args.indexOf("--state") + 1]!;
      const limit = Number(args[args.indexOf("--limit") + 1]);
      data.calls.push({ branch, state });
      if (
        data.failures.has(branch ?? "bulk") ||
        data.failures.has(`${branch}:${state}`)
      )
        throw new Error("offline");
      const matches = branch
        ? data.history.filter(
            (p) => p.headRefName === branch && p.state.toLowerCase() === state,
          )
        : data.bulk;
      return JSON.stringify(
        [...matches].sort((a, b) => b.number - a.number).slice(0, limit),
      );
    },
    () => data.time,
  );
  return { data, lookup };
}

for (const includeRecentClosed of [false, true]) {
  test(`finds an older open PR behind twelve closed PRs (bulk closed: ${includeRecentClosed})`, async () => {
    const { data, lookup } = fixture();
    data.history = [
      pr(1, "OPEN", "topic", true),
      ...Array.from({ length: 12 }, (_, i) => pr(i + 2, "CLOSED")),
    ];
    if (includeRecentClosed) data.bulk = [pr(13, "CLOSED")];
    const result = await lookup(["topic"]);
    assert.equal(result.get("topic")?.number, 1);
    assert.equal(result.get("topic")?.state, "draft");
    assert.deepEqual(
      data.calls.filter((c) => c.branch),
      [{ branch: "topic", state: "open" }],
    );
  });
}

test("selects the newest merged PR ahead of newer closed PRs", async () => {
  const { data, lookup } = fixture();
  data.history = [pr(1, "MERGED"), pr(2, "MERGED"), pr(3, "CLOSED")];
  data.bulk = [pr(3, "CLOSED")];
  assert.equal((await lookup(["topic"])).get("topic")?.number, 2);
  assert.deepEqual(
    data.calls.filter((c) => c.branch).map((c) => c.state),
    ["open", "merged"],
  );
});

test("caches confirmed misses for five minutes, but bulk hits appear immediately", async () => {
  const { data, lookup } = fixture();
  await lookup(["topic", "other"]);
  data.calls = [];
  data.time = 60_000;
  await lookup(["topic", "other"]);
  assert.deepEqual(data.calls, [{ branch: undefined, state: "all" }]);
  data.bulk = [pr(20, "OPEN")];
  data.time = 120_000;
  assert.equal((await lookup(["topic", "other"])).get("topic")?.number, 20);
  data.calls = [];
  data.time = 300_000;
  data.history = [pr(10, "CLOSED", "other")];
  assert.equal((await lookup(["topic", "other"])).get("other")?.number, 10);
  assert.deepEqual(
    data.calls.filter((c) => c.branch).map((c) => c.state),
    ["open", "merged", "closed"],
  );
});

test("failed lookups retain previous results and aren't cached as misses", async () => {
  const { data, lookup } = fixture();
  data.history = [pr(1, "OPEN")];
  await lookup(["topic"]);
  data.failures.add("topic");
  data.failures.add("other");
  assert.equal((await lookup(["topic", "other"])).get("topic")?.number, 1);
  data.failures.clear();
  data.history = [pr(2, "MERGED"), pr(3, "OPEN", "other")];
  const refreshed = await lookup(["topic", "other"]);
  assert.equal(refreshed.get("topic")?.number, 2);
  assert.equal(refreshed.get("other")?.number, 3);
  data.failures.add("bulk");
  assert.deepEqual(await lookup(["topic", "other"]), refreshed);
});

test("failed preferred-state lookup preserves a cached open PR over another closed PR", async () => {
  const { data, lookup } = fixture();
  data.history = [pr(1, "OPEN")];
  await lookup(["topic"]);
  data.bulk = [pr(2, "CLOSED")];
  data.failures.add("topic:open");
  assert.equal((await lookup(["topic"])).get("topic")?.number, 1);
});

test("fresh bulk state for the cached PR overrides its stale open state", async () => {
  const { data, lookup } = fixture();
  data.history = [pr(1, "OPEN")];
  await lookup(["topic"]);
  // The changed PR can appear after a newer PR of the same rank in the bulk list.
  data.bulk = [pr(2, "CLOSED"), pr(1, "CLOSED")];
  data.failures.add("topic:open");
  const result = (await lookup(["topic"])).get("topic");
  assert.equal(result?.number, 2);
  assert.equal(result?.state, "closed");
});

test("confirmed absence of open PRs isn't undone by a failed merged lookup", async () => {
  const { data, lookup } = fixture();
  data.history = [pr(1, "OPEN")];
  await lookup(["topic"]);
  data.history = [];
  data.bulk = [pr(2, "CLOSED")];
  data.failures.add("topic:merged");
  const result = (await lookup(["topic"])).get("topic");
  assert.equal(result?.number, 2);
  assert.equal(result?.state, "closed");
});

test("limits concurrent fallback requests to four and deduplicates branches", async () => {
  let active = 0;
  let peak = 0;
  let calls = 0;
  const lookup = createPRLookup(async (args) => {
    if (!args.includes("--head")) return "[]";
    calls++;
    peak = Math.max(peak, ++active);
    await new Promise<void>((resolve) => setImmediate(resolve));
    active--;
    return "[]";
  });
  const branches = Array.from({ length: 9 }, (_, i) => `topic-${i}`);
  await lookup([...branches, ...branches, ""]);
  assert.equal(peak, 4);
  assert.equal(calls, branches.length * 3);
});
