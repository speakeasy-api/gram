import { describe, expect, it } from "vitest";

import {
  buildCloudAgentStartCommand,
  buildCloudAgentStartHook,
  buildCloudDefaultEnvironmentSnippet,
  buildCloudManagedConfig,
  buildCloudSetupScript,
  CLOUD_ORG_TOKEN_SENTINEL,
  RELEASES_BASE,
} from "./cloud-setup";

const input = {
  version: "0.1.20",
  orgSlug: "acme-corp",
  orgName: "Acme Corporation",
  orgToken: "spk_org_test_token",
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
      org_token: "spk_org_test_token",
      org_slug: "acme-corp",
      org_name: "Acme Corporation",
      auto_update: "disabled",
      hide_ui: true,
    });
  });

  it("includes email when provided and omits it otherwise", () => {
    expect(
      buildCloudManagedConfig({ ...input, email: "ai@acme.example" }),
    ).toMatchObject({ email: "ai@acme.example" });
    expect(
      buildCloudManagedConfig({ ...input, email: "  " }),
    ).not.toHaveProperty("email");
  });
});

describe("buildCloudSetupScript", () => {
  const script = buildCloudSetupScript(input);

  it("pins linux_amd64 binaries and the helper .deb from the release bucket", () => {
    expect(script).toContain(`${RELEASES_BASE}/v0.1.20`);
    expect(script).toContain("$BASE/speakeasyd_0.1.20_linux_amd64");
    expect(script).toContain("$BASE/speakeasy_0.1.20_linux_amd64");
    expect(script).toContain("speakeasy-helper_0.1.20_linux_amd64.deb");
    expect(script).toContain("apt-get install -y");
    expect(script).not.toContain("linux_arm64");
  });

  it("writes managed.json only — no Claude settings and no process start", () => {
    const json = managedJsonFromScript(script);
    expect(json).toEqual(buildCloudManagedConfig(input));
    expect(script).toContain("/etc/speakeasy/managed.json");
    expect(script).not.toContain("settings.json");
    expect(script).not.toContain("managed-settings.json");
    expect(script).not.toContain("agenthooks");
    expect(script).not.toMatch(/speakeasyd\s+>>/);
    expect(script).not.toContain("-service start");
    expect(script).not.toContain("speakeasy-helper >>");
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
  const command = buildCloudAgentStartCommand();
  const hook = buildCloudAgentStartHook();
  const parsed = JSON.parse(hook) as {
    hooks: { SessionStart: { hooks: { type: string; command: string }[] }[] };
  };

  it("gates on CLAUDE_CODE_REMOTE and only starts the agent", () => {
    expect(command).toContain("CLAUDE_CODE_REMOTE:-}");
    expect(command).toContain('= "true"');
    expect(command).toContain("speakeasyd >>");
    expect(command).toContain("speakeasy-helper");
    expect(command).not.toContain("agenthooks");
    expect(command).not.toContain("managed.json");
    expect(parsed.hooks.SessionStart[0]?.hooks[0]?.type).toBe("command");
    expect(parsed.hooks.SessionStart[0]?.hooks[0]?.command).toBe(command);
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
