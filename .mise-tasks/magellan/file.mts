#!/usr/bin/env node

//MISE description="Magellan: file a run's findings as Linear Triage issues (dry run unless --file)"
//MISE dir="{{ config_root }}"
//USAGE arg "[run_dir]" help="A run directory under e2e/.magellan; defaults to the latest"
//USAGE flag "--file" help="Create the issues. Without it, print what would be filed"
//USAGE flag "--team <key>" default="DES" help="Linear team key for new issues"
//USAGE flag "--run-url <url>" help="Link to the CI run, added to each issue"

// Everything in findings.jsonl was written by an agent that browsed pages it
// does not control, so it is untrusted input here, in the one step that holds
// a Linear key:
// - each finding is checked against a strict schema, with length limits;
// - agent text reaches Linear only inside code blocks (or as a plain-text
//   title), so it cannot add links, images or mentions to an issue;
// - a screenshot must be a PNG directly in the run's out/ directory;
// - in CI, whose logs are public, nothing the agent wrote is printed.

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

const MAX_SCREENSHOT = 10 * 1024 * 1024;
const PNG_MAGIC = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);

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
  if (!/^\/[A-Za-z0-9/_.~%=&?-]*$/.test(route)) throw new Error("bad route");
  const steps = r["steps"];
  if (!Array.isArray(steps) || steps.length === 0 || steps.length > 30) {
    throw new Error("bad steps");
  }
  const screenshot = r["screenshot"];
  if (
    screenshot !== undefined &&
    screenshot !== "" &&
    (typeof screenshot !== "string" ||
      !/^[A-Za-z0-9_-][A-Za-z0-9._-]{0,79}\.png$/.test(screenshot))
  ) {
    throw new Error("bad screenshot");
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
    ...(screenshot ? { screenshot: screenshot as string } : {}),
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

export function describe(
  f: Finding,
  id: string,
  screenshotURL?: string,
): string {
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
    // Linear's own asset URL, returned by its upload API: not agent text.
    screenshotURL ? `\n![screenshot](${screenshotURL})` : "",
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

// Screenshots go to Linear's private storage, not to CI artifacts: the repo is
// public, and remote runs see a real org.
async function readScreenshot(
  runDir: string,
  name: string,
): Promise<Buffer | undefined> {
  const file = path.join(runDir, "out", path.basename(name));
  const stat = await fs.lstat(file).catch(() => undefined);
  if (!stat || !stat.isFile() || stat.size > MAX_SCREENSHOT) return undefined;
  const data = await fs.readFile(file);
  return data.subarray(0, 8).equals(PNG_MAGIC) ? data : undefined;
}

async function uploadScreenshot(data: Buffer): Promise<string> {
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
    { size: data.length, name: "screenshot.png" },
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

async function main() {
  const runDir = env["usage_run_dir"] || (await latestRunDir());
  const raw = await fs.readFile(
    path.join(runDir, "out", "findings.jsonl"),
    "utf8",
  );
  const findings: Finding[] = [];
  raw.split("\n").forEach((line, i) => {
    if (!line.trim()) return;
    try {
      findings.push(parseFinding(JSON.parse(line)));
    } catch (err) {
      // JSON.parse quotes the input in its message; say only which line.
      const reason =
        err instanceof SyntaxError ? "not JSON" : (err as Error).message;
      console.log(`skip   line ${i + 1}: ${reason}`);
    }
  });
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

    let screenshotURL: string | undefined;
    if (f.screenshot) {
      const data = await readScreenshot(runDir, f.screenshot);
      if (data) {
        screenshotURL = await uploadScreenshot(data).catch((err: unknown) => {
          console.warn(
            `screenshot upload failed for ${id}${quiet ? "" : `: ${err}`}`,
          );
          return undefined;
        });
      }
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
          description: describe(f, id, screenshotURL),
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
