import fs from "node:fs";
import path from "node:path";
import { type Page, type Route } from "@playwright/test";

import { expect, test } from "../ui-fuzz/fixtures.ts";
import { JevClient, JevError } from "../ui-fuzz/jev.ts";
import {
  BASELINE_EXPECTED,
  mutators,
  type Mutator,
} from "../ui-fuzz/mutations.ts";
import {
  QUESTION_BROKEN,
  QUESTION_STATE,
  buildStateDocument,
  screenQuestions,
  type ScreenDescription,
  type ScreenState,
} from "../ui-fuzz/questions.ts";
import { judge, sha256, toLine, type CaseRecord } from "../ui-fuzz/record.ts";
import {
  commit,
  ensureRunDir,
  frameRpcs,
  readDeps,
  runDir,
  runId,
  type RouteDeps,
} from "../ui-fuzz/run.ts";

// Step two: adversarial UI states, judged by Jev. For every static route the
// recorder saw, one unmutated baseline case, then one case per (rpc the
// route depends on × mutator). Each case rewrites that one response, snapshots
// the accessibility tree of the main region, and asks Jev which state the
// screen is in and whether anything looks broken. Every case is recorded to
// e2e/.ui-fuzz/<run>/, passing ones included, so runs can be diffed; see
// e2e/ui-fuzz/README.md.

const apiKey = process.env["OPENROUTER_DEV_KEY"] ?? "";
if (!apiKey || apiKey === "unset") {
  throw new Error(
    "OPENROUTER_DEV_KEY is not set. Run `mise run zero:openrouter`, or run this suite through `mise run test:ui-fuzz`.",
  );
}

const deps = readDeps();
if (!deps) {
  throw new Error(
    "No recording found. Run the recorder first: `mise run test:ui-fuzz` does both steps.",
  );
}
const frame = frameRpcs(deps);

const jev = new JevClient({ apiKey });
const records: CaseRecord[] = [];

type Case = {
  route: RouteDeps;
  rpc?: string;
  mutator?: Mutator;
  expected: ReadonlySet<ScreenState>;
};

const cases: Case[] = [];
for (const route of Object.values(deps.routes)) {
  cases.push({ route, expected: BASELINE_EXPECTED });
  for (const rpc of route.rpcs.filter((r) => !frame.has(r))) {
    for (const mutator of mutators)
      cases.push({ route, rpc, mutator, expected: mutator.expected });
  }
}

async function installMutation(
  page: Page,
  rpc: string,
  m: Mutator,
): Promise<{ skipped: () => string | undefined }> {
  let skipReason: string | undefined;
  await page.route(
    (url) => url.pathname === rpc,
    async (route: Route) => {
      if (route.request().method() !== "GET") {
        await route.continue();
        return;
      }
      let original: string | undefined;
      let upstream: Awaited<ReturnType<Route["fetch"]>> | undefined;
      if (m.needsOriginal) {
        upstream = await route.fetch();
        original = await upstream.text();
      }
      const applied = m.apply(original);
      if (applied.kind === "skip") {
        skipReason = applied.reason;
        if (upstream) await route.fulfill({ response: upstream });
        else await route.continue();
        return;
      }
      await route.fulfill({
        status: applied.response.status,
        contentType: applied.response.contentType,
        body: applied.response.body,
      });
    },
  );
  return { skipped: () => skipReason };
}

function caseName(c: Case): string {
  const dep = c.rpc
    ? ` ${c.rpc.replace("/rpc/", "")} × ${c.mutator!.name}`
    : " baseline";
  return `${c.route.key}${dep}`;
}

for (const c of cases) {
  test(caseName(c), async ({ page, session }) => {
    const base: CaseRecord = {
      run_id: runId,
      commit,
      at: new Date().toISOString(),
      screen: c.route.key,
      mutation: c.rpc ? `${c.rpc}#${c.mutator!.name}` : "baseline",
      expected: [...c.expected],
      verdict: "skipped",
      reason: "",
    };
    const target = c.route.path
      .replace(":orgSlug", session.orgSlug)
      .replace(":projectSlug", session.projectSlug);

    if (c.route.final_path !== target) {
      const reason = `route redirected to ${c.route.final_path} when recorded`;
      records.push({ ...base, reason });
      test.skip(true, reason);
      return;
    }

    const mutation =
      c.rpc && c.mutator
        ? await installMutation(page, c.rpc, c.mutator)
        : undefined;
    const hit = c.rpc
      ? page.waitForResponse((r) => new URL(r.url()).pathname === c.rpc, {
          timeout: 60_000,
        })
      : Promise.resolve();
    await page.goto(target);
    await hit;
    // Let react-query settle and any error boundary render. networkidle
    // can hang on long-polling pages, so it is bounded and best effort.
    await page
      .waitForLoadState("networkidle", { timeout: 15_000 })
      .catch(() => undefined);
    await page.waitForTimeout(1_000);

    const skipReason = mutation?.skipped();
    if (skipReason) {
      records.push({ ...base, reason: skipReason });
      test.skip(true, skipReason);
      return;
    }

    const region =
      (await page.locator("main").count()) > 0
        ? page.locator("main").first()
        : page.locator("body");
    const snapshot = await region.ariaSnapshot();
    const snapshotFile = path.join(
      ensureRunDir(),
      "snapshots",
      `${base.screen}-${base.mutation.replace(/[/#]/g, "_")}.yaml`,
    );
    fs.writeFileSync(snapshotFile, snapshot);

    const screen: ScreenDescription = {
      route:
        c.route.path.replace(/^\/:orgSlug(\/projects\/:projectSlug)?/, "") ||
        "/",
      purpose: `The "${c.route.title}" page of the dashboard.`,
    };
    const doc = buildStateDocument(screen, snapshot);
    const questions = screenQuestions();
    let record: CaseRecord;
    try {
      const { response, latencyMs } = await jev.ask(doc, questions);
      const verdict = judge(c.expected, response.answers);
      const state = response.answers[QUESTION_STATE];
      record = {
        ...base,
        ...verdict,
        state_choice: state?.choice,
        state_probabilities: state?.probabilities,
        broken: response.answers[QUESTION_BROKEN]?.noul,
        model: response.model,
        input_tokens: response.usage.input_tokens,
        output_tokens: response.usage.output_tokens,
        latency_ms: latencyMs,
        snapshot_chars: snapshot.length,
        snapshot_sha256: sha256(snapshot),
        snapshot_truncated: doc.snapshot_truncated,
      };
    } catch (err) {
      // A judge outage is not a UI regression. Record it as uncertain so the
      // summary shows the gap, and fail the case so nobody reads an empty
      // run as green.
      const reason = err instanceof JevError ? err.message : String(err);
      records.push({ ...base, verdict: "uncertain", reason: `jev: ${reason}` });
      throw err;
    }
    records.push(record);

    test.info().annotations.push({
      type: "jev",
      description: `${record.verdict}: ${record.reason}`,
    });
    expect(record.verdict, record.reason).not.toBe("fail");
  });
}

test.afterAll(() => {
  // One file per worker; `summarize.ts` merges them.
  const file = path.join(
    ensureRunDir(),
    `cases-w${test.info().workerIndex}.jsonl`,
  );
  fs.writeFileSync(file, records.map(toLine).join(""));
});
