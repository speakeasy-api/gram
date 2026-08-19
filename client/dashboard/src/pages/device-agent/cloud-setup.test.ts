import { spawnSync } from "node:child_process";
import { describe, expect, it } from "vitest";

import {
  buildCloudAgentBootstrapScript,
  buildCloudAgentStartCommand,
  buildCloudAgentStartHook,
  buildCloudDefaultEnvironmentSnippet,
  buildCloudManagedConfig,
  buildCloudSetupScript,
  CLOUD_AGENT_BOOTSTRAP_PATH,
  CLOUD_ORG_TOKEN_SENTINEL,
  RELEASES_BASE,
} from "./cloud-setup";

const input = {
  version: "0.1.20",
  orgSlug: "acme-corp",
  orgName: "Acme Corporation",
  orgToken: "spk_org_test_token",
  email: "claude-code-web@acme.example",
};

function managedJsonFromScript(script: string): Record<string, unknown> {
  const match = script.match(
    /<<'SPEAKEASY_MANAGED_JSON'\n([\s\S]*?)\nSPEAKEASY_MANAGED_JSON/,
  );
  expect(match?.[1]).toBeDefined();
  return JSON.parse(match![1]!) as Record<string, unknown>;
}

describe("buildCloudManagedConfig", () => {
  it("writes the ephemeral-VM identity contract", () => {
    expect(buildCloudManagedConfig(input)).toEqual({
      v: 1,
      email: "claude-code-web@acme.example",
      org_token: "spk_org_test_token",
      org_slug: "acme-corp",
      org_name: "Acme Corporation",
      auto_update: "disabled",
      hide_ui: true,
    });
  });

  it("trims the required reporting identity", () => {
    expect(
      buildCloudManagedConfig({ ...input, email: "  ai@acme.example  " }),
    ).toMatchObject({ email: "ai@acme.example" });
  });

  it("rejects an empty reporting identity", () => {
    expect(() => buildCloudManagedConfig({ ...input, email: "  " })).toThrow(
      /valid remote session identity email is required/,
    );
  });

  it("rejects a malformed reporting identity", () => {
    expect(() => buildCloudManagedConfig({ ...input, email: "@" })).toThrow(
      /valid remote session identity email is required/,
    );
  });
});

describe("buildCloudSetupScript", () => {
  const script = buildCloudSetupScript(input);

  it("renders syntactically valid Bash for setup and per-session startup", () => {
    for (const candidate of [script, buildCloudAgentBootstrapScript()]) {
      const result = spawnSync("bash", ["-n"], {
        input: candidate,
        encoding: "utf8",
      });
      expect(result.stderr).toBe("");
      expect(result.status).toBe(0);
    }
  });

  it("pins linux_amd64 binaries and the helper .deb from the release bucket", () => {
    expect(script).toContain(`${RELEASES_BASE}/v0.1.20`);
    expect(script).toContain("speakeasy-helper_0.1.20_linux_amd64.deb");
    expect(script).toContain("apt-get install -y");
    expect(script).not.toContain("linux_arm64");
    expect(script).toContain("checksums.txt");
    expect(script).toContain("fetch_and_verify");
    expect(script).toContain("sha256sum -c -");
    expect(script).toContain(
      'fetch_and_verify "speakeasyd_0.1.20_linux_amd64"',
    );
    expect(script).toContain('fetch_and_verify "speakeasy_0.1.20_linux_amd64"');
    expect(script).toContain(
      'fetch_and_verify "speakeasy-helper_0.1.20_linux_amd64.deb"',
    );
  });

  it("writes managed.json using the agent's documented Linux permissions", () => {
    expect(script).toContain("chmod 0644 /etc/speakeasy/managed.json");
    expect(script).not.toContain("chgrp");
    expect(script).not.toContain("id -gn 1000");
  });

  it("writes managed.json and the bootstrap, but no Claude settings", () => {
    const json = managedJsonFromScript(script);
    expect(json).toEqual(buildCloudManagedConfig(input));
    expect(script).toContain("/etc/speakeasy/managed.json");
    expect(script).toContain(`cat >${CLOUD_AGENT_BOOTSTRAP_PATH}`);
    expect(script).toContain(`chmod 0755 ${CLOUD_AGENT_BOOTSTRAP_PATH}`);
    expect(script).not.toContain("settings.json");
    expect(script).not.toContain("managed-settings.json");
    expect(script).not.toContain("agenthooks");
    expect(script).not.toContain("-service start");
    expect(script).not.toContain(`\n${CLOUD_AGENT_BOOTSTRAP_PATH}\n`);
  });

  it("embeds a minted token sentinel unchanged so the UI can slot a button", () => {
    const withSentinel = buildCloudSetupScript({
      ...input,
      orgToken: CLOUD_ORG_TOKEN_SENTINEL,
    });
    expect(managedJsonFromScript(withSentinel).org_token).toBe(
      CLOUD_ORG_TOKEN_SENTINEL,
    );
  });

  it("refuses to interpolate a non-semver version into the root script", () => {
    expect(() =>
      buildCloudSetupScript({ ...input, version: "1.0.0; rm -rf /" }),
    ).toThrow(/refusing to interpolate agent version/);
  });
});

describe("buildCloudAgentStartHook", () => {
  const bootstrap = buildCloudAgentBootstrapScript();
  const command = buildCloudAgentStartCommand();
  const hook = buildCloudAgentStartHook();
  const parsed = JSON.parse(hook) as {
    hooks: {
      SessionStart: {
        matcher: string;
        hooks: { type: string; command: string; timeout: number }[];
      }[];
    };
  };

  it("keeps process management in the installed bootstrap", () => {
    expect(command).toBe(CLOUD_AGENT_BOOTSTRAP_PATH);
    expect(bootstrap).toContain("CLAUDE_CODE_REMOTE:-}");
    expect(bootstrap).toContain("/usr/local/bin/speakeasyd");
    expect(bootstrap).toContain("/usr/lib/speakeasy/speakeasy-helper");
    expect(bootstrap).toContain("grep -q 'pending:'");
    expect(bootstrap).toContain('"$CLI" sync');
    expect(bootstrap).toContain("policy synced");
    expect(bootstrap).not.toContain("agenthooks");
    expect(parsed.hooks.SessionStart[0]?.hooks[0]?.type).toBe("command");
    expect(parsed.hooks.SessionStart[0]?.hooks[0]?.command).toBe(command);
    expect(parsed.hooks.SessionStart[0]?.matcher).toBe("startup|resume");
    expect(parsed.hooks.SessionStart[0]?.hooks[0]?.timeout).toBe(45);
  });

  it("does not paste Speakeasy observability commands into settings.json", () => {
    expect(hook).not.toContain("agenthooks run");
    expect(hook).not.toContain("CLAUDE_PLUGIN_ROOT");
    expect(hook.toLowerCase()).not.toContain("observability");
  });
});

describe("buildCloudDefaultEnvironmentSnippet", () => {
  it("uses a placeholder env id rather than inventing one", () => {
    const snippet = buildCloudDefaultEnvironmentSnippet();
    expect(JSON.parse(snippet)).toEqual({
      remote: { defaultEnvironmentId: "env_…" },
    });
  });
});
