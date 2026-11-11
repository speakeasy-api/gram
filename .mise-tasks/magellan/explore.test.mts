import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import {
  chmodSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readdirSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { after, test } from "node:test";

// Runs explore.sh in a scratch directory with `claude`, `mise` and `git`
// stubbed: each records how it was called, and nothing touches a browser,
// a remote or a model.

const script = resolve(".mise-tasks/magellan/explore.sh");
const explorePrompt = readFileSync(
  resolve(".github/prompts/magellan-explore.md"),
  "utf8",
);
const scratch = mkdtempSync(join(tmpdir(), "magellan-explore-test-"));
after(() => rmSync(scratch, { recursive: true, force: true }));

type Opts = {
  budget?: string | undefined;
  since?: string;
  changed?: string;
  org?: string;
  claudeExit?: number;
  fetchExit?: number;
  writeFindings?: number;
};

function explore(opts: Opts = {}) {
  const root = mkdtempSync(join(scratch, "repo-"));
  mkdirSync(join(root, ".github/prompts"), { recursive: true });
  writeFileSync(
    join(root, ".github/prompts/magellan-explore.md"),
    explorePrompt,
  );
  const bin = join(root, ".bin");
  mkdirSync(bin);
  const stub = (name: string, body: string) => {
    writeFileSync(join(bin, name), `#!/usr/bin/env bash\n${body}\n`);
    chmodSync(join(bin, name), 0o755);
  };
  stub(
    "claude",
    `printf '%s\\n' "$@" > "$STUB_DIR/claude.args"
cat > "$STUB_DIR/claude.stdin"
dir=$(ls -d e2e/.magellan/*/ | head -1)
for i in $(seq 1 "\${STUB_FINDINGS:-0}"); do echo '{"kind":"race"}' >> "$dir/out/findings.jsonl"; done
echo "agent transcript"
exit "\${STUB_CLAUDE_EXIT:-0}"`,
  );
  stub("mise", `printf '%s\\n' "$*" >> "$STUB_DIR/mise.calls"`);
  stub(
    "git",
    `printf '%s\\n' "$*" >> "$STUB_DIR/git.calls"
case "$1" in
  fetch) exit "\${STUB_FETCH_EXIT:-0}" ;;
  log) printf '%b' "$STUB_CHANGED" ;;
esac`,
  );
  const env: Record<string, string> = {
    PATH: `${bin}:${process.env["PATH"]}`,
    STUB_DIR: root,
    STUB_CHANGED: opts.changed ?? "",
    STUB_CLAUDE_EXIT: String(opts.claudeExit ?? 0),
    STUB_FETCH_EXIT: String(opts.fetchExit ?? 0),
    STUB_FINDINGS: String(opts.writeFindings ?? 0),
    GRAM_SITE_URL: "https://localhost:5173",
    usage_since: opts.since ?? "24 hours ago",
  };
  if (opts.budget !== undefined) env["usage_budget"] = opts.budget;
  else if (!("budget" in opts)) env["usage_budget"] = "45";
  if (opts.org) env["MAGELLAN_ORG"] = opts.org;
  const res = spawnSync("bash", [script], { cwd: root, env, encoding: "utf8" });
  const read = (f: string) =>
    existsSync(join(root, f)) ? readFileSync(join(root, f), "utf8") : undefined;
  const runs = existsSync(join(root, "e2e/.magellan"))
    ? readdirSync(join(root, "e2e/.magellan"))
    : [];
  return {
    ...res,
    root,
    runs,
    args: read("claude.args")?.split("\n").filter(Boolean),
    prompt: read("claude.stdin"),
    mise: read("mise.calls")?.split("\n").filter(Boolean) ?? [],
    git: read("git.calls")?.split("\n").filter(Boolean) ?? [],
  };
}

function flag(args: string[] | undefined, name: string): string {
  const i = args!.indexOf(name);
  assert.notEqual(i, -1, `missing ${name}`);
  return args![i + 1]!;
}

for (const budget of ["", "abc", "10m", "-5", "1.5", "5 ; rm -rf /", "$(id)"]) {
  test(`rejects budget ${JSON.stringify(budget)}`, () => {
    const r = explore({ budget });
    assert.notEqual(r.status, 0);
    assert.equal(r.args, undefined, "the agent must not start");
    assert.deepEqual(r.runs, []);
  });
}

test("requires a budget", () => {
  const r = explore({ budget: undefined } as Opts);
  assert.notEqual(r.status, 0);
});

test("creates a timestamped run directory with an empty findings file", () => {
  const r = explore();
  assert.equal(r.status, 0, r.stderr);
  assert.equal(r.runs.length, 1);
  assert.match(r.runs[0]!, /^\d{8}T\d{6}Z$/);
  const findings = join(
    r.root,
    "e2e/.magellan",
    r.runs[0]!,
    "out/findings.jsonl",
  );
  assert.equal(readFileSync(findings, "utf8"), "");
});

test("prints the run directory and the number of findings", () => {
  const r = explore({ writeFindings: 2 });
  assert.equal(r.status, 0, r.stderr);
  const lines = r.stdout.trim().split("\n");
  assert.equal(lines[0], `e2e/.magellan/${r.runs[0]}`);
  assert.equal(lines[1], "findings: 2");
});

test("keeps the agent's transcript in the run directory", () => {
  const r = explore();
  const t = readFileSync(
    join(r.root, "e2e/.magellan", r.runs[0]!, "transcript.txt"),
    "utf8",
  );
  assert.match(t, /agent transcript/);
});

test("an agent error is reported but the run still ends cleanly", () => {
  const r = explore({ claudeExit: 1 });
  assert.equal(r.status, 0, r.stderr);
  assert.match(
    r.stderr,
    /the agent exited with an error; see e2e\/.magellan\/\d{8}T\d{6}Z\/transcript.txt/,
  );
});

test("opens a browser session named after the run, then closes it", () => {
  const r = explore();
  const session = `magellan-${r.runs[0]}`;
  assert.deepEqual(r.mise, [
    `run playwright -s=${session} open https://localhost:5173`,
    `run playwright -s=${session} close`,
  ]);
});

test("closes the browser even when the agent fails", () => {
  const r = explore({ claudeExit: 2 });
  assert.match(r.mise.at(-1)!, /close$/);
});

test("fetches main and lists changed dashboard files, without tests or the SDK", () => {
  const r = explore({ since: "3 days ago" });
  assert.equal(r.git[0], "fetch --quiet origin main");
  assert.equal(
    r.git[1],
    "log origin/main --since=3 days ago --format= --name-only -- client/dashboard/src :!client/dashboard/src/sdk :!*.test.ts :!*.test.tsx",
  );
});

test("a failed fetch warns and goes on", () => {
  const r = explore({ fetchExit: 1 });
  assert.equal(r.status, 0, r.stderr);
  assert.match(r.stderr, /could not fetch origin\/main; focus may be stale/);
  assert.ok(r.args);
});

test("the focus list is the changed files, sorted and unique", () => {
  const r = explore({
    changed:
      "\\nclient/dashboard/src/b.tsx\\nclient/dashboard/src/a.tsx\\n\\nclient/dashboard/src/b.tsx\\n",
  });
  assert.match(
    r.prompt!,
    /since 24 hours ago\):\n\nclient\/dashboard\/src\/a.tsx\nclient\/dashboard\/src\/b.tsx\n\n/,
  );
});

