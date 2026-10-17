import { expect, test, type Page, type Route } from "@playwright/test";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

import { JevClient, JevError } from "../ui-fuzz/jev.ts";
import { mcpListMutations, type Mutation } from "../ui-fuzz/mutations.ts";
import {
  QUESTION_BROKEN,
  QUESTION_STATE,
  buildStateDocument,
  screenQuestions,
  type ScreenDescription,
} from "../ui-fuzz/questions.ts";
import {
  judge,
  sha256,
  summarize,
  toLine,
  type CaseRecord,
} from "../ui-fuzz/record.ts";

// Adversarial UI states, judged by Jev. Each case rewrites one API response
// the screen depends on, snapshots the accessibility tree of the main region,
// and asks Jev which state the screen is in and whether anything looks
// broken. The verdict compares that against the states the mutation allows.
// Every case is recorded to e2e/.ui-fuzz/<run>/cases.jsonl, including the
// ones that pass, so runs can be diffed; see e2e/ui-fuzz/README.md.
//
// Runs against whatever stack GRAM_SITE_URL points at, through
// `mise run test:ui-fuzz`, which wakes the worktree's stack first.

const repoRoot = path.resolve(import.meta.dirname, "..", "..");

const apiKey = process.env["OPENROUTER_DEV_KEY"] ?? "";
if (!apiKey || apiKey === "unset") {
  throw new Error(
    "OPENROUTER_DEV_KEY is not set. Run `mise run zero:openrouter`, or run this suite through `mise run test:ui-fuzz`.",
  );
}

const commit =
  process.env["GITHUB_SHA"] ??
  execFileSync("git", ["rev-parse", "HEAD"], {
    cwd: repoRoot,
    encoding: "utf8",
  }).trim();

const runId =
  process.env["UI_FUZZ_RUN_ID"] ??
  `${new Date().toISOString().replace(/[-:]/g, "").slice(0, 15)}-${commit.slice(0, 8)}`;
const runDir = path.join(repoRoot, "e2e", ".ui-fuzz", runId);
fs.mkdirSync(path.join(runDir, "snapshots"), { recursive: true });

const jev = new JevClient({ apiKey });
const records: CaseRecord[] = [];

const screens: { screen: ScreenDescription; mutations: Mutation[] }[] = [
  {
    screen: {
      route: "/mcp",
      purpose:
        "Lists the MCP servers in the current project and lets the user add, open, or manage one.",
    },
    mutations: mcpListMutations,
  },
];

// Dev-idp is credential-less: one click on Log in provisions a user and lands
// on a project. The project route prefix is read back from the URL rather
// than assumed, so the suite works in any seeded worktree.
async function loginAndFindProject(page: Page): Promise<string> {
  await page.goto("/");
  const login = page.getByRole("button", { name: "Log in" });
  if (await login.isVisible({ timeout: 10_000 }).catch(() => false)) {
    await login.click();
  }
  await page.waitForURL(/\/[^/]+\/projects\/[^/]+/, { timeout: 2 * 60_000 });
  const match = new URL(page.url()).pathname.match(/^\/[^/]+\/projects\/[^/]+/);
  if (!match)
    throw new Error(`landed on ${page.url()}, which is not a project route`);
  return match[0];
}

async function installMutation(
  page: Page,
  m: Mutation,
): Promise<{ skipped: () => string | undefined }> {
  let skipReason: string | undefined;
  await page.route(
    (url) => url.pathname === m.rpcPath,
    async (route: Route) => {
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

for (const { screen, mutations } of screens) {
  for (const m of mutations) {
    test(`${screen.route} × ${m.name}`, async ({ page }) => {
      const base: CaseRecord = {
        run_id: runId,
        commit,
        at: new Date().toISOString(),
        screen: screen.route,
        mutation: m.name,
        expected: [...m.expected],
        verdict: "skipped",
        reason: "",
      };

      const projectBase = await loginAndFindProject(page);
      const mutation = await installMutation(page, m);

      const hit = page.waitForResponse(
        (r) => new URL(r.url()).pathname === m.rpcPath,
        {
          timeout: 60_000,
        },
      );
      await page.goto(`${projectBase}${screen.route}`);
      await hit;
      // Let react-query settle and any error boundary render. networkidle
      // can hang on long-polling pages, so it is bounded and best effort.
      await page
        .waitForLoadState("networkidle", { timeout: 15_000 })
        .catch(() => undefined);
      await page.waitForTimeout(1_000);

      const skipReason = mutation.skipped();
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
        runDir,
        "snapshots",
        `${screen.route.replace(/\//g, "_")}-${m.name}.yaml`,
      );
      fs.writeFileSync(snapshotFile, snapshot);

      const doc = buildStateDocument(screen, snapshot);
      const questions = screenQuestions();
      let record: CaseRecord;
      try {
        const { response, latencyMs } = await jev.ask(doc, questions);
        const verdict = judge(m.expected, response.answers);
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
        // A judge outage is not a UI regression. Record it as uncertain so
        // the run's summary shows the gap, and fail the case so nobody reads
        // an empty run as green.
        const reason = err instanceof JevError ? err.message : String(err);
        record = { ...base, verdict: "uncertain", reason: `jev: ${reason}` };
        records.push(record);
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
}

test.afterAll(() => {
  fs.writeFileSync(
    path.join(runDir, "cases.jsonl"),
    records.map(toLine).join(""),
  );
  const summary = { run_id: runId, commit, counts: summarize(records) };
  fs.writeFileSync(
    path.join(runDir, "summary.json"),
    JSON.stringify(summary, null, 2) + "\n",
  );
  console.log(
    `ui-fuzz ${runId}: ${JSON.stringify(summary.counts)} -> ${path.relative(repoRoot, runDir)}`,
  );
});
