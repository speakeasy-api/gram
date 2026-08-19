import { spawnSync } from "node:child_process";
import { describe, expect, it } from "vitest";

import {
  buildCloudDefaultEnvironmentSnippet,
  buildCloudSetupCommand,
  CLOUD_ORG_TOKEN_SENTINEL,
  RELEASES_BASE,
} from "./cloud-setup";

const input = {
  version: "1.2.3",
  sha256: "a".repeat(64),
  email: "remote-session@example.test",
  orgToken: "spk_org_test_token",
};

describe("buildCloudSetupCommand", () => {
  it("renders a minimal, valid bootstrap for the device-agent CLI", () => {
    const script = buildCloudSetupCommand(input);
    const result = spawnSync("bash", ["-n"], {
      input: script,
      encoding: "utf8",
    });

    expect(result.stderr).toBe("");
    expect(result.status).toBe(0);
    expect(script).toContain(
      `${RELEASES_BASE}/v1.2.3/speakeasy_1.2.3_linux_amd64`,
    );
    expect(script).toContain("sha256sum -c -");
    expect(script).toContain("'remote-session@example.test'");
    expect(script).toContain("'spk_org_test_token'");
    expect(script).toContain('"$CLI" setup --anthropic-cloud');
  });

  it("leaves machine provisioning and SessionStart setup to the CLI", () => {
    const script = buildCloudSetupCommand(input);

    expect(script).not.toContain("managed.json");
    expect(script).not.toContain("managed-settings.json");
    expect(script).not.toContain("speakeasyd");
    expect(script).not.toContain("speakeasy-helper");
    expect(script).not.toContain("SessionStart");
  });

  it("supports the inline token-generation sentinel", () => {
    expect(
      buildCloudSetupCommand({
        ...input,
        orgToken: CLOUD_ORG_TOKEN_SENTINEL,
      }),
    ).toContain(CLOUD_ORG_TOKEN_SENTINEL);
  });

  it("quotes identity values before passing them through the environment", () => {
    const script = buildCloudSetupCommand({
      ...input,
      email: "remote'session@example.test",
      orgToken: "token'with-quote",
    });
    const result = spawnSync("bash", ["-n"], {
      input: script,
      encoding: "utf8",
    });

    expect(result.stderr).toBe("");
    expect(result.status).toBe(0);
    expect(script).toContain(`remote'\\''session@example.test`);
    expect(script).toContain(`token'\\''with-quote`);
  });

  it("rejects invalid dynamic release data and email", () => {
    expect(() =>
      buildCloudSetupCommand({ ...input, version: "1.2.3; rm -rf /" }),
    ).toThrow(/valid pinned device agent version/);
    expect(() =>
      buildCloudSetupCommand({ ...input, sha256: "not-a-checksum" }),
    ).toThrow(/valid device agent CLI checksum/);
    expect(() => buildCloudSetupCommand({ ...input, email: "@" })).toThrow(
      /valid remote session identity email/,
    );
  });
});

describe("buildCloudDefaultEnvironmentSnippet", () => {
  it("uses a placeholder environment id", () => {
    expect(JSON.parse(buildCloudDefaultEnvironmentSnippet())).toEqual({
      remote: { defaultEnvironmentId: "env_…" },
    });
  });
});
