import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { test } from "node:test";

for (const input of ["", "events\n", "quit\n"]) {
  test(`fixture serve exits after stdin closes (${JSON.stringify(input)})`, async (t) => {
    const child = spawn(
      process.execPath,
      [new URL("./serve.ts", import.meta.url).pathname],
      {
        stdio: ["pipe", "pipe", "pipe"],
      },
    );
    t.after(() => child.kill("SIGKILL"));
    let stdout = "";
    let stderr = "";
    child.stdout.setEncoding("utf8").on("data", (chunk) => {
      stdout += chunk;
    });
    child.stderr.setEncoding("utf8").on("data", (chunk) => {
      stderr += chunk;
    });
    const closed = once(child, "close", {
      signal: AbortSignal.timeout(10_000),
    });
    child.stdin.end(input);
    const [code, signal] = await closed;
    assert.equal(signal, null, stderr);
    assert.equal(code, 0, stderr);
    assert.match(stdout, /127\.0\.0\.1/);
  });
}
