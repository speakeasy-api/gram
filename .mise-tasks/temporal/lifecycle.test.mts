import assert from "node:assert/strict";
import { test, type TestContext } from "node:test";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { execFile } from "node:child_process";
import { promisify } from "node:util";

const exec = promisify(execFile);
async function lifecycleFixture(t: TestContext) {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "gram-temporal-test-"));
  t.after(() => fs.rm(dir, { recursive: true, force: true }));
  const bin = path.join(dir, "bin");
  await fs.mkdir(bin);
  await fs.mkdir(path.join(dir, "git"));
  const programs: Record<string, string> = {
    git: 'printf "%s/git\\n" "$FIXTURE"',
    pitchfork:
      'if [ "$1" = stop ]; then [ "${FAIL_STOP:-}" != 1 ] || exit 1; touch "$FIXTURE/stopped"; fi',
    mise: 'if [ "$2" = temporal:schedules ]; then test -f "$FIXTURE/stopped" || exit 99; touch "$FIXTURE/paused"; fi',
    docker:
      'test -f "$FIXTURE/stopped" || exit 98; if [ "$3" = "*" ] && [ "$4" = stop ]; then test -f "$FIXTURE/paused" || exit 97; fi; printf "%s\\n" "$*" >> "$FIXTURE/docker-calls"; touch "$FIXTURE/containers-stopped"',
  };
  for (const [name, script] of Object.entries(programs)) {
    await fs.writeFile(path.join(bin, name), `#!/bin/bash\n${script}\n`, {
      mode: 0o755,
    });
  }
  return {
    dir,
    env: {
      ...process.env,
      PATH: `${bin}:${process.env["PATH"]}`,
      FIXTURE: dir,
      GRAM_NO_PARK: "1",
      TEMPORAL_NAMESPACE: "gram-infra-example",
    },
  };
}
test("pause stops registration before schedule enumeration and containers", async (t) => {
  const fixture = await lifecycleFixture(t);
  await exec("/bin/bash", [path.resolve(".mise-tasks/pause.sh")], {
    cwd: fixture.dir,
    env: fixture.env,
  });
  await fs.access(path.join(fixture.dir, "containers-stopped"));
});
test("failed daemon stop prevents publishing a paused stack", async (t) => {
  const fixture = await lifecycleFixture(t);
  await assert.rejects(
    exec("/bin/bash", [path.resolve(".mise-tasks/pause.sh")], {
      cwd: fixture.dir,
      env: { ...fixture.env, FAIL_STOP: "1" },
    }),
  );
  await assert.rejects(
    fs.access(path.join(fixture.dir, "git/gram-stack-paused")),
  );
  await assert.rejects(fs.access(path.join(fixture.dir, "paused")));
});
test("nuke removes local volumes without touching shared services", async (t) => {
  const fixture = await lifecycleFixture(t);
  await exec(
    "/bin/bash",
    [
      path.resolve(".mise-tasks/nuke.sh"),
      "--keep-shared",
      "--delete-namespace",
    ],
    { cwd: fixture.dir, env: fixture.env },
  );
  const calls = await fs.readFile(
    path.join(fixture.dir, "docker-calls"),
    "utf8",
  );
  assert.match(calls, /down --volumes --remove-orphans/);
  assert.doesNotMatch(calls, /gram-shared/);
});