test("with no changed files the agent roams", () => {
  const r = explore();
  assert.match(r.prompt!, /\(none — roam instead\)/);
});

test("the prompt carries the shared explore instructions", () => {
  assert.ok(explore().prompt!.includes(explorePrompt));
});

test("the prompt names the run, budget, site, org and browser command", () => {
  const r = explore({ budget: "20", org: "my-org" });
  const id = r.runs[0]!;
  assert.match(
    r.prompt!,
    new RegExp(`Run id: ${id}\\. Time budget: 20 minutes\\.`),
  );
  assert.match(r.prompt!, /Site: https:\/\/localhost:5173\. Org: my-org\./);
  assert.match(
    r.prompt!,
    new RegExp(`\`mise run playwright -s=magellan-${id} <command>\``),
  );
  assert.match(
    r.prompt!,
    new RegExp(`appended to\\n  \`e2e/.magellan/${id}/out/findings.jsonl\``),
  );
});

test("the org defaults to the local seed org", () => {
  assert.match(explore().prompt!, /Org: speakeasy\./);
});

test("runs claude headless with a turn limit", () => {
  const args = explore().args!;
  assert.equal(args[0], "-p");
  assert.equal(flag(args, "--max-turns"), "200");
});

test("pre-approves writes to this run's output only, and the browser", () => {
  const r = explore();
  const out = `./e2e/.magellan/${r.runs[0]}/out/**`;
  assert.deepEqual(flag(r.args, "--allowedTools").split(","), [
    "Read",
    "Glob",
    "Grep",
    `Write(${out})`,
    `Edit(${out})`,
    `Bash(mise run playwright -s=magellan-${r.runs[0]}:*)`,
  ]);
});

test("never skips permission checks", () => {
  for (const a of explore().args!) {
    assert.doesNotMatch(a, /dangerously|bypassPermissions|--permission-mode/);
  }
});
