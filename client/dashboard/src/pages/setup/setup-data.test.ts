import { describe, expect, it } from "vitest";
import { ACTIVE_AGENT_PROVIDER_IDS } from "@/components/agent-providers/agent-providers";
import { AGENT_PLATFORMS } from "./setup-data";

describe("AGENT_PLATFORMS", () => {
  it("does not offer OpenClaw as a setup platform", () => {
    // The other-platforms card is the device agent's rollout and the agent
    // does not cover OpenClaw, so listing it offered a walkthrough nothing
    // behind it could deliver.
    expect(AGENT_PLATFORMS.find(({ id }) => id === "openclaw")).toBeUndefined();
  });

  it("follows the shared setup provider order", () => {
    expect(
      AGENT_PLATFORMS.slice(0, ACTIVE_AGENT_PROVIDER_IDS.setup.length).map(
        ({ id }) => id,
      ),
    ).toEqual([...ACTIVE_AGENT_PROVIDER_IDS.setup]);
  });

  it("enables Claude OpenTelemetry traces", () => {
    const managedSettings = AGENT_PLATFORMS.find(
      ({ id }) => id === "claude",
    )?.setupSteps.find(({ code }) =>
      code?.includes("CLAUDE_CODE_ENABLE_TELEMETRY"),
    )?.code;

    expect(managedSettings).toBeDefined();
    const settings = JSON.parse(managedSettings ?? "{}") as {
      env: Record<string, string>;
    };
    expect(settings.env).toMatchObject({
      CLAUDE_CODE_ENABLE_TELEMETRY: "1",
      CLAUDE_CODE_ENHANCED_TELEMETRY_BETA: "1",
      OTEL_EXPORTER_OTLP_ENDPOINT: "https://app.getgram.ai/otel",
      OTEL_EXPORTER_OTLP_HEADERS:
        "Gram-Project={{GRAM_PROJECT_SLUG}},Gram-Key={{GRAM_API_KEY}}",
      OTEL_EXPORTER_OTLP_PROTOCOL: "http/protobuf",
      OTEL_LOGS_EXPORTER: "otlp",
      OTEL_METRICS_EXPORTER: "otlp",
      OTEL_TRACES_EXPORTER: "otlp",
    });
  });

  it("ends Cowork setup with authenticated OTEL export instructions", () => {
    const cowork = AGENT_PLATFORMS.find(({ id }) => id === "claude-cowork");
    const step = cowork?.setupSteps.at(-1);

    expect(step).toMatchObject({
      title: "Enable OTEL export",
      description:
        "In the Cowork tab of the Claude org settings, scroll to Monitoring and enter the values below. Save the settings.",
      code: `OTLP endpoint: https://app.getgram.ai/rpc/hooks.otel
OTLP protocol: http/json
OTLP headers: Gram-Project=default,Gram-Key={{GRAM_API_KEY}}`,
      language: "text",
      requiresApiKey: true,
    });
  });

  it("delegates Codex telemetry configuration to the device agent", () => {
    const codex = AGENT_PLATFORMS.find(({ id }) => id === "codex");

    expect(codex?.setupSteps).toHaveLength(1);
    const step = codex?.setupSteps[0];
    expect(step?.title).toBe("Deploy the Speakeasy device agent via MDM");
    expect(step).not.toHaveProperty("code");
    expect(step).not.toHaveProperty("requiresApiKey");
    expect(step?.description).toContain(
      "OpenTelemetry logs, metrics, and traces",
    );
  });
});
