#!/usr/bin/env node

//MISE description="Run the collection's Reflow golden flows against the selected target, record CI runs, report and gate"
//MISE dir="{{ config_root }}"

//USAGE flag "--collection <id>" env="REFLOW_COLLECTION" required=#true help="Reflow application collection ID"
//USAGE flag "--shard-size <n>" default="15" help="Golden flows per CI run; Reflow refuses more than 20 including ancestors"
//USAGE flag "--parallelism <n>" default="1" help="Flows to overlap within a shard (1-16); the dev dashboard times out page loads above 1"
//USAGE flag "--base-branch <name>" help="Pull requests: borrow passes from, and compare with, this branch's CI runs"
//USAGE flag "--branch <name>" help="Branch to record the CI runs on (default: the checked-out branch)"
//USAGE flag "--ci-context <file>" help="CI provenance JSON passed to reflow ci run"
//USAGE flag "--out-dir <dir>" default=".reflow-ci" help="Directory for per-shard results and the Markdown report"
//USAGE flag "--publish" help="Publish each shard's report as a private Reflow review page"

// Reflow refuses to apply more than 20 golden flows (with their `needs`
// ancestors) in one request, and the suite is larger than that. So the golden
// list is read from Reflow, sorted, and cut into fixed-size shards. Each shard
// is its own CI run; its baseline is the previous CI run of the same selection,
// so shard membership only shifts when a golden flow is added or removed.

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

async function run() {
  await fs.mkdir(outDir, { recursive: true });
  const slugs = await goldenSlugs();
  const groups = shards(slugs, shardSize);
  console.log(`${slugs.length} golden flows in ${groups.length} shards`);

  const baseArgs = baseBranch ? ["--base-branch", baseBranch] : [];
  const report: string[] = [];
  let failed = false;

  for (const [index, group] of groups.entries()) {
    const name = `shard-${index + 1}`;
    const resultFile = path.join(outDir, `${name}.json`);
    console.log(`\n== ${name}: ${group.join(", ")}`);

    const runArgs = [
      ...group,
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
    const ran = await $`reflow ci run ${runArgs}`.nothrow();
    process.stdout.write(ran.stdout);
    process.stderr.write(ran.stderr);

    const result = JSON.parse(
      await fs.readFile(resultFile, "utf8").catch(() => "{}"),
    ) as CiRunResult;
    if (!result.ci_run_id) {
      // Nothing was recorded, so there is nothing to report or gate on.
      failed = true;
      report.push(
        `### ${name}\n\nNo CI run was recorded (exit ${ran.exitCode}).\n`,
      );
      continue;
    }

    const reportArgs = [
      "--ci-run",
      result.ci_run_id,
      ...baseArgs,
      "--format",
      "markdown",
      ...(publish ? ["--publish"] : []),
    ];
    const rendered = await $`reflow ci report ${reportArgs}`.nothrow();
    report.push(`### ${name}\n\n${rendered.stdout}`);

    const gate =
      await $`reflow ci gate --ci-run ${result.ci_run_id} ${baseArgs}`.nothrow();
    process.stdout.write(gate.stdout);
    if (gate.exitCode !== 0) {
      failed = true;
    }
  }

  await fs.writeFile(path.join(outDir, "report.md"), report.join("\n"));
  if (failed) {
    process.exitCode = 1;
  }
}

run();
