import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import {
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { after, describe as suite, test } from "node:test";

// The helpers are imported without running the filer.
process.env["MAGELLAN_FILE_NO_MAIN"] = "1";
delete process.env["usage_run_url"];
const { block, describe, fingerprint, parseFinding, plainTitle, redact } =
  await import("./file.mts");

const script = resolve(".mise-tasks/magellan/file.mts");
const mockLinear = resolve(".mise-tasks/magellan/testdata/mock-linear.mjs");
const scratch = mkdtempSync(join(tmpdir(), "magellan-file-test-"));
after(() => rmSync(scratch, { recursive: true, force: true }));

function valid(overrides: Record<string, unknown> = {}) {
  return {
    kind: "rpc-error",
    severity: "high",
    route: "/acme/projects/default/toolsets",
    signature: "toolsets.list 500 on load",
    title: "Toolsets page fails to load",
    steps: ["Open the project", "Click Toolsets"],
    expected: "The toolsets list loads",
    actual: "An error page",
    evidence: 'POST /rpc/toolsets.list 500 {"name":"unexpected"}',
    ...overrides,
  };
}

suite("parseFinding", () => {
  test("accepts a complete finding", () => {
    const f = parseFinding(valid());
    assert.equal(f.kind, "rpc-error");
    assert.equal(f.severity, "high");
    assert.deepEqual(f.steps, ["Open the project", "Click Toolsets"]);
    assert.equal(f.story, undefined);
  });

  for (const kind of [
    "console-error",
    "rpc-error",
    "dead-end",
    "race",
    "a11y",
  ]) {
    test(`accepts kind ${kind}`, () => {
      assert.equal(parseFinding(valid({ kind })).kind, kind);
    });
  }

  for (const severity of ["high", "medium", "low"]) {
    test(`accepts severity ${severity}`, () => {
      assert.equal(parseFinding(valid({ severity })).severity, severity);
    });
  }

  for (const raw of [null, undefined, 1, "finding", true, [], [valid()]]) {
    test(`rejects a non-object: ${JSON.stringify(raw) ?? "undefined"}`, () => {
      assert.throws(() => parseFinding(raw), /not an object/);
    });
  }

  for (const kind of [
    "",
    "bug",
    "RPC-ERROR",
    "rpc_error",
    3,
    null,
    undefined,
  ]) {
    test(`rejects kind ${JSON.stringify(kind) ?? "undefined"}`, () => {
      assert.throws(() => parseFinding(valid({ kind })), /bad kind/);
    });
  }

  for (const severity of ["critical", "High", "", 1, null]) {
    test(`rejects severity ${JSON.stringify(severity)}`, () => {
      assert.throws(() => parseFinding(valid({ severity })), /bad severity/);
    });
  }

  for (const field of [
    "route",
    "signature",
    "title",
    "expected",
    "actual",
    "evidence",
  ]) {
    test(`rejects a missing ${field}`, () => {
      const raw = valid();
      delete (raw as Record<string, unknown>)[field];
      assert.throws(() => parseFinding(raw), new RegExp(`${field} is missing`));
    });
    test(`rejects a blank ${field}`, () => {
      assert.throws(
        () => parseFinding(valid({ [field]: "  \n\t" })),
        new RegExp(`${field} is missing`),
      );
    });
    test(`rejects a non-string ${field}`, () => {
      assert.throws(
        () => parseFinding(valid({ [field]: { toString: () => "/x" } })),
        new RegExp(`${field} is missing`),
      );
    });
  }

  for (const [field, max] of [
    ["route", 300],
    ["signature", 200],
    ["title", 300],
    ["expected", 2000],
    ["actual", 2000],
    ["evidence", 8000],
  ] as const) {
    test(`accepts ${field} at exactly ${max} characters`, () => {
      const value =
        field === "route" ? "/" + "a".repeat(max - 1) : "a".repeat(max);
      assert.equal(parseFinding(valid({ [field]: value }))[field], value);
    });
    test(`rejects ${field} over ${max} characters`, () => {
      const value =
        field === "route" ? "/" + "a".repeat(max) : "a".repeat(max + 1);
      assert.throws(
        () => parseFinding(valid({ [field]: value })),
        new RegExp(`${field} is longer than ${max}`),
      );
    });
  }

  for (const route of [
    "/",
    "/acme",
    "/acme/projects/default",
    "/acme/projects/default/mcp/my_server.v2",
    "/acme/projects/default/logs?from=2026-01-01&level=error",
    "/acme/projects/default/search?q=a%20b",
    "/~user/x-y",
  ]) {
    test(`accepts route ${route}`, () => {
      assert.equal(parseFinding(valid({ route })).route, route);
    });
  }

  for (const route of [
    "acme/projects/default",
    "https://evil.example/acme",
    "//evil.example/x",
    "///evil.example",
    "/acme/<script>",
    "/acme/projects/default#frag",
    "/acme projects",
    "/acme/\nprojects",
    "/acme/`whoami`",
    "/acme/[link](https://evil.example)",
    '/acme/"quote',
    "/acme/'quote",
    "/acme/@mention",
  ]) {
    test(`rejects route ${JSON.stringify(route)}`, () => {
      assert.throws(() => parseFinding(valid({ route })), /bad route/);
    });
  }

  test("splits newline-separated steps and drops blank lines", () => {
    const f = parseFinding(
      valid({ steps: "  Open the project \n\n Click X\n" }),
    );
    assert.deepEqual(f.steps, ["Open the project", "Click X"]);
  });

  test("splits CRLF steps without keeping the carriage return visible", () => {
    const f = parseFinding(valid({ steps: "One\r\nTwo" }));
    assert.deepEqual(f.steps, ["One", "Two"]);
  });

  for (const steps of [[], "", "\n\n  \n", null, undefined, 5, { 0: "a" }]) {
    test(`rejects steps ${JSON.stringify(steps) ?? "undefined"}`, () => {
      assert.throws(() => parseFinding(valid({ steps })), /bad steps/);
    });
  }

  test("accepts 30 steps and rejects 31", () => {
    const thirty = Array.from({ length: 30 }, (_, i) => `step ${i}`);
    assert.equal(parseFinding(valid({ steps: thirty })).steps.length, 30);
    assert.throws(
      () => parseFinding(valid({ steps: [...thirty, "one more"] })),
      /bad steps/,
    );
  });

  test("rejects 31 newline-separated steps", () => {
    const lines = Array.from({ length: 31 }, (_, i) => `step ${i}`).join("\n");
    assert.throws(() => parseFinding(valid({ steps: lines })), /bad steps/);
  });

  test("rejects a non-string step and names its index", () => {
    assert.throws(
      () => parseFinding(valid({ steps: ["ok", 2] })),
      /steps\[1\] is missing/,
    );
  });

  test("rejects a blank step in an array", () => {
    assert.throws(
      () => parseFinding(valid({ steps: ["ok", "   "] })),
      /steps\[1\] is missing/,
    );
  });

  test("rejects a step over 500 characters", () => {
    assert.throws(
      () => parseFinding(valid({ steps: ["a".repeat(501)] })),
      /steps\[0\] is longer than 500/,
    );
  });

  test("keeps a draft story", () => {
    const story = "# Flow\n\n## When\n\n1. Do it";
    assert.equal(parseFinding(valid({ story })).story, story);
  });

  for (const story of [undefined, null, "", "   \n  "]) {
    test(`treats story ${JSON.stringify(story) ?? "undefined"} as none`, () => {
      const f = parseFinding(valid({ story }));
      assert.equal(f.story, undefined);
      assert.equal("story" in f, false);
    });
  }

  for (const story of [1, true, ["# a"], { body: "x" }]) {
    test(`rejects story ${JSON.stringify(story)}`, () => {
      assert.throws(() => parseFinding(valid({ story })), /bad story/);
    });
  }

  test("rejects a story over 8000 characters", () => {
    assert.throws(
      () => parseFinding(valid({ story: "a".repeat(8001) })),
      /story is longer than 8000/,
    );
  });

  test("drops fields outside the schema", () => {
    const f = parseFinding(
      valid({ type: "file_finding", assignee: "someone", labels: ["x"] }),
    ) as Record<string, unknown>;
    assert.deepEqual(Object.keys(f).sort(), [
      "actual",
      "evidence",
      "expected",
      "kind",
      "route",
      "severity",
      "signature",
      "steps",
      "title",
    ]);
  });

  test("an error names the field, never the content", () => {
    const secret = "sk-live-should-not-appear";
    for (const raw of [
      valid({ kind: secret }),
      valid({ severity: secret }),
      valid({ route: `/${secret} x` }),
      valid({ title: secret.repeat(100) }),
      valid({ steps: [secret.repeat(100)] }),
    ]) {
      assert.throws(
        () => parseFinding(raw),
        (err: Error) => !err.message.includes(secret),
      );
    }
  });
});

suite("redact", () => {
  for (const [name, input, secret] of [
    [
      "a JWT",
      "token eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.c2lnbmF0dXJlX2hlcmU failed",
      "eyJhbGciOiJIUzI1NiJ9",
    ],
    [
      "a bearer header",
      "Authorization: Bearer abcdef0123456789abcdef",
      "abcdef0123456789abcdef",
    ],
    ["a lowercase bearer", "bearer abcdef0123456789", "abcdef0123456789"],
    ["a basic header", "Basic dXNlcjpwYXNzd29yZA==", "dXNlcjpwYXNzd29yZA=="],
    [
      "the session cookie",
      "cookie: gram_session=s3cr3t-session-value; Path=/",
      "s3cr3t-session-value",
    ],
    [
      "a set-cookie header",
      "set-cookie: abc123def456; HttpOnly",
      "abc123def456",
    ],
    ["a JSON api key", '{"api_key": "k-1234567890"}', "k-1234567890"],
    [
      "a JSON access token",
      '{"access_token":"at.1234567890"}',
      "at.1234567890",
    ],
    ["a refresh token", "refresh-token=rt_abcdefgh", "rt_abcdefgh"],
    ["a client secret", "client_secret: cs_abcdefgh", "cs_abcdefgh"],
    ["a password", 'password="hunter2hunter2"', "hunter2hunter2"],
    ["a Gram-Key header", "Gram-Key: gk_live_0123456789", "gk_live_0123456789"],
    ["an x-api-key header", "x-api-key=ak0123456789", "ak0123456789"],
    ["a query token", "/x?token=abcdef123456", "abcdef123456"],
    [
      "an OpenAI-style key",
      "used sk-proj-abcdefghijklmnop1234",
      "abcdefghijklmnop1234",
    ],
    [
      "an Anthropic key",
      "sk-ant-api03-abcdefghijklmnopqrstu",
      "abcdefghijklmnopqrstu",
    ],
    ["a Stripe key", "sk_live_abcdefghijklmnop1234", "abcdefghijklmnop1234"],
    [
      "a GitHub token",
      "ghp_abcdefghijklmnopqrstuvwxyz0123",
      "ghp_abcdefghijklmnopqrstuvwxyz0123",
    ],
    [
      "a fine-grained GitHub token",
      "github_pat_11ABCDEFG0123456789_abcdefghij",
      "github_pat_11ABCDEFG0123456789",
    ],
    [
      "a Linear key",
      "lin_api_abcdefghijklmnopqrstuvwxyz",
      "lin_api_abcdefghijklmnopqrstuvwxyz",
    ],
    [
      "a Slack token",
      "xoxb-1234567890-abcdefghij",
      "xoxb-1234567890-abcdefghij",
    ],
    ["an AWS key id", "AKIAABCDEFGHIJKLMNOP", "AKIAABCDEFGHIJKLMNOP"],
    [
      "a Speakeasy key",
      "gram_live_abcdef0123456789abcd",
      "gram_live_abcdef0123456789abcd",
    ],
    [
      "an email",
      "invited jane.doe+test@example.co.uk to the org",
      "jane.doe+test@example.co.uk",
    ],
  ] as const) {
    test(`redacts ${name}`, () => {
      const out = redact(input);
      assert.ok(!out.includes(secret), out);
      assert.match(out, /<redacted/);
    });
  }

  for (const input of [
    "POST /rpc/toolsets.list 500",
    "TypeError: Cannot read properties of undefined (reading 'map')",
    '{"name":"unexpected","message":"toolset not found","status":404}',
    'Warning: Each child in a list should have a unique "key" prop.',
    "Open the project, then click Toolsets",
  ]) {
    test(`keeps ordinary evidence: ${input.slice(0, 40)}`, () => {
      assert.equal(redact(input), input);
    });
  }

  test("redacts every occurrence", () => {
    const out = redact("a@example.com and b@example.com");
    assert.equal(out, "<redacted-email> and <redacted-email>");
  });

  test("keeps the header name so the evidence still reads", () => {
    assert.equal(
      redact("Authorization: Bearer abcdef0123456789"),
      "Authorization: Bearer <redacted>",
    );
  });

  test("is applied to every field that reaches Linear", () => {
    const secret = "jane@example.com";
    const f = parseFinding(
      valid({
        title: `Invite for ${secret} fails`,
        steps: [`Invite ${secret}`],
        expected: secret,
        actual: secret,
        evidence: `{"email":"${secret}"}`,
        story: `Invite ${secret}`,
      }),
    );
    const body = describe(f, fingerprint(f));
    assert.ok(!body.includes(secret));
    assert.ok(!f.title.includes(secret));
  });

  test("redacts a token in the route's query string", () => {
    const f = parseFinding(
      valid({ route: "/acme/projects/p/x?access_token=abcdef123456" }),
    );
    assert.ok(!f.route.includes("abcdef123456"));
  });

  test("does not change the fingerprint of a route without secrets", () => {
    const f = parseFinding(valid());
    assert.equal(f.route, valid().route);
  });
});

suite("fingerprint", () => {
  const f = parseFinding(valid());

  test("has the magellan- prefix and 12 hex characters", () => {
    assert.match(fingerprint(f), /^magellan-[0-9a-f]{12}$/);
  });

  test("is stable", () => {
    assert.equal(fingerprint(f), fingerprint(parseFinding(valid())));
  });

  test("ignores the org and project slugs", () => {
    const other = parseFinding(
      valid({ route: "/integration-org/projects/sandbox/toolsets" }),
    );
    assert.equal(fingerprint(other), fingerprint(f));
  });

  test("ignores the org slug on org-level routes", () => {
    const a = parseFinding(valid({ route: "/acme/settings/billing" }));
    const b = parseFinding(valid({ route: "/other/settings/billing" }));
    assert.equal(fingerprint(a), fingerprint(b));
  });

  test("tells an org-level route from the project home", () => {
    const org = parseFinding(valid({ route: "/acme/settings" }));
    const project = parseFinding(valid({ route: "/acme/projects/p/settings" }));
    // Both reduce to /settings: the same page name means the same bug.
    assert.equal(fingerprint(org), fingerprint(project));
    const home = parseFinding(valid({ route: "/acme/projects/p" }));
    assert.notEqual(fingerprint(home), fingerprint(org));
  });

  test("ignores signature case and surrounding space", () => {
    const other = parseFinding(
      valid({ signature: "  TOOLSETS.LIST 500 ON LOAD \n" }),
    );
    assert.equal(fingerprint(other), fingerprint(f));
  });

  for (const [field, value] of [
    ["kind", "console-error"],
    ["route", "/acme/projects/default/environments"],
    ["signature", "toolsets.list 502 on load"],
  ] as const) {
    test(`changes with ${field}`, () => {
      assert.notEqual(
        fingerprint(parseFinding(valid({ [field]: value }))),
        fingerprint(f),
      );
    });
  }

  for (const [field, value] of [
    ["severity", "low"],
    ["title", "Something else entirely"],
    ["steps", ["Different"]],
    ["expected", "x"],
    ["actual", "y"],
    ["evidence", "z"],
    ["story", "# story"],
  ] as const) {
    test(`does not change with ${field}`, () => {
      assert.equal(
        fingerprint(parseFinding(valid({ [field]: value }))),
        fingerprint(f),
      );
    });
  }

  test("a route cannot carry the key separator", () => {
    assert.throws(
      () => parseFinding(valid({ route: "/o/projects/p/a|b" })),
      /bad route/,
    );
  });
});

suite("block", () => {
  test("fences with three backticks by default", () => {
    assert.equal(block("hello"), "```text\nhello\n```");
  });

  test("takes a language", () => {
    assert.equal(block("# x", "markdown"), "```markdown\n# x\n```");
  });

  test("handles empty content", () => {
    assert.equal(block(""), "```text\n\n```");
  });

  for (const run of [1, 2, 3, 4, 7, 12]) {
    test(`content with a run of ${run} backticks cannot close the fence`, () => {
      const content = `before\n${"`".repeat(run)}\n[link](https://evil.example)`;
      const out = block(content);
      const fence = out.slice(0, out.indexOf("text"));
      assert.equal(fence.length, Math.max(3, run + 1));
      assert.ok(out.endsWith(`\n${fence}`));
      // No line inside is the fence or longer.
      const inner = out.split("\n").slice(1, -1);
      for (const line of inner) {
        assert.ok(
          !line.startsWith(fence),
          `inner line closes the fence: ${line}`,
        );
      }
    });
  }

  test("uses the longest run when there are several", () => {
    const out = block("` `` ````` ``");
    assert.ok(out.startsWith("``````text\n"));
  });

  test("keeps the content verbatim", () => {
    const content = "<img src=x onerror=alert(1)> @here **bold** \u0000";
    assert.ok(block(content).includes(`\n${content}\n`));
  });
});

suite("plainTitle", () => {
  test("keeps a plain title", () => {
    assert.equal(plainTitle("Toolsets page fails"), "Toolsets page fails");
  });

  test("flattens newlines and tabs", () => {
    assert.equal(plainTitle("a\nb\r\nc\td"), "a b c d");
  });

  test("replaces control characters", () => {
    assert.equal(plainTitle("a\u0000b\u001bc\u007fd"), "a b c d");
  });

  test("collapses runs of spaces and trims", () => {
    assert.equal(plainTitle("   a    b   "), "a b");
  });

  test("cuts to 120 characters", () => {
    assert.equal(plainTitle("x".repeat(300)).length, 120);
  });

  test("cuts after flattening, not before", () => {
    assert.equal(
      plainTitle(`${" ".repeat(200)}${"y".repeat(10)}`),
      "y".repeat(10),
    );
  });

  test("a title cannot add a second line to the issue", () => {
    assert.ok(!plainTitle("Bug\n\n## Injected heading").includes("\n"));
  });
});

suite("describe", () => {
  const f = parseFinding(
    valid({
      story: "# Story\n\n```\nnested\n```",
      evidence: "```\n[click](https://evil.example)\n```",
    }),
  );
  const id = fingerprint(f);
  const body = describe(f, id);

  test("ends with the fingerprint", () => {
    assert.ok(body.trimEnd().endsWith(`<sub>${id}</sub>`));
  });

  test("labels the agent's text as unverified", () => {
    assert.match(body, /treat it as unverified/);
  });

  test("numbers the steps", () => {
    assert.match(body, /1\. Open the project\n2\. Click Toolsets/);
  });

  test("puts every agent field inside a code block", () => {
    for (const value of [
      f.route,
      ...f.steps,
      f.expected,
      f.actual,
      f.evidence,
      f.story!,
    ]) {
      const at = body.indexOf(value);
      assert.notEqual(at, -1);
      const before = body.slice(0, at);
      const fences = before.match(/^`{3,}[a-z]*$/gm) ?? [];
      assert.equal(fences.length % 2, 1, `not fenced: ${value}`);
    }
  });

  test("agent markdown cannot escape its block", () => {
    // Every fence the body opens is closed by a fence of the same length.
    const lines = body.split("\n");
    let open: string | null = null;
    for (const line of lines) {
      const m = /^(`{3,})([a-z]*)$/.exec(line);
      if (!m) continue;
      if (open === null) open = m[1]!;
      else if (m[1] === open && m[2] === "") open = null;
    }
    assert.equal(open, null);
  });

  test("includes the draft story section only with a story", () => {
    assert.match(body, /## Draft flow check/);
    const noStory = describe(parseFinding(valid()), id);
    assert.doesNotMatch(noStory, /Draft flow check/);
  });

  test("does not include the severity or title text in the body", () => {
    // The title is the issue title; the severity is the priority.
    assert.ok(!body.includes(f.title));
  });

  test("has no run link without --run-url", () => {
    assert.doesNotMatch(body, /\[Run\]/);
  });
});

// --- The script end to end ---------------------------------------------

type Run = { status: number | null; stdout: string; stderr: string };

function run(
  env: Record<string, string>,
  opts: { linear?: unknown[]; cwd?: string } = {},
): Run & { requests: { query: string; variables: Record<string, unknown> }[] } {
  const dir = mkdtempSync(join(scratch, "run-"));
  const requestsFile = join(dir, "requests.jsonl");
  const responsesFile = join(dir, "responses.json");
  writeFileSync(responsesFile, JSON.stringify(opts.linear ?? []));
  writeFileSync(requestsFile, "");
  const res = spawnSync(process.execPath, ["--import", mockLinear, script], {
    cwd: opts.cwd ?? dir,
    encoding: "utf8",
    env: {
      PATH: process.env["PATH"] ?? "",
      MOCK_LINEAR_RESPONSES: responsesFile,
      MOCK_LINEAR_REQUESTS: requestsFile,
      ...env,
    },
  });
  const requests = readFileSync(requestsFile, "utf8")
    .split("\n")
    .filter(Boolean)
    .map((l) => JSON.parse(l));
  return {
    status: res.status,
    stdout: res.stdout,
    stderr: res.stderr,
    requests,
  };
}

function agentOutput(items: unknown[]): string {
  const dir = mkdtempSync(join(scratch, "agent-"));
  const file = join(dir, "agent_output.json");
  writeFileSync(file, JSON.stringify({ items }));
  return file;
}

function runDir(lines: string[]): { cwd: string; dir: string } {
  const cwd = mkdtempSync(join(scratch, "cwd-"));
  const dir = join(cwd, "e2e/.magellan/20261106T030000Z");
  mkdirSync(join(dir, "out"), { recursive: true });
  writeFileSync(join(dir, "out/findings.jsonl"), lines.join("\n"));
  return { cwd, dir };
}

const teams = {
  data: {
    teams: {
      nodes: [{ id: "team-1", states: { nodes: [{ id: "triage-1" }] } }],
    },
  },
};
const noHits = { data: { searchIssues: { nodes: [] } } };
const created = (n: number) => ({
  data: {
    issueCreate: {
      issue: { identifier: `DES-${n}`, url: `https://linear.app/x/DES-${n}` },
    },
  },
});

suite("file.mts dry run", () => {
  test("prints the issue it would file from a run directory", () => {
    const { cwd } = runDir([JSON.stringify(valid())]);
    const r = run({}, { cwd });
    assert.equal(r.status, 0, r.stderr);
    assert.match(r.stdout, /1 findings in e2e\/.magellan\/20261106T030000Z/);
    assert.match(
      r.stdout,
      /would file \(high, magellan-[0-9a-f]{12}\): \[Magellan\] Toolsets page fails to load/,
    );
    assert.match(r.stdout, /treat it as unverified/);
    assert.equal(r.requests.length, 0);
  });

  test("picks the latest run directory by name", () => {
    const cwd = mkdtempSync(join(scratch, "cwd-"));
    for (const [name, title] of [
      ["20261101T030000Z", "Old"],
      ["20261105T030000Z", "Newest"],
      ["20261103T030000Z", "Middle"],
      ["not-a-run", "Ignored"],
    ]) {
      mkdirSync(join(cwd, "e2e/.magellan", name!, "out"), { recursive: true });
      writeFileSync(
        join(cwd, "e2e/.magellan", name!, "out/findings.jsonl"),
        JSON.stringify(valid({ title })),
      );
    }
    const r = run({}, { cwd });
    assert.equal(r.status, 0, r.stderr);
    assert.match(r.stdout, /\[Magellan\] Newest/);
    assert.doesNotMatch(r.stdout, /Old|Middle|Ignored/);
  });

  test("uses the run directory it is given", () => {
    const { cwd, dir } = runDir([JSON.stringify(valid({ title: "Given" }))]);
    const r = run({ usage_run_dir: dir }, { cwd: scratch });
    assert.equal(r.status, 0, r.stderr);
    assert.match(r.stdout, /\[Magellan\] Given/);
    void cwd;
  });

  test("fails when there is no run", () => {
    const cwd = mkdtempSync(join(scratch, "cwd-"));
    mkdirSync(join(cwd, "e2e/.magellan"), { recursive: true });
    const r = run({}, { cwd });
    assert.notEqual(r.status, 0);
    assert.match(r.stderr, /No runs in e2e\/.magellan/);
  });

  test("skips lines that are not JSON, by line number only", () => {
    const { cwd } = runDir([
      "{not json SECRET-IN-LINE",
      "",
      JSON.stringify(valid()),
      "[1,2",
    ]);
    const r = run({}, { cwd });
    assert.equal(r.status, 0, r.stderr);
    assert.match(r.stdout, /skip   line 1: not JSON/);
    assert.match(r.stdout, /skip   line 4: not JSON/);
    assert.doesNotMatch(r.stdout + r.stderr, /SECRET-IN-LINE/);
    assert.match(r.stdout, /1 findings/);
  });

  test("skips invalid findings with the reason", () => {
    const { cwd } = runDir([
      JSON.stringify(valid({ kind: "nope" })),
      JSON.stringify(valid({ route: "https://evil.example" })),
      JSON.stringify(valid()),
    ]);
    const r = run({}, { cwd });
    assert.match(r.stdout, /skip   finding 1: bad kind/);
    assert.match(r.stdout, /skip   finding 2: bad route/);
    assert.match(r.stdout, /1 findings/);
  });

  test("an empty run files nothing and does not need Linear", () => {
    const { cwd } = runDir([]);
    const r = run({ usage_file: "true" }, { cwd });
    assert.equal(r.status, 0, r.stderr);
    assert.match(r.stdout, /0 findings/);
    assert.equal(r.requests.length, 0);
  });

  test("reads only file_finding items from the agent output", () => {
    const file = agentOutput([
      { type: "noop", message: "nothing" },
      { type: "add_comment", body: "hi" },
      { type: "file_finding", ...valid({ title: "From agent" }) },
      { ...valid({ title: "Untyped" }) },
      null,
    ]);
    const r = run({ usage_agent_output: file });
    assert.equal(r.status, 0, r.stderr);
    assert.match(r.stdout, /1 findings in the agent output/);
    assert.match(r.stdout, /From agent/);
    assert.doesNotMatch(r.stdout, /Untyped/);
  });

  test("an agent output with no items is empty", () => {
    const dir = mkdtempSync(join(scratch, "agent-"));
    const file = join(dir, "agent_output.json");
    writeFileSync(file, "{}");
    const r = run({ usage_agent_output: file });
    assert.equal(r.status, 0, r.stderr);
    assert.match(r.stdout, /0 findings in the agent output/);
  });

  test("the agent output wins over a run directory", () => {
    const { cwd } = runDir([JSON.stringify(valid({ title: "From dir" }))]);
    const file = agentOutput([
      { type: "file_finding", ...valid({ title: "From output" }) },
    ]);
    const r = run({ usage_agent_output: file }, { cwd });
    assert.match(r.stdout, /From output/);
    assert.doesNotMatch(r.stdout, /From dir/);
  });

  test("accepts steps as one string per line from the safe output", () => {
    const file = agentOutput([
      { type: "file_finding", ...valid({ steps: "Open it\nClick it" }) },
    ]);
    const r = run({ usage_agent_output: file });
    assert.match(r.stdout, /1\. Open it\n2\. Click it/);
  });

  test("in CI, prints only identifiers", () => {
    const secret = "UNFIXED-PRODUCTION-BUG-DETAIL";
    const file = agentOutput([
      {
        type: "file_finding",
        ...valid({ title: secret, actual: secret, evidence: secret }),
      },
    ]);
    const r = run({ usage_agent_output: file, CI: "true" });
    assert.equal(r.status, 0, r.stderr);
    assert.match(r.stdout, /would file \(high, magellan-[0-9a-f]{12}\)\n?/);
    assert.doesNotMatch(r.stdout + r.stderr, new RegExp(secret));
  });

  test("adds the run link to the issue body", () => {
    const file = agentOutput([{ type: "file_finding", ...valid() }]);
    const r = run({
      usage_agent_output: file,
      usage_run_url: "https://github.com/o/r/actions/runs/1",
    });
    assert.match(
      r.stdout,
      /\[Run\]\(https:\/\/github.com\/o\/r\/actions\/runs\/1\)/,
    );
  });
});

suite("file.mts --file", () => {
  const filing = { usage_file: "true", LINEAR_API_KEY: "lin_test" };

  test("needs a Linear key", () => {
    const file = agentOutput([{ type: "file_finding", ...valid() }]);
    const r = run({ usage_agent_output: file, usage_file: "true" });
    assert.notEqual(r.status, 0);
    assert.match(r.stderr, /LINEAR_API_KEY is not set/);
    assert.equal(r.requests.length, 0);
  });

  test("files a new finding in the team's triage state", () => {
    const file = agentOutput([{ type: "file_finding", ...valid() }]);
    const r = run(
      { ...filing, usage_agent_output: file, usage_team: "QA" },
      { linear: [teams, noHits, created(7)] },
    );
    assert.equal(r.status, 0, r.stderr);
    assert.match(
      r.stdout,
      /filed  DES-7 \[Magellan\] Toolsets page fails to load https:\/\/linear.app\/x\/DES-7/,
    );
    assert.equal(r.requests.length, 3);
    assert.deepEqual(r.requests[0]!.variables, { key: "QA" });
    const id = fingerprint(parseFinding(valid()));
    assert.deepEqual(r.requests[1]!.variables, { term: id });
    const input = r.requests[2]!.variables["input"] as Record<string, unknown>;
    assert.equal(input["teamId"], "team-1");
    assert.equal(input["stateId"], "triage-1");
    assert.equal(input["title"], "[Magellan] Toolsets page fails to load");
    assert.equal(input["priority"], 2);
    assert.ok(String(input["description"]).includes(id));
    assert.deepEqual(Object.keys(input).sort(), [
      "description",
      "priority",
      "stateId",
      "teamId",
      "title",
    ]);
  });

  test("defaults to the DES team", () => {
    const file = agentOutput([{ type: "file_finding", ...valid() }]);
    const r = run(
      { ...filing, usage_agent_output: file },
      { linear: [teams, noHits, created(1)] },
    );
    assert.deepEqual(r.requests[0]!.variables, { key: "DES" });
  });

  test("sends the key as the Authorization header", () => {
    const file = agentOutput([{ type: "file_finding", ...valid() }]);
    const r = run(
      { ...filing, usage_agent_output: file },
      { linear: [teams, noHits, created(1)] },
    );
    for (const req of r.requests) {
      assert.equal((req as unknown as { auth: string }).auth, "lin_test");
      assert.equal(
        (req as unknown as { url: string }).url,
        "https://api.linear.app/graphql",
      );
    }
  });

  for (const [severity, priority] of [
    ["high", 2],
    ["medium", 3],
    ["low", 4],
  ] as const) {
    test(`maps severity ${severity} to priority ${priority}`, () => {
      const file = agentOutput([
        { type: "file_finding", ...valid({ severity }) },
      ]);
      const r = run(
        { ...filing, usage_agent_output: file },
        { linear: [teams, noHits, created(1)] },
      );
      const input = r.requests[2]!.variables["input"] as Record<
        string,
        unknown
      >;
      assert.equal(input["priority"], priority);
    });
  }

  test("omits the state when the team has no triage state", () => {
    const file = agentOutput([{ type: "file_finding", ...valid() }]);
    const r = run(
      { ...filing, usage_agent_output: file },
      {
        linear: [
          {
            data: {
              teams: { nodes: [{ id: "team-1", states: { nodes: [] } }] },
            },
          },
          noHits,
          created(2),
        ],
      },
    );
    assert.equal(r.status, 0, r.stderr);
    const input = r.requests[2]!.variables["input"] as Record<string, unknown>;
    assert.equal("stateId" in input, false);
  });

  test("fails on an unknown team", () => {
    const file = agentOutput([{ type: "file_finding", ...valid() }]);
    const r = run(
      { ...filing, usage_agent_output: file, usage_team: "NOPE" },
      { linear: [{ data: { teams: { nodes: [] } } }] },
    );
    assert.notEqual(r.status, 0);
    assert.match(r.stderr, /No Linear team with key NOPE/);
  });

  test("skips a finding already filed and still open", () => {
    const id = fingerprint(parseFinding(valid()));
    const file = agentOutput([{ type: "file_finding", ...valid() }]);
    const r = run(
      { ...filing, usage_agent_output: file },
      {
        linear: [
          teams,
          {
            data: {
              searchIssues: {
                nodes: [
                  {
                    identifier: "DES-3",
                    description: `x <sub>${id}</sub>`,
                    state: { type: "triage" },
                  },
                ],
              },
            },
          },
        ],
      },
    );
    assert.equal(r.status, 0, r.stderr);
    assert.match(r.stdout, /seen   DES-3/);
    assert.equal(r.requests.length, 2);
  });

  test("a canceled issue still counts as seen", () => {
    const id = fingerprint(parseFinding(valid()));
    const file = agentOutput([{ type: "file_finding", ...valid() }]);
    const r = run(
      { ...filing, usage_agent_output: file },
      {
        linear: [
          teams,
          {
            data: {
              searchIssues: {
                nodes: [
                  {
                    identifier: "DES-4",
                    description: id,
                    state: { type: "canceled" },
                  },
                ],
              },
            },
          },
        ],
      },
    );
    assert.match(r.stdout, /seen   DES-4/);
    assert.equal(r.requests.length, 2);
  });

  test("a completed issue means the bug came back: file a new one", () => {
    const id = fingerprint(parseFinding(valid()));
    const file = agentOutput([{ type: "file_finding", ...valid() }]);
    const r = run(
      { ...filing, usage_agent_output: file },
      {
        linear: [
          teams,
          {
            data: {
              searchIssues: {
                nodes: [
                  {
                    identifier: "DES-5",
                    description: id,
                    state: { type: "completed" },
                  },
                ],
              },
            },
          },
          created(6),
        ],
      },
    );
    assert.match(r.stdout, /filed  DES-6/);
  });

  test("a fuzzy search hit without the fingerprint is not a match", () => {
    const file = agentOutput([{ type: "file_finding", ...valid() }]);
    const r = run(
      { ...filing, usage_agent_output: file },
      {
        linear: [
          teams,
          {
            data: {
              searchIssues: {
                nodes: [
                  {
                    identifier: "DES-8",
                    description: "magellan-000000000000",
                    state: { type: "triage" },
                  },
                  { identifier: "DES-9", state: { type: "triage" } },
                ],
              },
            },
          },
          created(10),
        ],
      },
    );
    assert.match(r.stdout, /filed  DES-10/);
  });

  test("files several findings, each searched separately", () => {
    const file = agentOutput([
      { type: "file_finding", ...valid({ signature: "one" }) },
      { type: "file_finding", ...valid({ signature: "two" }) },
    ]);
    const r = run(
      { ...filing, usage_agent_output: file },
      { linear: [teams, noHits, created(11), noHits, created(12)] },
    );
    assert.equal(r.status, 0, r.stderr);
    assert.match(r.stdout, /filed  DES-11[\s\S]*filed  DES-12/);
    assert.notDeepEqual(r.requests[1]!.variables, r.requests[3]!.variables);
  });

  test("in CI, a Linear error prints codes, not messages", () => {
    const file = agentOutput([{ type: "file_finding", ...valid() }]);
    const r = run(
      { ...filing, usage_agent_output: file, CI: "true" },
      {
        linear: [
          {
            status: 400,
            body: {
              errors: [
                {
                  message: "Echoed AGENT-TEXT",
                  extensions: { code: "INVALID_INPUT" },
                },
                { message: "No code AGENT-TEXT" },
              ],
            },
          },
        ],
      },
    );
    assert.notEqual(r.status, 0);
    assert.match(r.stderr, /Linear: 400 INVALID_INPUT,error/);
    assert.doesNotMatch(r.stdout + r.stderr, /AGENT-TEXT/);
  });

  test("locally, a Linear error prints the messages", () => {
    const file = agentOutput([{ type: "file_finding", ...valid() }]);
    const r = run(
      { ...filing, usage_agent_output: file },
      {
        linear: [
          { status: 200, body: { errors: [{ message: "Rate limited" }] } },
        ],
      },
    );
    assert.notEqual(r.status, 0);
    assert.match(r.stderr, /Rate limited/);
  });

  test("a non-JSON Linear response is an error", () => {
    const file = agentOutput([{ type: "file_finding", ...valid() }]);
    const r = run(
      { ...filing, usage_agent_output: file, CI: "true" },
      { linear: [{ status: 502, raw: "<html>bad gateway</html>" }] },
    );
    assert.notEqual(r.status, 0);
    assert.match(r.stderr, /Linear: 502/);
  });

  test("in CI, filing logs the fingerprint, not the title or URL", () => {
    const file = agentOutput([
      { type: "file_finding", ...valid({ title: "SECRET-TITLE" }) },
    ]);
    const r = run(
      { ...filing, usage_agent_output: file, CI: "true" },
      { linear: [teams, noHits, created(13)] },
    );
    assert.match(r.stdout, /filed  DES-13 magellan-[0-9a-f]{12}\n/);
    assert.doesNotMatch(r.stdout, /SECRET-TITLE|linear.app/);
  });

  test("the issue title is one plain line", () => {
    const file = agentOutput([
      {
        type: "file_finding",
        ...valid({ title: "Bug\n\n# Heading\twith tab" }),
      },
    ]);
    const r = run(
      { ...filing, usage_agent_output: file },
      { linear: [teams, noHits, created(14)] },
    );
    const input = r.requests[2]!.variables["input"] as Record<string, unknown>;
    assert.equal(input["title"], "[Magellan] Bug # Heading with tab");
  });
});
