import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { parseTOML } from "confbox";

const config = parseTOML(
  readFileSync(new URL("../../mise.toml", import.meta.url), "utf8"),
) as { env: Record<string, string> };
const publicBindings = [
  ["GRAM_SERVER_ADDRESS", "GRAM_SERVER_PORT"],
  ["GRAM_ADMIN_SERVER_ADDRESS", "GRAM_ADMIN_PORT"],
  ["MOCK_OIDC_ADDRESS", "GRAM_ADMIN_OIDC_EMULATOR_PORT"],
  ["GRAM_DEVIDP_ADDRESS", "GRAM_DEVIDP_PORT"],
] as const;
const controlBindings = [
  ["GRAM_CONTROL_ADDRESS", "GRAM_CONTROL_PORT"],
  ["GRAM_WORKER_CONTROL_ADDRESS", "GRAM_WORKER_CONTROL_PORT"],
  ["GRAM_STREAMS_CONTROL_ADDRESS", "GRAM_STREAMS_CONTROL_PORT"],
  ["GRAM_ADMIN_CONTROL_ADDRESS", "GRAM_ADMIN_CONTROL_PORT"],
  ["GRAM_DEVIDP_CONTROL_ADDRESS", "GRAM_DEVIDP_CONTROL_PORT"],
] as const;
const keys = new Set<string>([
  "GRAM_BIND_IP",
  "GRAM_PYSTREAMS_CONTROL_HOST",
  ...[...publicBindings, ...controlBindings].flatMap(([address, port]) => [
    address,
    port,
  ]),
]);

for (const bindIP of [config.env.GRAM_BIND_IP, "", "100.64.0.1", "[::1]"]) {
  test(`listener templates render ${bindIP || "wildcard hosts"}`, (t) => {
    const dir = mkdtempSync(join(tmpdir(), "local-listeners-test-"));
    t.after(() => rmSync(dir, { recursive: true, force: true }));
    const entries = Object.entries(config.env).filter(([key]) => keys.has(key));
    writeFileSync(
      join(dir, "mise.toml"),
      "[env]\n" +
        entries
          .map(
            ([key, value]) =>
              `${key} = ${JSON.stringify(key === "GRAM_BIND_IP" ? bindIP : value)}`,
          )
          .join("\n"),
    );
    const result = spawnSync("mise", ["env", "--json"], {
      cwd: dir,
      env: {
        PATH: process.env["PATH"],
        HOME: dir,
        MISE_CONFIG_DIR: dir,
        MISE_TRUSTED_CONFIG_PATHS: dir,
      },
      encoding: "utf8",
    });
    assert.equal(result.status, 0, result.stderr);
    const env = JSON.parse(result.stdout) as Record<string, string>;
    for (const [address, port] of publicBindings) {
      assert.equal(env[address], `${bindIP}:${config.env[port]}`);
    }
    for (const [address, port] of controlBindings) {
      assert.equal(env[address], `127.0.0.1:${config.env[port]}`);
    }
    assert.equal(env.GRAM_PYSTREAMS_CONTROL_HOST, "127.0.0.1");
  });
}
