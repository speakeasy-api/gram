import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const task = fileURLToPath(new URL("./daemon.sh", import.meta.url));
const daemons = [
  ["server", "GRAM_CONTROL_ADDRESS", "GRAM_CONTROL_PORT"],
  ["admin", "GRAM_ADMIN_CONTROL_ADDRESS", "GRAM_ADMIN_CONTROL_PORT"],
  ["worker", "GRAM_WORKER_CONTROL_ADDRESS", "GRAM_WORKER_CONTROL_PORT"],
  ["streams", "GRAM_STREAMS_CONTROL_ADDRESS", "GRAM_STREAMS_CONTROL_PORT"],
  ["dev-idp", "GRAM_DEVIDP_CONTROL_ADDRESS", "GRAM_DEVIDP_CONTROL_PORT"],
  ["mock-oidc", "MOCK_OIDC_ADDRESS", "GRAM_ADMIN_OIDC_EMULATOR_PORT"],
] as const;

const addresses = [
  [undefined, "localhost:23456"],
  ["", "localhost:23456"],
  [":23456", "localhost:23456"],
  ["0.0.0.0:23456", "localhost:23456"],
  ["[::]:23456", "localhost:23456"],
  ["100.64.0.1:23456", "100.64.0.1:23456"],
  ["127.0.0.1:23456", "127.0.0.1:23456"],
  ["[::1]:23456", "[::1]:23456"],
] as const;

for (const [name, addressVar, portVar] of daemons) {
  for (const [address, expected] of addresses) {
    test(`${name} probes ${String(address)} as ${expected}`, () => {
      const env = {
        ...process.env,
        GRAM_DEVIDP_ADDRESS: "192.0.2.10:34567",
        GRAM_DEVIDP_PORT: "34567",
        usage_name: name,
        [portVar]: "23456",
        [addressVar]: address,
      };
      if (address === undefined) delete env[addressVar];
      const result = spawnSync(
        "bash",
        [
          "-c",
          'mise() { printf "%s" "$4"; }; source "$1"',
          "check-daemon-test",
          task,
        ],
        { env, encoding: "utf8" },
      );
      assert.equal(result.status, 0, result.stderr);
      assert.equal(result.stdout, `http://${expected}/healthz`);
    });
  }
}
