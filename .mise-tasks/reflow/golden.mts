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

type GoldenFlow = { slug: string };
type CiRunResult = { ci_run_id?: string; error?: unknown };

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

async function goldenSlugs(): Promise<string[]> {
  const request = JSON.stringify({
    action: "list",
    collection_id: collection,
    golden: true,
  });
  const out = await $`reflow mcp call manage_flows ${request}`;
  const envelope = JSON.parse(out.stdout) as {
    content?: { type: string; text: string }[];
  };
  const text = envelope.content?.find((c) => c.type === "text")?.text;
  assert(text, "manage_flows list returned no text content");
  const body = JSON.parse(text) as { golden?: GoldenFlow[] };
  assert(
    Array.isArray(body.golden),
    "manage_flows list returned no golden flows",
  );
  return body.golden.map((f) => f.slug).sort();
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
): Promise<{ id?: string; exitCode: number | null }> {
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
  return { id: result.ci_run_id, exitCode: ran.exitCode };
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

  console.log(`== all ${slugs.length} golden flows`);
  let full = await ciRun("all", []);
  if (!full.id) {
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
      if (!shard.id || !(await gate(shard.id))) {
        failed = true;
      }
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

  await fs.writeFile(path.join(outDir, "report.md"), report);
  if (failed) {
    process.exitCode = 1;
  }
}

run();
