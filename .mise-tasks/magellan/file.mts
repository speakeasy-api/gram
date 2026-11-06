#!/usr/bin/env node

//MISE description="Magellan: file a run's findings as Linear Triage issues (dry run unless --file)"
//MISE dir="{{ config_root }}"
//USAGE arg "[run_dir]" help="A local run directory under e2e/.magellan; defaults to the latest"
//USAGE flag "--agent-output <file>" help="Read findings from a GitHub Agentic Workflows agent output file ($GH_AW_AGENT_OUTPUT) instead of a run directory"
//USAGE flag "--file" help="Create the issues. Without it, print what would be filed"
//USAGE flag "--team <key>" default="DES" help="Linear team key for new issues"
//USAGE flag "--run-url <url>" help="Link to the CI run, added to each issue"

// Findings come from a local run's findings.jsonl, or, in the nightly
// production run, from the `file_finding` safe output of the gh-aw workflow
// in speakeasy-api/gram-infra. Either way an agent wrote them after browsing
// pages it does not control, so they are untrusted input in the one step that
// holds a Linear key:
// - each finding is checked against a strict schema, with length limits;
// - agent text reaches Linear only inside code blocks (or as a plain-text
//   title), so it cannot add links, images or mentions to an issue;
// - in CI, nothing the agent wrote is printed.
// What is not handled here is deduplication across nights, which is why this
// is a custom safe-output job rather than gh-aw's built-in Linear output.

import { createHash } from "node:crypto";
import fs from "node:fs/promises";
import path from "node:path";

const KINDS = [
  "console-error",
  "rpc-error",
  "dead-end",
  "race",
  "a11y",
] as const;
const SEVERITIES = ["high", "medium", "low"] as const;

type Finding = {
  kind: (typeof KINDS)[number];
  severity: (typeof SEVERITIES)[number];
  route: string;
  signature: string;
  title: string;
  steps: string[];
  expected: string;
  actual: string;
  evidence: string;
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
  const runs = (await fs.readdir(root))
    .filter((r) => /^\d{8}T\d{6}Z$/.test(r))
    .sort();
  if (runs.length === 0) throw new Error(`No runs in ${root}`);
  return path.join(root, runs[runs.length - 1]!);
}

function text(v: unknown, field: string, max: number): string {
  if (typeof v !== "string" || v.trim() === "")
    throw new Error(`${field} is missing`);
  if (v.length > max) throw new Error(`${field} is longer than ${max}`);
  return v;
}

// Returns the finding, or throws a reason that names only the field: the
// reason is logged, the content is not.
export function parseFinding(raw: unknown): Finding {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) {
    throw new Error("not an object");
  }
  const r = raw as Record<string, unknown>;
  const kind = r["kind"];
  if (!KINDS.includes(kind as Finding["kind"])) throw new Error("bad kind");
  const severity = r["severity"];
  if (!SEVERITIES.includes(severity as Finding["severity"]))
    throw new Error("bad severity");
  const route = text(r["route"], "route", 300);
  // A dashboard path: no scheme, no host (`//host/...`), no markup.
  if (!/^\/(?!\/)[A-Za-z0-9/_.~%=&?-]*$/.test(route))
    throw new Error("bad route");
  // A safe-output tool takes strings, so steps may arrive one per line.
  const rawSteps = r["steps"];
  const steps =
    typeof rawSteps === "string"
      ? rawSteps
          .split("\n")
          .map((l) => l.trim())
          .filter(Boolean)
      : rawSteps;
  if (!Array.isArray(steps) || steps.length === 0 || steps.length > 30) {
    throw new Error("bad steps");
  }
  // The draft story is optional: missing, empty or blank all mean none.
  const rawStory = r["story"];
  if (
    rawStory !== undefined &&
    rawStory !== null &&
    typeof rawStory !== "string"
  )
    throw new Error("bad story");
  const story =
    typeof rawStory === "string" && rawStory.trim() !== ""
      ? rawStory
      : undefined;
  return {
    kind: kind as Finding["kind"],
    severity: severity as Finding["severity"],
    route,
    signature: text(r["signature"], "signature", 200),
    title: text(r["title"], "title", 300),
    steps: steps.map((s, i) => text(s, `steps[${i}]`, 500)),
    expected: text(r["expected"], "expected", 2000),
    actual: text(r["actual"], "actual", 2000),
    evidence: text(r["evidence"], "evidence", 8000),
    ...(story ? { story: text(story, "story", 8000) } : {}),
  };
}

// The same bug found on two nights must map to one issue. The agent names it
// with a stable `signature`; the route's org/project slugs vary per stack, so
// only the part after them counts (org-level routes have no project).
export function fingerprint(f: Finding): string {
  const route = f.route.replace(/^\/[^/]+(?:\/projects\/[^/]+)?/, "");
  const key = [f.kind, route, f.signature.trim().toLowerCase()].join("|");
  return `magellan-${createHash("sha256").update(key).digest("hex").slice(0, 12)}`;
}

