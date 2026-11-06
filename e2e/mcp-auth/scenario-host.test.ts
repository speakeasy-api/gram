import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";

test("failed TLS initialization closes the already started HTTP listener", () => {
  const directory = mkdtempSync(join(tmpdir(), "scenario-host-failure-"));
  const invalid = join(directory, "invalid.pem");
  writeFileSync(invalid, "not a certificate");
  try {
    const child = spawnSync(
      process.execPath,
      [
        "--input-type=module",
        "-e",
        `
      import assert from 'node:assert/strict';
      import { startScenarioHost } from './e2e/mcp-auth/scenario-host.ts';
      await assert.rejects(startScenarioHost(${JSON.stringify(directory)}));
    `,
      ],
      {
        encoding: "utf8",
        timeout: 15_000,
        env: {
          ...process.env,
          GRAM_SSL_CERT_FILE: invalid,
          GRAM_SSL_KEY_FILE: invalid,
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
