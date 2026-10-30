#!/usr/bin/env node

//MISE description="Magellan: file a run's findings as Linear Triage issues (dry run unless --file)"
//MISE dir="{{ config_root }}"
//USAGE arg "[run_dir]" help="A run directory under e2e/.magellan; defaults to the latest"
//USAGE flag "--file" help="Create the issues. Without it, print what would be filed"
//USAGE flag "--team <key>" default="DES" help="Linear team key for new issues"
//USAGE flag "--run-url <url>" help="Link to the CI run, added to each issue"

import { createHash } from "node:crypto";
import fs from "node:fs/promises";
import path from "node:path";

type Finding = {
  kind: string;
  severity: "high" | "medium" | "low";
  route: string;
  signature: string;
  title: string;
  steps: string[];
  expected: string;
  actual: string;
  evidence: string;
  screenshot?: string;
  story?: string;
};

const env = process.env;
const shouldFile = env["usage_file"] === "true";
const teamKey = env["usage_team"] ?? "DES";
const runURL = env["usage_run_url"];
// CI logs are public; findings can describe unfixed bugs in production. In CI,
// log only identifiers and keep the detail in Linear.
const quiet = Boolean(env["CI"]);

// Linear priority: 2 high, 3 medium, 4 low.
const priority = { high: 2, medium: 3, low: 4 } as const;

async function latestRunDir(): Promise<string> {
  const root = "e2e/.magellan";
  const runs = (await fs.readdir(root)).sort();
  if (runs.length === 0) throw new Error(`No runs in ${root}`);
  return path.join(root, runs[runs.length - 1]!);
}

// The same bug found on two nights must map to one issue. The agent names it
// with a stable `signature`; the route's org/project slugs vary per stack, so
// only the part after the project counts.
function fingerprint(f: Finding): string {
  const route = f.route.replace(/^\/[^/]+\/projects\/[^/]+/, "");
  const key = [f.kind, route, f.signature.trim().toLowerCase()].join("|");
  return `magellan-${createHash("sha256").update(key).digest("hex").slice(0, 12)}`;
}

function describe(f: Finding, id: string, screenshotURL?: string): string {
  return [
    `Found by Magellan, the nightly dashboard explorer. Evidence-backed: it reproduced twice from a fresh page load.`,
    ``,
    `**Route:** \`${f.route}\``,
    `**Kind:** ${f.kind}`,
    ``,
    `## Steps`,
    ...f.steps.map((s, i) => `${i + 1}. ${s}`),
    ``,
    `**Expected:** ${f.expected}`,
    ``,
    `**Actual:** ${f.actual}`,
    ``,
    `## Evidence`,
    "```",
    f.evidence,
    "```",
    screenshotURL ? `\n![${f.screenshot}](${screenshotURL})` : "",
    runURL ? `\n[Run](${runURL})` : "",
    ...(f.story
      ? [
          ``,
          `## Draft flow check`,
          `Once fixed, add this as \`e2e/flows/<name>/story.md\` so it stays fixed:`,
          ``,
          "```markdown",
          f.story,
          "```",
        ]
      : []),
    ``,
    `<sub>${id}</sub>`,
  ].join("\n");
}

// Screenshots go to Linear's private storage, not to CI artifacts: the repo is
// public, and remote runs see a real org.
async function uploadScreenshot(file: string): Promise<string | undefined> {
  const data = await fs.readFile(file).catch(() => undefined);
  if (!data) return undefined;
  const { fileUpload } = await linear<{
    fileUpload: {
      uploadFile: {
        uploadUrl: string;
        assetUrl: string;
        headers: { key: string; value: string }[];
      };
    };
  }>(
    `mutation($size: Int!, $name: String!) { fileUpload(contentType: "image/png", filename: $name, size: $size) {
      uploadFile { uploadUrl assetUrl headers { key value } } } }`,
    { size: data.length, name: path.basename(file) },
  );
  const { uploadUrl, assetUrl, headers } = fileUpload.uploadFile;
  const res = await fetch(uploadUrl, {
    method: "PUT",
    headers: {
      "Content-Type": "image/png",
      "Cache-Control": "public, max-age=31536000",
      ...Object.fromEntries(headers.map((h) => [h.key, h.value])),
    },
    body: data,
  });
  if (!res.ok) throw new Error(`Linear upload: ${res.status}`);
  return assetUrl;
}

async function linear<T>(query: string, variables: object): Promise<T> {
  const key = env["LINEAR_API_KEY"];
  if (!key) throw new Error("LINEAR_API_KEY is not set");
  const res = await fetch("https://api.linear.app/graphql", {
    method: "POST",
    headers: { "Content-Type": "application/json", Authorization: key },
    body: JSON.stringify({ query, variables }),
  });
  const body = (await res.json()) as { data?: T; errors?: unknown };
  if (!res.ok || body.errors) {
    throw new Error(`Linear: ${res.status} ${JSON.stringify(body.errors)}`);
  }
  return body.data!;
}

async function main() {
  const runDir = env["usage_run_dir"] || (await latestRunDir());
  const lines = (await fs.readFile(path.join(runDir, "findings.jsonl"), "utf8"))
    .split("\n")
    .filter((l) => l.trim());
  const findings = lines.map((l) => JSON.parse(l) as Finding);
  console.log(`${findings.length} findings in ${runDir}`);
  if (findings.length === 0) return;

  let teamId = "";
  let triageStateId = "";
  if (shouldFile) {
    const { teams } = await linear<{
      teams: { nodes: { id: string; states: { nodes: { id: string }[] } }[] };
    }>(
      `query($key: String!) { teams(filter: { key: { eq: $key } }) { nodes { id
        states(filter: { type: { eq: "triage" } }) { nodes { id } } } } }`,
      { key: teamKey },
    );
    const team = teams.nodes[0];
    if (!team) throw new Error(`No Linear team with key ${teamKey}`);
    teamId = team.id;
    triageStateId = team.states.nodes[0]?.id ?? "";
  }

  for (const f of findings) {
    const id = fingerprint(f);
    const title = `[Magellan] ${f.title}`;
    const label = quiet ? id : title;
    if (!shouldFile) {
      console.log(
        `\n--- would file (${f.severity}, ${id})${quiet ? "" : `: ${title}`}`,
      );
      if (!quiet) console.log(describe(f, id));
      continue;
    }

    const { searchIssues } = await linear<{
      searchIssues: { nodes: { identifier: string; url: string }[] };
    }>(
      `query($term: String!) { searchIssues(term: $term, first: 1, includeArchived: true) { nodes { identifier url } } }`,
      { term: id },
    );
    const existing = searchIssues.nodes[0];
    if (existing) {
      console.log(`seen   ${existing.identifier} ${label}`);
      continue;
    }

    const { issueCreate } = await linear<{
      issueCreate: { issue: { identifier: string; url: string } };
    }>(
      `mutation($input: IssueCreateInput!) { issueCreate(input: $input) { issue { identifier url } } }`,
      {
        input: {
          teamId,
          ...(triageStateId ? { stateId: triageStateId } : {}),
          title,
          description: describe(
            f,
            id,
            f.screenshot
              ? await uploadScreenshot(path.join(runDir, f.screenshot))
              : undefined,
          ),
          priority: priority[f.severity],
        },
      },
    );
    console.log(`filed  ${issueCreate.issue.identifier} ${label}`);
  }
}

await main();