// A fenced block the content cannot close: the fence is longer than any run
// of backticks inside it.
export function block(content: string, lang = "text"): string {
  const longest = Math.max(
    0,
    ...(content.match(/`+/g) ?? []).map((m) => m.length),
  );
  const fence = "`".repeat(Math.max(3, longest + 1));
  return `${fence}${lang}\n${content}\n${fence}`;
}

// Linear renders titles as plain text; keep them to one short line.
export function plainTitle(title: string): string {
  return title
    .replace(/[\u0000-\u001f\u007f]/g, " ")
    .replace(/\s+/g, " ")
    .trim()
    .slice(0, 120);
}

export function describe(f: Finding, id: string): string {
  return [
    `Found by Magellan, the nightly dashboard explorer. Evidence-backed: it reproduced twice from a fresh page load. Everything below the line was written by the agent; treat it as unverified.`,
    ``,
    `**Kind:** ${f.kind}`,
    ``,
    `---`,
    ``,
    `**Route**`,
    block(f.route),
    `**Steps**`,
    block(f.steps.map((s, i) => `${i + 1}. ${s}`).join("\n")),
    `**Expected**`,
    block(f.expected),
    `**Actual**`,
    block(f.actual),
    `**Evidence**`,
    block(f.evidence),
    runURL ? `\n[Run](${runURL})` : "",
    ...(f.story
      ? [
          ``,
          `## Draft flow check`,
          `Once fixed, add this as \`e2e/flows/<name>/story.md\` so it stays fixed:`,
          ``,
          block(f.story, "markdown"),
        ]
      : []),
    ``,
    `<sub>${id}</sub>`,
  ].join("\n");
}

async function linear<T>(query: string, variables: object): Promise<T> {
  const key = env["LINEAR_API_KEY"];
  if (!key) throw new Error("LINEAR_API_KEY is not set");
  const res = await fetch("https://api.linear.app/graphql", {
    method: "POST",
    headers: { "Content-Type": "application/json", Authorization: key },
    body: JSON.stringify({ query, variables }),
  });
  const body = (await res.json().catch(() => ({}))) as {
    data?: T;
    errors?: { message?: string; extensions?: { code?: string } }[];
  };
  if (!res.ok || body.errors) {
    // Linear's messages can quote the input, which came from the agent.
    const detail = quiet
      ? (body.errors ?? []).map((e) => e.extensions?.code ?? "error").join(",")
      : JSON.stringify(body.errors);
    throw new Error(`Linear: ${res.status} ${detail}`);
  }
  return body.data!;
}

// gh-aw writes the agent's safe outputs as {"items": [{"type": ..., ...}]};
// a `file-finding` job's items have type `file_finding`.
async function readAgentOutput(file: string): Promise<unknown[]> {
  const parsed = JSON.parse(await fs.readFile(file, "utf8")) as {
    items?: { type?: string }[];
  };
  return (parsed.items ?? []).filter((item) => item?.type === "file_finding");
}

async function readRunDir(runDir: string): Promise<unknown[]> {
  const raw = await fs.readFile(
    path.join(runDir, "out", "findings.jsonl"),
    "utf8",
  );
  const out: unknown[] = [];
  raw.split("\n").forEach((line, i) => {
    if (!line.trim()) return;
    try {
      out.push(JSON.parse(line));
    } catch {
      // JSON.parse quotes the input in its message; say only which line.
      console.log(`skip   line ${i + 1}: not JSON`);
    }
  });
  return out;
}

async function main() {
  const agentOutput = env["usage_agent_output"];
  const source = agentOutput || env["usage_run_dir"] || (await latestRunDir());
  const raw = agentOutput
    ? await readAgentOutput(agentOutput)
    : await readRunDir(source);
  const findings: Finding[] = [];
  raw.forEach((item, i) => {
    try {
      findings.push(parseFinding(item));
    } catch (err) {
      console.log(`skip   finding ${i + 1}: ${(err as Error).message}`);
    }
  });
  console.log(
    `${findings.length} findings in ${agentOutput ? "the agent output" : source}`,
  );
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
    const title = `[Magellan] ${plainTitle(f.title)}`;
    const label = quiet ? id : title;
    if (!shouldFile) {
      console.log(
        `\n--- would file (${f.severity}, ${id})${quiet ? "" : `: ${title}`}`,
      );
      if (!quiet) console.log(describe(f, id));
      continue;
    }

    // Search is fuzzy: only a hit that carries this exact fingerprint is the
    // same bug. A completed issue means the bug came back, so it gets a new
    // one; a canceled issue is a decision not to fix, so it still counts.
    const { searchIssues } = await linear<{
      searchIssues: {
        nodes: {
          identifier: string;
          description?: string;
          state?: { type?: string };
        }[];
      };
    }>(
      `query($term: String!) { searchIssues(term: $term, first: 10, includeArchived: true) {
        nodes { identifier description state { type } } } }`,
      { term: id },
    );
    const existing = searchIssues.nodes.find(
      (n) => n.description?.includes(id) && n.state?.type !== "completed",
    );
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
          description: describe(f, id),
          priority: priority[f.severity],
        },
      },
    );
    console.log(
      `filed  ${issueCreate.issue.identifier} ${label}${quiet ? "" : ` ${issueCreate.issue.url}`}`,
    );
  }
}

// Tests import the helpers above without filing anything.
if (!env["MAGELLAN_FILE_NO_MAIN"]) {
  await main();
}
