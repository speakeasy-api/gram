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
  it("renders a valid script that installs the pinned daemon", () => {
    const script = buildCloudSetupCommand(input);
    const result = spawnSync("bash", ["-n"], {
      input: script,
      encoding: "utf8",
    });

    expect(result.stderr).toBe("");
    expect(result.status).toBe(0);
    expect(script).toContain(
      `${RELEASES_BASE}/v\${VERSION}/speakeasyd_\${VERSION}_linux_amd64`,
    );
    expect(script).toContain("VERSION='1.2.3'");
    expect(script).toContain(`SHA256='${"a".repeat(64)}'`);
    expect(script).toContain("sha256sum -c -");
    expect(script).toContain("chmod 0755 /usr/local/bin/speakeasyd");
  });

  it("writes managed enrollment for the daemon to read at startup", () => {
    const script = buildCloudSetupCommand(input);

    expect(script).toContain("/etc/speakeasy/managed.json");
    expect(script).toContain(`EMAIL='remote-session@example.test'`);
    expect(script).toContain(`ORG_TOKEN='spk_org_test_token'`);
    expect(script).toContain(`"auto_update": "disabled"`);
    expect(script).toContain("chmod 0600 /etc/speakeasy/managed.json");
  });

  it("registers a singleton SessionStart hook that revives the daemon", () => {
    const script = buildCloudSetupCommand(input);
    const settingsJSON = script.match(/<<'JSON'\n([\s\S]*?)\nJSON\n/)?.[1];

    expect(settingsJSON).toBeDefined();
    const settings = JSON.parse(settingsJSON ?? "") as {
      hooks: {
        SessionStart: {
          matcher: string;
          hooks: { type: string; command: string; async: boolean }[];
        }[];
      };
    };
    const [entry] = settings.hooks.SessionStart;
    expect(entry?.matcher).toBe("startup|resume");
    const [hook] = entry?.hooks ?? [];
    expect(hook?.type).toBe("command");
    expect(hook?.async).toBe(true);
    expect(hook?.command).toContain("flock -n /run/speakeasyd.lock");
    expect(hook?.command).toContain("/usr/local/bin/speakeasyd");
  });

  it("leaves everything after daemon start to the agent's own sync loop", () => {
    const script = buildCloudSetupCommand(input);

    expect(script).not.toContain("setup --anthropic-cloud");
    expect(script).not.toContain("remote-session start");
    expect(script).not.toContain("speakeasy-helper");
    expect(script).not.toContain("managed-settings.json");
  });

  it("supports the inline token-generation sentinel", () => {
    expect(
      buildCloudSetupCommand({
        ...input,
        orgToken: CLOUD_ORG_TOKEN_SENTINEL,
      }),
    ).toContain(CLOUD_ORG_TOKEN_SENTINEL);
  });

  it("shell-quotes identity values", () => {
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

  it("rejects invalid release data, email, and JSON-breaking values", () => {
    expect(() =>
      buildCloudSetupCommand({ ...input, version: "1.2.3; rm -rf /" }),
    ).toThrow(/valid pinned device agent version/);
    expect(() =>
      buildCloudSetupCommand({ ...input, sha256: "not-a-checksum" }),
    ).toThrow(/valid device agent daemon checksum/);
    expect(() => buildCloudSetupCommand({ ...input, email: "@" })).toThrow(
      /valid remote session identity email/,
    );
    expect(() =>
      buildCloudSetupCommand({ ...input, email: 'quo"te@example.test' }),
    ).toThrow(/valid remote session identity email/);
    expect(() =>
      buildCloudSetupCommand({ ...input, orgToken: 'tok"en' }),
    ).toThrow(/valid org token/);
    expect(() =>
      buildCloudSetupCommand({ ...input, email: "bell\x07@example.test" }),
    ).toThrow(/valid remote session identity email/);
    expect(() =>
      buildCloudSetupCommand({ ...input, orgToken: "tok\x01en" }),
    ).toThrow(/valid org token/);
  });
});

describe("buildCloudDefaultEnvironmentSnippet", () => {
  it("uses a placeholder environment id", () => {
    expect(JSON.parse(buildCloudDefaultEnvironmentSnippet())).toEqual({
      remote: { defaultEnvironmentId: "env_…" },
    });
  });
});
