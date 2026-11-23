#!/usr/bin/env node

//MISE description="Run the collection's Reflow golden flows against the selected target, record CI runs, report and gate"
//MISE dir="{{ config_root }}"

//USAGE flag "--collection <id>" env="REFLOW_COLLECTION" required=#true help="Reflow application collection ID"
//USAGE flag "--shard-size <n>" default="15" help="Golden flows per shard when one run of all of them is refused"
//USAGE flag "--parallelism <n>" default="1" help="Flows to overlap within a shard (1-16); the dev dashboard times out page loads above 1"
//USAGE flag "--base-branch <name>" help="Pull requests: borrow passes from, and compare with, this branch's CI runs"
//USAGE flag "--branch <name>" help="Branch to record the CI runs on (default: the checked-out branch)"
//USAGE flag "--ci-context <file>" help="CI provenance JSON passed to reflow ci run"
//USAGE flag "--out-dir <dir>" default=".reflow-ci" help="Directory for CI run results and the Markdown report"
//USAGE flag "--publish" help="Publish the report as a private Reflow review page"
//USAGE flag "--post" help="Ask the Reflow GitHub App to write the report as the pull request's comment"

// One CI run covers every golden flow, so there is one report, one gate and
// one pull request comment. Reflow refuses a run in which more than 20 flows
// (with their `needs` ancestors) have to execute; flows it can carry a pass
// for do not count. A pull request usually touches few flows, so the single run
// fits. When it does not (the first run on a branch, or a change many flows
// cover), the sorted golden list is cut into fixed-size shards that each run
// their share, and the full run is repeated, now carrying those results.

import assert from "node:assert";
import fs from "node:fs/promises";
import path from "node:path";
import { $ } from "zx";

$.verbose = false;

type SearchPage = {
  flows: { slug: string; golden?: unknown }[];
  next_offset?: number | null;
};
type CiRunResult = {
  ci_run_id?: string;
  error?: unknown;
  apply?: { error?: { reason?: string; message?: string } } | null;
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
const publish = process.env["usage_publish"] === "true";
const post = process.env["usage_post"] === "true";
const baseArgs = baseBranch ? ["--base-branch", baseBranch] : [];

// search_flows is the CLI's one flow listing. Its golden filter "golden" keeps
// every golden flow (pristine or edited); pages follow next_offset.
async function goldenSlugs(): Promise<string[]> {
  const slugs: string[] = [];
  let offset: number | null = 0;
  while (offset !== null) {
    const request = JSON.stringify({
      collection_id: collection,
      golden: "golden",
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
    slugs.push(
      ...page.flows.filter((f) => f.golden != null).map((f) => f.slug),
    );
    offset = page.next_offset ?? null;
  }
  return slugs.sort();
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
): Promise<{ id?: string; exitCode: number | null; tooLarge: boolean }> {
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
    id: result.ci_run_id,
    exitCode: ran.exitCode,
    // Reflow refused to run this many flows at once.
    tooLarge:
      applyError?.reason === "plan_too_large" ||
      /more than 20/i.test(applyError?.message ?? "") ||
      /Golden selection exceeds 20 flows/.test(refusal),
  };
}

async function gate(ciRunId: string): Promise<boolean> {
  const gated =
    await $`reflow ci gate --ci-run ${ciRunId} ${baseArgs}`.nothrow();
  process.stdout.write(gated.stdout);
  return gated.exitCode === 0;
}

async function run() {
  await fs.mkdir(outDir, { recursive: true });
  const slugs = await goldenSlugs();
  if (slugs.length === 0) {
    throw new Error(
      "The collection has no golden flows, so there is nothing to gate on",
    );
  }
  let failed = false;
  const shardLines: string[] = [];

  console.log(`== all ${slugs.length} golden flows`);
  let full = await ciRun("all", []);
  if (!full.id && full.tooLarge) {
    // Usually more than 20 flows had to run. Each shard runs its share, so
    // the second full run can carry those results and run little or nothing.
    const groups = shards(slugs, shardSize);
    console.log(
      `\nNo CI run was recorded for all flows (exit ${full.exitCode}); running ${groups.length} shards first`,
    );
    for (const [index, group] of groups.entries()) {
      const name = `shard-${index + 1}`;
      console.log(`\n== ${name}: ${group.join(", ")}`);
      const shard = await ciRun(name, group);
      const passed = shard.id ? await gate(shard.id) : false;
      if (!passed) {
        failed = true;
      }
      shardLines.push(
        `- ${name} (${group.length} flows): ${shard.id ? (passed ? "passed" : `failed, CI run \`${shard.id}\``) : `no CI run recorded (exit ${shard.exitCode})`}`,
      );
    }
    console.log(`\n== all ${slugs.length} golden flows, again`);
    full = await ciRun("all", []);
  }

  let report: string;
  if (!full.id) {
    failed = true;
    report = `No CI run was recorded for all golden flows (exit ${full.exitCode}).\n`;
  } else {
    const reportArgs = [
      "--ci-run",
      full.id,
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
      report += `\nThe report for CI run \`${full.id}\` failed (exit ${rendered.exitCode}).\n`;
    }
    if (!(await gate(full.id))) {
      failed = true;
    }
  }

  if (shardLines.length) {
    report = `All flows did not fit in one run, so they ran in shards first:\n\n${shardLines.join("\n")}\n\n${report}`;
  }
  await fs.writeFile(path.join(outDir, "report.md"), report);
  if (failed) {
    process.exitCode = 1;
  }
}

run();
