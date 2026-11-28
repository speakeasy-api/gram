import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { existsSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";

for (const signal of [undefined, "SIGTERM", "SIGINT"] as const) {
  test(`worker directories are removed on ${signal ?? "normal exit"}`, async () => {
    const root = mkdtempSync(join(tmpdir(), "worker-temp-test-"));
    const directories = [join(root, "first"), join(root, "second")];
    const child = spawn(
      process.execPath,
      [
        "--input-type=module",
        "-e",
        `
        import assert from 'node:assert/strict';
        import { mkdirSync, existsSync } from 'node:fs';
        import { registerWorkerTempDirectory } from ${JSON.stringify(new URL("./worker-temp-directory.ts", import.meta.url).href)};
        const events = ['exit', 'SIGINT', 'SIGTERM'];
        const baseline = events.map(event => process.listenerCount(event));
        const directories = ${JSON.stringify(directories)};
        const checkListeners = extra => events.forEach((event, index) =>
          assert.equal(process.listenerCount(event), baseline[index] + extra, event));
        // Failed builds must release their listeners before another build retries.
        for (let attempt = 0; attempt < 12; attempt++) {
          mkdirSync(directories[0]);
          const cleanup = registerWorkerTempDirectory(directories[0]);
          checkListeners(1);
          cleanup();
          cleanup();
          assert.equal(existsSync(directories[0]), false);
          checkListeners(0);
        }
        for (const directory of directories) {
          mkdirSync(directory);
          registerWorkerTempDirectory(directory);
          checkListeners(1);
        }
        ${signal ? "setInterval(() => {}, 1000);" : "process.exitCode = 7;"}
        process.stdout.write('ready');
        `,
      ],
      { stdio: ["ignore", "pipe", "pipe"] },
    );
    let stderr = "";
    child.stderr.setEncoding("utf8").on("data", (chunk) => (stderr += chunk));
    // Close waits for stdout/stderr to drain as well as process termination.
    const closed = once(child, "close");
    const timeout = setTimeout(() => child.kill("SIGKILL"), 10_000);
    let ready = false;
    let existedBeforeSignal = false;
    child.stdout.once("data", () => {
      ready = true;
      if (signal) {
        existedBeforeSignal = directories.every(existsSync);
        child.kill(signal);
      }
    });
    try {
      const [code, actualSignal] = await closed;
      assert.ok(ready, stderr);
      if (signal) assert.ok(existedBeforeSignal);
      assert.equal(actualSignal, signal ?? null, stderr);
      assert.equal(code, signal ? null : 7, stderr);
      for (const directory of directories) {
        assert.equal(existsSync(directory), false, `${directory} leaked`);
      }
      assert.equal(stderr, "");
    } finally {
      clearTimeout(timeout);
      child.kill("SIGKILL");
      await closed.catch(() => {});
      rmSync(root, { recursive: true, force: true });
    }
  });
}

test("existing worker signal handler receives termination only once", async () => {
  const directory = mkdtempSync(join(tmpdir(), "worker-handler-test-"));
  const child = spawn(
    process.execPath,
    [
      "--input-type=module",
      "-e",
      `
    import assert from 'node:assert/strict';
    import { existsSync } from 'node:fs';
    import { registerWorkerTempDirectory } from ${JSON.stringify(new URL("./worker-temp-directory.ts", import.meta.url).href)};
    let deliveries = 0;
    const directory = ${JSON.stringify(directory)};
    process.on('SIGTERM', () => {
      deliveries++;
      setTimeout(() => {
        assert.equal(deliveries, 1);
        assert.equal(existsSync(directory), false);
        process.exit(42);
      }, 30);
    });
    registerWorkerTempDirectory(directory);
    setInterval(() => {}, 1000);
    process.stdout.write('ready');
  `,
    ],
    { stdio: ["ignore", "pipe", "pipe"] },
  );
  let stderr = "";
  child.stderr.setEncoding("utf8").on("data", (chunk) => (stderr += chunk));
  const closed = once(child, "close");
  const timeout = setTimeout(() => child.kill("SIGKILL"), 10_000);
  child.stdout.once("data", () => child.kill("SIGTERM"));
  try {
    const [code, signal] = await closed;
    assert.equal(code, 42, stderr);
    assert.equal(signal, null, stderr);
  } finally {
    clearTimeout(timeout);
    child.kill("SIGKILL");
    await closed.catch(() => {});
    rmSync(directory, { recursive: true, force: true });
  }
});
