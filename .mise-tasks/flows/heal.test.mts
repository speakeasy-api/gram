import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import {
  chmodSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { after, test } from "node:test";

// Runs heal.sh in a scratch checkout with `claude` stubbed: the stub records
// its arguments and prompt, and replies with a scripted transcript.

const script = resolve(".mise-tasks/flows/heal.sh");
const healPrompt = readFileSync(
  resolve(".github/prompts/heal-flow.md"),
  "utf8",
);
const scratch = mkdtempSync(join(tmpdir(), "flows-heal-test-"));
after(() => rmSync(scratch, { recursive: true, force: true }));

const verdict =
  '{"flow":"catalog-connect","verdict":"healed","step":"2","summary":"The button moved."}';

function checkout(flows: string[] = ["catalog-connect"]) {
  const root = mkdtempSync(join(scratch, "repo-"));
  mkdirSync(join(root, ".github/prompts"), { recursive: true });
  writeFileSync(join(root, ".github/prompts/heal-flow.md"), healPrompt);
  for (const flow of flows) {
    mkdirSync(join(root, "e2e/flows", flow), { recursive: true });
    writeFileSync(join(root, "e2e/flows", flow, "story.md"), `# ${flow}\n`);
  }
  const bin = join(root, ".bin");
  mkdirSync(bin);
  writeFileSync(
    join(bin, "claude"),
    `#!/usr/bin/env bash
printf '%s\\n' "$@" > "$STUB_DIR/claude.args"
cat > "$STUB_DIR/claude.stdin"
printf '%b' "$STUB_REPLY"
exit "\${STUB_EXIT:-0}"
`,
  );
  chmodSync(join(bin, "claude"), 0o755);
  return { root, bin };
}

function heal(
  flow: string | undefined,
  opts: { flows?: string[]; reply?: string; exit?: number; site?: string } = {},
) {
  const { root, bin } = checkout(opts.flows);
  const env: Record<string, string> = {
    PATH: `${bin}:${process.env["PATH"]}`,
    STUB_DIR: root,
    STUB_REPLY: opts.reply ?? `thinking...\\nran the replay\\n${verdict}\\n`,
    STUB_EXIT: String(opts.exit ?? 0),
  };
  if (opts.site !== "")
    env["GRAM_SITE_URL"] = opts.site ?? "https://localhost:5173";
  if (flow !== undefined) env["usage_flow"] = flow;
  const res = spawnSync("bash", [script], { cwd: root, env, encoding: "utf8" });
  const read = (f: string) =>
    existsSync(join(root, f)) ? readFileSync(join(root, f), "utf8") : undefined;
  return {
    ...res,
    root,
    args: read("claude.args")?.split("\n").filter(Boolean),
    prompt: read("claude.stdin"),
    verdictFile:
      flow === undefined ? undefined : read(`e2e/.flows/${flow}-verdict.json`),
    transcript:
      flow === undefined
        ? undefined
        : read(`e2e/.flows/${flow}-transcript.txt`),
  };
}

function flag(args: string[] | undefined, name: string): string {
  const i = args!.indexOf(name);
  assert.notEqual(i, -1, `missing ${name}`);
  return args![i + 1]!;
}

test("requires a flow", () => {
  const r = heal(undefined);
  assert.notEqual(r.status, 0);
  assert.equal(r.args, undefined);
});

for (const flow of [
  "../etc",
  "catalog-connect/../x",
  "Catalog",
  "catalog connect",
  "catalog_connect",
  "catalog-connect;rm -rf /",
  "$(id)",
  "",
]) {
  test(`rejects flow name ${JSON.stringify(flow)}`, () => {
    const r = heal(flow);
    assert.notEqual(r.status, 0);
    assert.equal(r.args, undefined, "the agent must not start");
  });
}

test("rejects a flow without a story", () => {
  const r = heal("missing-flow");
  assert.notEqual(r.status, 0);
  assert.match(r.stderr, /No story at e2e\/flows\/missing-flow\/story.md/);
  assert.equal(r.args, undefined);
});

test("a flow name with a leading dash stays a path, not an option", () => {
  const r = heal("-x", { flows: ["-x"] });
  // Matches the pattern and has a story, so it runs; the name is only ever
  // used inside paths and quoted arguments.
  assert.equal(r.status, 0, r.stderr);
});

test("requires the site URL", () => {
  const r = heal("catalog-connect", { site: "" });
  assert.notEqual(r.status, 0);
  assert.match(r.stderr, /GRAM_SITE_URL/);
});

test("writes the last line of the reply as the verdict", () => {
  const r = heal("catalog-connect");
  assert.equal(r.status, 0, r.stderr);
  assert.equal(r.verdictFile, `${verdict}\n`);
  assert.equal(r.stdout.trim(), verdict);
});

test("keeps the whole transcript", () => {
  const r = heal("catalog-connect");
  assert.match(r.transcript!, /thinking\.\.\.\nran the replay\n/);
  assert.ok(r.transcript!.trimEnd().endsWith(verdict));
});

test("fails when the agent fails", () => {
  const r = heal("catalog-connect", { exit: 3 });
  assert.notEqual(r.status, 0);
});

test("runs claude headless with a turn limit", () => {
  const r = heal("catalog-connect");
  assert.equal(r.args![0], "-p");
  assert.equal(flag(r.args, "--max-turns"), "80");
});

test("offers only the file and shell tools", () => {
  const r = heal("catalog-connect");
  assert.deepEqual(flag(r.args, "--tools").split(","), [
    "Bash",
    "Read",
    "Write",
    "Edit",
    "Glob",
    "Grep",
  ]);
});

test("pre-approves edits to this flow's script only", () => {
  const allowed = flag(heal("catalog-connect").args, "--allowedTools").split(
    ",",
  );
  const edits = allowed.filter((t) => /^(Edit|Write)\(/.test(t));
  assert.deepEqual(edits, [
    "Edit(./e2e/flows/catalog-connect/flow.spec.ts)",
    "Write(./e2e/flows/catalog-connect/flow.spec.ts)",
    "Write(./e2e/.flows/**)",
  ]);
  for (const t of allowed) {
    assert.doesNotMatch(t, /story\.md/, "the story is for people to edit");
    assert.notEqual(t, "Edit", "no unscoped Edit");
    assert.notEqual(t, "Write", "no unscoped Write");
  }
});

test("pre-approves only the commands the prompt names", () => {
  const allowed = flag(heal("catalog-connect").args, "--allowedTools").split(
    ",",
  );
  const bash = allowed.filter((t) => t.startsWith("Bash"));
  assert.deepEqual(bash, [
    "Bash(mise run playwright -s=catalog-connect:*)",
    "Bash(mise run test:flows flows/catalog-connect/)",
    "Bash(git diff origin/main...HEAD -- client/ e2e/flows/)",
    "Bash(aube run -F @gram/e2e type-check)",
  ]);
  assert.ok(!allowed.includes("Bash"), "no unscoped Bash");
});

test("only the browser rule takes arguments, and only in the flow's session", () => {
  const allowed = flag(heal("catalog-connect").args, "--allowedTools").split(
    ",",
  );
  const wildcards = allowed.filter(
    (t) => t.startsWith("Bash(") && t.includes("*"),
  );
  assert.deepEqual(wildcards, [
    "Bash(mise run playwright -s=catalog-connect:*)",
  ]);
  for (const t of allowed) {
    assert.doesNotMatch(t, /^Bash\(git [^)]*\*/, "git takes no free arguments");
  }
});

test("the rules follow the flow being healed", () => {
  const r = heal("server-from-source", {
    flows: ["server-from-source"],
    reply:
      '{"flow":"server-from-source","verdict":"broken","step":"1","summary":"x"}\\n',
  });
  assert.equal(r.status, 0, r.stderr);
  const allowed = flag(r.args, "--allowedTools");
  assert.match(allowed, /Bash\(mise run playwright -s=server-from-source:\*\)/);
  assert.match(
    allowed,
    /Bash\(mise run test:flows flows\/server-from-source\/\)/,
  );
  assert.doesNotMatch(allowed, /catalog-connect/);
  assert.match(r.verdictFile!, /"flow":"server-from-source"/);
  assert.ok(r.transcript);
});

test("never skips permission checks", () => {
  const args = heal("catalog-connect").args!;
  for (const a of args) {
    assert.doesNotMatch(a, /dangerously|bypassPermissions|--permission-mode/);
  }
});

test("the prompt carries the shared heal instructions", () => {
  const r = heal("catalog-connect");
  assert.ok(r.prompt!.includes(healPrompt));
});

test("the prompt names the flow, the site and the local commands", () => {
  const p = heal("catalog-connect", {
    site: "https://gram.local:5173",
  }).prompt!;
  assert.match(p, /^# Heal the flow check `catalog-connect`/);
  assert.match(p, /The dashboard is at https:\/\/gram.local:5173\./);
  assert.match(p, /`mise run playwright -s=catalog-connect <command>`/);
  assert.match(p, /`mise run test:flows flows\/catalog-connect\/`/);
  assert.match(p, /`aube run -F @gram\/e2e type-check`/);
  assert.match(p, /`git diff origin\/main\.\.\.HEAD -- client\/ e2e\/flows\/`/);
});

test("every command in the prompt is pre-approved", () => {
  const r = heal("catalog-connect");
  const allowed = flag(r.args, "--allowedTools").split(",");
  const prefixes = allowed
    .filter((t) => t.startsWith("Bash("))
    .map((t) => t.slice(5, -1));
  const approved = (c: string) =>
    prefixes.some((p) =>
      p.endsWith(":*") ? c.startsWith(p.slice(0, -2) + " ") : c === p,
    );
  const commands = [...r.prompt!.matchAll(/^- [^:]+: `([^`]+)`/gm)].map(
    (m) => m[1]!,
  );
  assert.ok(commands.length >= 4);
  for (const c of commands) {
    assert.ok(
      approved(c.replace(" <command>", " open")),
      `not pre-approved: ${c}`,
    );
  }
});

test("the prompt ends by asking for one JSON verdict line for this flow", () => {
  const p = heal("catalog-connect").prompt!;
  const last = p.trimEnd().split("\n").at(-1)!;
  const shape = JSON.parse(
    last.replace("<story step that failed or moved, or null>", "x"),
  );
  assert.equal(shape.flow, "catalog-connect");
  assert.equal(shape.verdict, "healed|broken|changed");
  assert.deepEqual(Object.keys(shape), ["flow", "verdict", "step", "summary"]);
});

test("heals one flow among several", () => {
  const r = heal("catalog-connect", {
    flows: ["catalog-connect", "server-from-source"],
  });
  assert.equal(r.status, 0, r.stderr);
  assert.doesNotMatch(r.prompt!, /server-from-source/);
});
