import { describe, expect, it } from "vitest";
import { platformSteps } from "@/pages/setup/setup-data";
import { teamGatewayInstallPlatforms } from "./team-gateway-install";

const URL = "https://app.example.com/mcp/gateway/acme";

describe("teamGatewayInstallPlatforms", () => {
  const platforms = teamGatewayInstallPlatforms("acme", URL);
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

  it("puts the endpoint in every step that pastes it", () => {
    const pasted = platforms.flatMap((p) =>
      [
        ...p.setupSteps,
        ...(p.setupSteps[0]?.eligibility?.personalSteps ?? []),
      ].flatMap((s) => s.fields ?? []),
    );
    expect(pasted.length).toBeGreaterThan(0);
    for (const field of pasted) expect(field.value).toBe(URL);
  });

  it("signs Codex in after adding it", () => {
    expect(byId("codex").setupSteps.map((s) => s.code)).toEqual([
      `codex mcp add 'acme' \\\n  --url '${URL}'`,
      "codex mcp login 'acme'",
    ]);
  });
});
