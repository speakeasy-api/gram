import fs from "node:fs";
import path from "node:path";

import { summarize, type CaseRecord, type Verdict } from "./record.ts";
import { commit, runDir, runId } from "./run.ts";

// Merges the per-worker case files of one run into cases.jsonl and
// summary.json, and prints the failures. Run after the fuzz step, whether
// or not it passed: `node summarize.ts` with UI_FUZZ_RUN_ID set.

export function mergeRun(dir: string): CaseRecord[] {
  const files = fs
    .readdirSync(dir)
    .filter((f) => /^cases-w\d+\.jsonl$/.test(f));
  const records: CaseRecord[] = [];
  for (const file of files.sort()) {
    for (const line of fs
      .readFileSync(path.join(dir, file), "utf8")
      .split("\n")) {
      if (line.trim() !== "") records.push(JSON.parse(line) as CaseRecord);
    }
  }
  records.sort((a, b) =>
    `${a.screen} ${a.mutation}`.localeCompare(`${b.screen} ${b.mutation}`),
  );
  return records;
}

export function render(records: CaseRecord[]): string {
  const counts = summarize(records);
  const lines = [
    `ui-fuzz ${runId} (${commit.slice(0, 8)}): ${records.length} cases`,
    ...(Object.keys(counts) as Verdict[]).map(
      (v) => `  ${v.padEnd(10)} ${counts[v]}`,
    ),
  ];
  const failures = records.filter((r) => r.verdict === "fail");
  if (failures.length > 0) {
    lines.push("", "failures:");
    for (const f of failures)
      lines.push(`  ${f.screen} ${f.mutation}: ${f.reason}`);
  }
  return lines.join("\n");
}

if (
  process.argv[1] &&
  path.resolve(process.argv[1]) === path.resolve(import.meta.filename)
) {
  if (!fs.existsSync(runDir)) {
    console.error(`no run directory at ${runDir}`);
    process.exit(2);
  }
  const records = mergeRun(runDir);
  fs.writeFileSync(
    path.join(runDir, "cases.jsonl"),
    records.map((r) => JSON.stringify(r) + "\n").join(""),
  );
  fs.writeFileSync(
    path.join(runDir, "summary.json"),
    JSON.stringify(
      {
        run_id: runId,
        commit,
        counts: summarize(records),
        cases: records.length,
      },
      null,
      2,
    ) + "\n",
  );
  console.log(render(records));
  console.log(`-> ${runDir}`);
}
