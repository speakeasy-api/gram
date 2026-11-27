#!/usr/bin/env node

//MISE description="Run the collection's Reflow golden flows against the selected target, record CI runs, report and gate"
//MISE dir="{{ config_root }}"

//USAGE flag "--collection <id>" env="REFLOW_COLLECTION" required=#true help="Reflow application collection ID"
//USAGE flag "--shard-size <n>" default="15" help="Flows per shard when one run of all of them is refused"
//USAGE flag "--parallelism <n>" default="1" help="Flows to overlap within a shard (1-16); the dev dashboard times out page loads above 1"
//USAGE flag "--base-branch <name>" help="Pull requests: borrow passes from, compare with, and fail only on new failures against this branch's CI runs"
//USAGE flag "--branch <name>" help="Branch to record the CI runs on (default: the checked-out branch)"
//USAGE flag "--ci-context <file>" help="CI provenance JSON passed to reflow ci run; its pr_number scopes the run to that pull request's flow drafts"
//USAGE flag "--out-dir <dir>" default=".reflow-ci" help="Directory for CI run results and the Markdown report"
//USAGE flag "--fresh" help="Run every flow again (--since none) instead of carrying earlier passes, as after promoting merged pull request drafts"
//USAGE flag "--publish" help="Publish the report as a private Reflow review page"
//USAGE flag "--post" help="Ask the Reflow GitHub App to write the report as the pull request's comment"

// The roots are the collection's golden flows. On a pull request they are the
// golden flows as the pull request sees them, plus every flow the pull request
// edits as a PR draft that runs on its own (a `use` helper edited inline-only
// runs through its callers). One CI run tries to cover all roots. Reflow
// refuses a run in which more than 20 flows (with their `needs` ancestors) have
// to execute; flows it can carry a pass for do not count. When the run is
// refused, the sorted roots are cut into fixed-size shards. Every shard must
// pass its gate, the shards together must cover every root, and one report
// judges all the shards' CI runs together.
//
// On a pull request every run is locked to the edit set read at the start
// (--pr-edit-set): if someone changes a draft mid-run, the run fails instead of
// mixing draft versions. ci run takes the pull request from --ci-context.

import assert from "node:assert";
import fs from "node:fs/promises";
import path from "node:path";
import { $ } from "zx";

$.verbose = false;

type SearchPage = {
  flows: {
    slug: string;
    golden?: unknown;
    pr_draft?: { inline_only?: boolean } | null;
  }[];
  next_offset?: number | null;
  pr?: { edit_set_hash: string };
};
type CiRunResult = {
  ci_run_id?: string | null;
  ci_run?: { flows: { slug: string | null; outcome: string }[] } | null;
  error?: unknown;
  apply?: { error?: { reason?: string; message?: string } } | null;
};
type CiRun = {
  name: string;
  id?: string;
  exitCode: number | null;
  tooLarge: boolean;
  result: CiRunResult;
};

const collection = process.env["usage_collection"];
assert(collection, "--collection is required");
const shardSize = Number(process.env["usage_shard_size"] ?? "15");
assert(
  Number.isInteger(shardSize) && shardSize >= 1 && shardSize <= 19,
  "--shard-size must be 1-19 so the shared seed ancestor fits under Reflow's limit of 20",
);
const parallelism = process.env["usage_parallelism"] ?? "1";
const baseBranch = process.env["usage_base_branch"];
const branch = process.env["usage_branch"];
const ciContext = process.env["usage_ci_context"];
const outDir = path.resolve(process.env["usage_out_dir"] ?? ".reflow-ci");
const fresh = process.env["usage_fresh"] === "true";
const publish = process.env["usage_publish"] === "true";
const post = process.env["usage_post"] === "true";
const baseArgs = baseBranch ? ["--base-branch", baseBranch] : [];

async function prNumberFromContext(): Promise<number | undefined> {
  if (!ciContext) return undefined;
  const context = JSON.parse(await fs.readFile(ciContext, "utf8")) as {
    pr_number?: number;
  };
  return context.pr_number ?? undefined;
}

