import { describe, expect, it } from "vitest";
import { platformSteps } from "@/pages/setup/setup-data";
import { organizationGatewayInstallPlatforms } from "./organization-gateway-install";

const URL = "https://app.example.com/mcp/gateway/acme";

describe("organizationGatewayInstallPlatforms", () => {
  const platforms = organizationGatewayInstallPlatforms("acme", URL);
  const byId = (id: string) => platforms.find((p) => p.id === id)!;

  it("follows the setup flow's agent order, then adds ChatGPT and others", () => {
    expect(platforms.map((p) => p.id)).toEqual([
      "claude",
      "claude-cowork",
      "codex",
      "cursor",
      "opencode",
      "chatgpt",
      "other",
    ]);
  });

  it("never asks for an API key", () => {
    for (const platform of platforms) {
      for (const step of [
        ...platform.setupSteps,
        ...(platform.setupSteps[0]?.eligibility?.personalSteps ?? []),
      ]) {
        expect(step.requiresApiKey).toBeFalsy();
      }
    }
  });

  it("splits Claude into an org rollout and a personal path", () => {
    const cowork = byId("claude-cowork");
    expect(platformSteps(cowork, true).map((s) => s.title)).toEqual([
      "Plan check",
      "Add the connector for your organization",
      "Each person connects",
    ]);
    expect(platformSteps(cowork, false).map((s) => s.title)).toEqual([
      "Plan check",
      "Add a custom connector",
    ]);
  });

  it("puts the endpoint in every step that hands it over", () => {
    const allSteps = platforms.flatMap((p) => [
      ...p.setupSteps,
      ...(p.setupSteps[0]?.eligibility?.personalSteps ?? []),
    ]);
    const fields = allSteps.flatMap((s) => s.fields ?? []);
    expect(fields.length).toBeGreaterThan(0);
    for (const field of fields) expect(field.value).toBe(URL);

    // Commands and config carry it too; only the Codex login names the
    // server instead of its URL.
    const snippets = allSteps
      .map((s) => s.code)
      .filter((code): code is string => !!code && !code.includes("login"));
    expect(snippets.length).toBe(3);
    for (const code of snippets) expect(code).toContain(URL);

    const cursorLink = new globalThis.URL(
      byId("cursor").setupSteps[0]!.helpLink!.url,
    );
    expect(JSON.parse(atob(cursorLink.searchParams.get("config")!))).toEqual({
      url: URL,
    });
  });

  it("signs Codex in after adding it", () => {
    expect(byId("codex").setupSteps.map((s) => s.code)).toEqual([
      `codex mcp add 'acme' \\\n  --url '${URL}'`,
      "codex mcp login 'acme'",
    ]);
  });
});
