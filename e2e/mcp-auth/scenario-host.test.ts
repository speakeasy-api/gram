import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";

for (const publicationFails of [false, true]) {
  test(`unavailable TLS listeners: publication failure ${publicationFails}`, () => {
    const directory = mkdtempSync(join(tmpdir(), "scenario-host-failure-"));
    // A directory at host.json makes publication fail after HTTP starts.
    if (publicationFails) mkdirSync(join(directory, "host.json"));
    try {
      const child = spawnSync(
        process.execPath,
        [
          "--input-type=module",
          "-e",
          `
      import assert from 'node:assert/strict';
      import { startScenarioHost } from './e2e/mcp-auth/scenario-host.ts';
      if (${publicationFails}) {
        await assert.rejects(startScenarioHost(${JSON.stringify(directory)}), { code: 'EISDIR' });
      } else {
        const host = await startScenarioHost(${JSON.stringify(directory)});
        assert.equal(typeof host.info.http, 'string');
        assert.equal(typeof host.info.workload.unavailable, 'string');
        assert.equal(typeof host.info.localhost.unavailable, 'string');
        await host.close();
      }
    `,
        ],
        {
          encoding: "utf8",
          timeout: 15_000,
          env: {
            ...process.env,
            CAROOT: join(directory, "missing-ca"),
            GRAM_SSL_CERT_FILE: join(directory, "missing.pem"),
            GRAM_SSL_KEY_FILE: join(directory, "missing-key.pem"),
            MCP_AUTH_WORKLOAD_HOST: "invalid",
          },
        },
      );
      assert.equal(child.error, undefined);
      assert.equal(child.status, 0, child.stderr);
    } finally {
      rmSync(directory, { recursive: true, force: true });
    }
  });
}