// search_flows is the CLI's one flow listing; pages follow next_offset. With pr
// it lists flows as the pull request sees them: golden is the flow's golden
// state on the pull request, pr_draft names an edit, and pr.edit_set_hash
// identifies the whole set of the pull request's drafts.
async function expectedRoots(
  prNumber: number | undefined,
): Promise<{ roots: string[]; editSet?: string }> {
  const roots = new Set<string>();
  let editSet: string | undefined;
  let offset: number | null = 0;
  while (offset !== null) {
    const request = JSON.stringify({
      collection_id: collection,
      // A pull request can demote a golden flow it edits; that draft is still
      // a root, so its search keeps every flow and filters below.
      golden: prNumber === undefined ? "golden" : "any",
      ...(prNumber === undefined ? {} : { pr: prNumber }),
      limit: 200,
      offset,
    });
    const out = await $`reflow mcp call search_flows ${request}`.nothrow();
    if (out.exitCode !== 0) {
      throw new Error(
        `search_flows failed (exit ${out.exitCode}): ${out.stdout || out.stderr}`,
      );
    }
    const envelope = JSON.parse(out.stdout) as {
      structuredContent?: SearchPage;
      content?: { type: string; text: string }[];
    };
    const text = envelope.content?.find((c) => c.type === "text")?.text;
    const page =
      envelope.structuredContent ??
      (text ? (JSON.parse(text) as SearchPage) : undefined);
    assert(
      page && Array.isArray(page.flows),
      "search_flows returned no flows list",
    );
    if (prNumber !== undefined) {
      const hash = page.pr?.edit_set_hash;
      assert(hash, `search_flows named no edit set for PR #${prNumber}`);
      if (editSet !== undefined && editSet !== hash) {
        throw new Error(
          `PR #${prNumber} flow drafts changed while they were listed; run again`,
        );
      }
      editSet = hash;
    }
    for (const flow of page.flows) {
      const runnableDraft =
        flow.pr_draft != null && flow.pr_draft.inline_only !== true;
      if (flow.golden != null || runnableDraft) {
        roots.add(flow.slug);
      }
    }
    offset = page.next_offset ?? null;
  }
  return { roots: [...roots].sort(), editSet };
}

function shards<T>(items: T[], size: number): T[][] {
  const out: T[][] = [];
  for (let i = 0; i < items.length; i += size) {
    out.push(items.slice(i, i + size));
  }
  return out;
}

async function ciRun(
  name: string,
  selection: string[],
  editSet: string | undefined,
): Promise<CiRun> {
  const resultFile = path.join(outDir, `${name}.json`);
  const runArgs = [
    ...selection,
    "--golden",
    "--collection",
    collection,
    "--parallelism",
    parallelism,
    "--result-file",
    resultFile,
    ...baseArgs,
    ...(fresh ? ["--since", "none"] : []),
    ...(editSet ? ["--pr-edit-set", editSet] : []),
    ...(branch ? ["--branch", branch] : []),
    ...(ciContext ? ["--ci-context", ciContext] : []),
  ];
  // A result file left by an earlier invocation must not stand in for this run.
  await fs.rm(resultFile, { force: true });
  const ran = await $`reflow ci run ${runArgs}`.nothrow();
  process.stdout.write(ran.stdout);
  process.stderr.write(ran.stderr);
  const result = JSON.parse(
    await fs.readFile(resultFile, "utf8").catch(() => "{}"),
  ) as CiRunResult;
  const applyError = result.apply?.error;
  // ci run refuses an oversized selection before apply, with a top-level error
  // and "Golden selection exceeds 20 flows" on its output, so check those too.
  const refusal = `${ran.stdout}\n${ran.stderr}\n${JSON.stringify(result.error ?? "")}`;
  return {
    name,
    id: result.ci_run_id ?? undefined,
    exitCode: ran.exitCode,
    // Reflow refused to run this many flows at once.
    tooLarge:
      applyError?.reason === "plan_too_large" ||
      /more than 20/i.test(applyError?.message ?? "") ||
      /Golden selection exceeds 20 flows/.test(refusal),
    result,
  };
}

