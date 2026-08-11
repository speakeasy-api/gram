#!/usr/bin/env node
// Turns `impeccable detect --json` output into GitHub Actions annotations and a
// job summary, then exits non-zero if anything was found.
//
// Usage: node .github/scripts/impeccable-summary.mjs findings.json

import { appendFileSync, readFileSync } from "node:fs";
import { relative } from "node:path";

const [, , findingsPath] = process.argv;

if (!findingsPath) {
  console.error("usage: impeccable-summary.mjs <findings.json>");
  process.exit(1);
}

const raw = readFileSync(findingsPath, "utf8").trim();
const findings = raw === "" ? [] : JSON.parse(raw);

const workspace = process.env.GITHUB_WORKSPACE ?? process.cwd();
const summaryPath = process.env.GITHUB_STEP_SUMMARY;

function writeSummary(markdown) {
  if (summaryPath) {
    appendFileSync(summaryPath, `${markdown}\n`);
  } else {
    console.log(markdown);
  }
}

if (findings.length === 0) {
  console.log("No UI anti-patterns detected.");
  writeSummary("## Impeccable\n\nNo UI anti-patterns detected.");
  process.exit(0);
}

const rows = findings.map((finding) => {
  const file = finding.file ? relative(workspace, finding.file) : "unknown";
  return { ...finding, file };
});

// Workflow commands are newline-delimited and `::`-separated, so any of those
// characters in a finding would truncate the annotation or inject a new one.
// See https://docs.github.com/actions/reference/workflow-commands-for-github-actions
// `%` goes first — it is the escape character.
function escapeData(value) {
  return String(value ?? "")
    .replaceAll("%", "%25")
    .replaceAll("\r", "%0D")
    .replaceAll("\n", "%0A");
}

function escapeProperty(value) {
  return escapeData(value).replaceAll(":", "%3A").replaceAll(",", "%2C");
}

for (const row of rows) {
  // `warning` findings are still failures — the job's job is to keep new slop
  // out. The annotation level only controls how GitHub renders it.
  const level = row.severity === "error" ? "error" : "warning";
  const message = `${row.name}: ${row.description}${row.snippet ? ` (${row.snippet})` : ""}`;
  const props = [
    `file=${escapeProperty(row.file)}`,
    `line=${row.line ?? 1}`,
    `title=${escapeProperty(`impeccable/${row.antipattern}`)}`,
  ].join(",");
  console.log(`::${level} ${props}::${escapeData(message)}`);
}

const table = rows
  .map(
    (row) =>
      `| \`${row.file}:${row.line ?? 1}\` | ${row.antipattern} | ${row.severity} | ${row.name} |`,
  )
  .join("\n");

writeSummary(
  [
    "## Impeccable",
    "",
    `Found **${rows.length}** UI anti-pattern${rows.length === 1 ? "" : "s"}.`,
    "",
    "| Location | Rule | Severity | Issue |",
    "| --- | --- | --- | --- |",
    table,
    "",
    "Rule catalog: <https://impeccable.style/slop>",
  ].join("\n"),
);

process.exit(1);
