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
import { join } from "node:path";
import { test, type TestContext } from "node:test";
import { fileURLToPath } from "node:url";

const repo = fileURLToPath(new URL("../", import.meta.url));
function fixture(t: TestContext) {
  const root = mkdtempSync(join(tmpdir(), "catalog-tasks-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  mkdirSync(join(root, "bin"));
  mkdirSync(join(root, "server"));
  writeFileSync(join(root, "calls"), "");
  writeFileSync(
    join(root, "bin/go"),
    '#!/bin/bash\nprintf "%s\\n" "$*" >> "$FIXTURE/calls"\nexit "${GO_STATUS:-0}"\n',
    { mode: 0o755 },
  );
  return {
    root,
    run(task: string, env: NodeJS.ProcessEnv = {}, args: string[] = []) {
      const result = spawnSync("/bin/bash", [join(repo, task), ...args], {
        cwd: root,
        env: {
          PATH: `${root}/bin:/usr/bin:/bin`,
          HOME: root,
          FIXTURE: root,
          ...env,
        },
        encoding: "utf8",
      });
      return { ...result, calls: readFileSync(join(root, "calls"), "utf8") };
    },
  };
}

test("bootstrap without explicit opt-in reports an unpopulated production catalog without access", (t) => {
  const f = fixture(t);
  for (const value of [undefined, "false", "0", "f", "F", "FALSE", "False"]) {
    const result = f.run("catalog/bootstrap.sh", {
      GRAM_CATALOG_SOURCE_MANAGED: value,
    });
    assert.equal(result.status, 0, result.stderr);
    assert.match(result.stdout, /production catalog.*not populated/i);
    assert.equal(result.calls, "");
  }
});

const enabled = {
  GRAM_CATALOG_SOURCE_MANAGED: "true",
  GRAM_CATALOG_SOURCE_URL: "https://catalog.example.invalid",
  GRAM_CATALOG_SOURCE_AUDIENCE: "https://catalog.example.invalid",
  GRAM_CATALOG_SOURCE_READER: "catalog-reader@example.invalid",
};
test("enabled bootstrap calls the server CLI and rejects missing source configuration", (t) => {
  const f = fixture(t);
  const result = f.run("catalog/bootstrap.sh", enabled);
  assert.equal(result.status, 0, result.stderr);
  assert.equal(result.calls, "run . catalog-transfer --action bootstrap\n");
  for (const key of [
    "GRAM_CATALOG_SOURCE_URL",
    "GRAM_CATALOG_SOURCE_AUDIENCE",
    "GRAM_CATALOG_SOURCE_READER",
  ]) {
    const missing = f.run("catalog/bootstrap.sh", { ...enabled, [key]: "" });
    assert.notEqual(missing.status, 0);
    assert.match(missing.stderr, new RegExp(key));
  }
});
test("unavailable bootstrap fails with ADC, C1, and retry guidance", (t) => {
  const f = fixture(t);
  const result = f.run("catalog/bootstrap.sh", { ...enabled, GO_STATUS: "1" });
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /ADC/);
  assert.match(result.stderr, /C1.*ConductorOne/);
  assert.match(result.stderr, /mise run catalog:bootstrap/);
});

test("refresh previews by default and applies only an exact UUID without seed or reset", (t) => {
  const f = fixture(t);
  const preview = f.run("catalog/refresh.sh", enabled);
  assert.equal(preview.status, 0, preview.stderr);
  assert.equal(preview.calls, "run . catalog-transfer --action preview\n");
  const plan = "12345678-1234-4234-8234-123456789abc";
  const apply = f.run("catalog/refresh.sh", {
    ...enabled,
    usage_action: "apply",
    usage_plan_id: plan,
  });
  assert.equal(apply.status, 0, apply.stderr);
  assert.equal(
    apply.calls,
    `run . catalog-transfer --action preview\nrun . catalog-transfer --action apply --plan-id ${plan}\n`,
  );
  for (const env of [
    { usage_action: "apply" },
    { usage_action: "apply", usage_plan_id: "yes" },
    { usage_action: "bootstrap" },
    { usage_action: "preview", usage_plan_id: plan },
  ]) {
    assert.notEqual(
      f.run("catalog/refresh.sh", { ...enabled, ...env }).status,
      0,
    );
  }
});

function bootFixture(t: TestContext) {
  const f = fixture(t);
  mkdirSync(join(f.root, "gitdir"));
  writeFileSync(join(f.root, "zero"), readFileSync(join(repo, "../zero")), {
    mode: 0o755,
  });
  writeFileSync(
    join(f.root, "bin/git"),
    `#!/bin/bash
case "$*" in
  'rev-parse --is-inside-work-tree') exit 1 ;;
  *) echo "$FIXTURE/gitdir" ;;
esac
`,
    { mode: 0o755 },
  );
  writeFileSync(join(f.root, "bin/sleep"), "#!/bin/bash\nexit 0\n", {
    mode: 0o755,
  });
  writeFileSync(
    join(f.root, "bin/mise"),
    `#!/bin/bash
printf '%s\n' "$*" >> "$FIXTURE/calls"
case "$*" in
  'which speakeasy'|'tasks info local:start') exit 1 ;;
  'run seed')
    if [ -n "\${FAIL_FIRST_SEED:-}" ] && [ ! -f "$FIXTURE/seed-tried" ]; then
      touch "$FIXTURE/seed-tried"
      exit 1
    fi ;;
esac
if [ "$*" = "run \${FAIL_TASK:-none}" ]; then exit 1; fi
exit 0
`,
    { mode: 0o755 },
  );
  return f;
}

test("zero bootstraps after seeding and exposes bootstrap failure without summary success", (t) => {
  const f = bootFixture(t);
  const result = f.run("../zero", { FAIL_TASK: "catalog:bootstrap" }, [
    "--agent",
  ]);
  assert.equal(result.status, 21, result.stderr);
  assert.match(result.calls, /run start\nrun seed\nrun catalog:bootstrap\n/);
  assert.doesNotMatch(result.calls, /run zero:summary/);
});

test("workboot cannot hide a catalog failure with a seed-only retry", (t) => {
  const f = bootFixture(t);
  const result = f.run("git/workboot.sh", { FAIL_TASK: "catalog:bootstrap" });
  assert.equal(result.status, 21, result.stderr);
  assert.equal(result.calls.match(/run seed\n/g)?.length, 1);
  assert.doesNotMatch(result.calls, /run pause/);
  assert.equal(
    readFileSync(join(f.root, "gitdir/gram-stack-boot.failed"), "utf8"),
    "21\n",
  );
});

test("workboot seed fallback must bootstrap successfully before pausing", (t) => {
  const f = bootFixture(t);
  const result = f.run("git/workboot.sh", { FAIL_FIRST_SEED: "1" });
  assert.equal(result.status, 0, result.stderr);
  assert.match(
    result.calls,
    /run seed\nrun seed\nrun catalog:bootstrap\nrun pause\n/,
  );
});

test("workboot seed fallback propagates catalog failure without further seeding", (t) => {
  const f = bootFixture(t);
  const result = f.run("git/workboot.sh", {
    FAIL_FIRST_SEED: "1",
    FAIL_TASK: "catalog:bootstrap",
  });
  assert.equal(result.status, 21, result.stderr);
  assert.equal(result.calls.match(/run seed\n/g)?.length, 2);
  assert.doesNotMatch(result.calls, /run pause/);
});

test("workboot does not seed over an earlier setup failure", (t) => {
  const f = bootFixture(t);
  const result = f.run("git/workboot.sh", { FAIL_TASK: "zero:migrations" });
  assert.notEqual(result.status, 0);
  assert.doesNotMatch(result.calls, /run seed|run catalog:bootstrap|run pause/);
});

test("workboot pauses a successful bootstrap without retrying seed", (t) => {
  const f = bootFixture(t);
  const result = f.run("git/workboot.sh");
  assert.equal(result.status, 0, result.stderr);
  assert.match(
    result.calls,
    /run start\nrun seed\nrun catalog:bootstrap\nrun zero:summary\nrun pause\n/,
  );
  assert.equal(result.calls.match(/run seed\n/g)?.length, 1);
});

test("catalog tasks accept Go boolean true spellings and reject invalid modes", (t) => {
  const f = fixture(t);
  for (const task of ["catalog/bootstrap.sh", "catalog/refresh.sh"]) {
    for (const value of ["1", "t", "T", "TRUE", "true", "True"]) {
      const result = f.run(task, {
        ...enabled,
        GRAM_CATALOG_SOURCE_MANAGED: value,
      });
      assert.equal(result.status, 0, `${task}: ${value}: ${result.stderr}`);
      assert.match(result.calls, /catalog-transfer --action/);
      assert.doesNotMatch(result.stdout, /mode disabled/);
    }
    for (const value of ["yes", "enabled", "truE"]) {
      const result = f.run(task, {
        ...enabled,
        GRAM_CATALOG_SOURCE_MANAGED: value,
      });
      assert.notEqual(result.status, 0, `${task}: ${value}`);
      assert.match(result.stderr, /Invalid GRAM_CATALOG_SOURCE_MANAGED/);
    }
  }
});