// A pull request fails only on what it breaks. A flow that already fails on
// the base branch (still_failing) is the base branch's problem, and failing on
// it would block every open pull request until the base branch is fixed.
const failOn = baseBranch ? ["--fail-on", "new_failure,unknown,not_run"] : [];

async function gate(ciRunId: string): Promise<boolean> {
  const gated =
    await $`reflow ci gate --ci-run ${ciRunId} ${baseArgs} ${failOn}`.nothrow();
  process.stdout.write(gated.stdout);
  return gated.exitCode === 0;
}

// A CI run counts only when ci run exited 0 with no error, recorded a CI run
// and that CI run passes its gate. Anything else, including an incomplete run
// that recorded nothing, fails the job.
async function judge(run: CiRun): Promise<string | undefined> {
  if (!run.id) return `no CI run recorded (exit ${run.exitCode})`;
  if (run.exitCode !== 0) return `exit ${run.exitCode}, CI run \`${run.id}\``;
  if (run.result.error != null) {
    return `error ${JSON.stringify(run.result.error)}, CI run \`${run.id}\``;
  }
  if (!(await gate(run.id))) return `failed, CI run \`${run.id}\``;
  return undefined;
}

// A root is covered when a CI run ran it or carried its pass.
function uncovered(roots: string[], runs: CiRun[]): string[] {
  const covered = new Set(
    runs.flatMap((run) =>
      (run.result.ci_run?.flows ?? [])
        .filter((f) => f.outcome === "ran" || f.outcome === "not_affected")
        .map((f) => f.slug),
    ),
  );
  return roots.filter((slug) => !covered.has(slug));
}

async function run() {
  await fs.mkdir(outDir, { recursive: true });
  const prNumber = await prNumberFromContext();
  const { roots, editSet } = await expectedRoots(prNumber);
  if (roots.length === 0) {
    throw new Error(
      "The collection has no golden flows, so there is nothing to gate on",
    );
  }
  let failed = false;
  const lines: string[] = [];
  const scope =
    prNumber === undefined
      ? ""
      : ` on PR #${prNumber} (edit set ${editSet ?? "none"})`;

  console.log(`== all ${roots.length} flows${scope}`);
  const all = await ciRun("all", [], editSet);
  let runs: CiRun[] = [all];
  if (!all.id && all.tooLarge) {
    const groups = shards(roots, shardSize);
    console.log(
      `\nAll flows did not fit in one run (exit ${all.exitCode}); running ${groups.length} shards`,
    );
    runs = [];
    for (const [index, group] of groups.entries()) {
      const name = `shard-${index + 1}`;
      console.log(`\n== ${name}: ${group.join(", ")}`);
      runs.push(await ciRun(name, group, editSet));
    }
  }

  for (const run of runs) {
    const problem = await judge(run);
    if (problem) failed = true;
    if (runs.length > 1) {
      lines.push(`- ${run.name}: ${problem ?? "passed"}`);
    } else if (problem) {
      lines.push(`- all flows: ${problem}`);
    }
  }
  const missing = uncovered(roots, runs);
  if (missing.length) {
    failed = true;
    lines.push(`- not covered by any CI run: ${missing.join(", ")}`);
  }

  let report = "";
  const ids = runs.flatMap((run) => (run.id ? [run.id] : []));
  if (ids.length) {
    const reportArgs = [
      ...ids.flatMap((id) => ["--ci-run", id]),
      ...baseArgs,
      "--format",
      "markdown",
      ...(publish ? ["--publish"] : []),
      ...(post ? ["--post"] : []),
    ];
    const rendered = await $`reflow ci report ${reportArgs}`.nothrow();
    process.stderr.write(rendered.stderr);
    report = rendered.stdout;
    if (rendered.exitCode !== 0) {
      failed = true;
      report += `\nThe report failed (exit ${rendered.exitCode}).\n`;
    }
  }

  if (lines.length) {
    const intro =
      runs.length > 1
        ? "All flows did not fit in one run, so they ran in shards:"
        : "The CI run did not pass:";
    report = `${intro}\n\n${lines.join("\n")}\n\n${report}`;
  }
  await fs.writeFile(path.join(outDir, "report.md"), report);
  if (failed) {
    process.exitCode = 1;
  }
}

run();
