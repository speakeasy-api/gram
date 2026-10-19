import { getCursorInstallCommand } from "@/lib/cursor-install-command";
import { describe, expect, it } from "vitest";
import { ACTIVE_AGENT_PROVIDER_IDS } from "@/components/agent-providers/agent-providers";
import { getAgentPlatforms, platformSteps } from "./setup-data";

const AGENT_PLATFORMS = getAgentPlatforms("https://app.getgram.ai");

describe("getAgentPlatforms", () => {
  it("does not offer OpenClaw as a setup platform", () => {
    // The other-platforms card is the device agent's rollout and the agent
    // does not cover OpenClaw, so listing it offered a walkthrough nothing
    // behind it could deliver.
    expect(AGENT_PLATFORMS.find(({ id }) => id === "openclaw")).toBeUndefined();
  });

  it("offers Copilot CLI with a working plugin download walkthrough", () => {
    const copilot = AGENT_PLATFORMS.find(({ id }) => id === "copilot");
    expect(copilot?.name).toBe("GitHub Copilot CLI");
    expect(copilot?.available).toBe(true);
    expect(copilot?.setupSteps[0]?.download?.platform).toBe("copilot");
    expect(copilot?.setupSteps[0]?.description).toContain(
      "Coordinate rotation and replacement; a new download does not revoke keys in older copies.",
    );
    expect(copilot?.setupSteps[1]?.code).toContain(
      "copilot --plugin-dir speakeasy-hooks",
    );
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
        "Speakeasy-AI-Project={{GRAM_PROJECT_SLUG}},Speakeasy-AI-Key={{GRAM_API_KEY}}",
      OTEL_EXPORTER_OTLP_PROTOCOL: "http/protobuf",
      OTEL_LOGS_EXPORTER: "otlp",
      OTEL_METRICS_EXPORTER: "otlp",
      OTEL_TRACES_EXPORTER: "otlp",
    });
  });

  it("separates Cowork hook networking from native telemetry export", () => {
    const steps = AGENT_PLATFORMS.find(
      ({ id }) => id === "claude-cowork",
    )!.setupSteps;
    const index = steps.findIndex(
      ({ title }) => title === "Verify hook and bootstrap network access",
    );
    expect(index).toBeGreaterThanOrEqual(0);
    expect(index).toBeGreaterThan(
      steps.findIndex(({ title }) => title === "Enable OTEL export"),
    );
    expect(index).toBe(steps.length - 1);
    const step = steps[index]!;
    expect(step.description).toContain(
      "Admin settings → Capabilities → Network egress",
    );
    expect(step.description).toContain("Native OTEL automatically allowlists");
    expect(step.description).toContain(
      "bootstrap binary downloads are separate",
    );
    expect(step.fields).toEqual([
      { label: "Additional allowed domains", value: "app.getgram.ai" },
      { label: "Additional allowed domains", value: "ai.speakeasy.com" },
    ]);
    expect(step.afterFields).toContain("Start a new Cowork session");
    expect(step.afterFields).toContain(
      "confirm hook events and native monitoring separately",
    );
  });

  it("includes authenticated OTEL export instructions", () => {
    const cowork = AGENT_PLATFORMS.find(({ id }) => id === "claude-cowork");
    const step = cowork?.setupSteps.find(
      ({ title }) => title === "Enable OTEL export",
    );

    expect(step).toMatchObject({
      title: "Enable OTEL export",
      description: expect.stringContaining(
        "Admin settings → Cowork → Monitoring",
      ),
      fields: [
        {
          label: "OTLP endpoint",
          value: "https://app.getgram.ai/rpc/hooks.otel",
        },
        { label: "OTLP protocol", value: "http/json" },
        {
          label: "OTLP headers",
          value:
            "Speakeasy-AI-Project=default,Speakeasy-AI-Key={{GRAM_API_KEY}}",
          requiresApiKey: true,
        },
      ],
      afterFields: expect.stringContaining("automatically allowlists"),
      requiresApiKey: true,
    });
  });

  it("places the dynamic Cowork plugin identifier inline instead of a copy block", () => {
    const step = AGENT_PLATFORMS.find(
      ({ id }) => id === "claude-cowork",
    )?.setupSteps.find(
      ({ title }) => title === "Mark the observability plugin as Required",
    );
    expect(step?.description).toContainEqual({
      code: "{{GRAM_CLAUDE_PLUGIN_NAME}}",
      fallback: "the observability plugin",
    });
    expect(step).not.toHaveProperty("code");
    expect(step).not.toHaveProperty("language");
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

  it("qualifies personal Cowork eligibility, alternatives, and credential distribution", () => {
    const platform = AGENT_PLATFORMS.find(({ id }) => id === "claude-cowork")!;
    const personal = JSON.stringify(platformSteps(platform, false));
    expect(personal).toContain("Pro or Max");
    expect(personal).toContain("private GitHub repository");
    expect(personal).toContain("Customize → Plugins → Add → Upload plugin");
    expect(personal).toContain("download a fresh ZIP");
    expect(personal).toContain(
      "A new download does not revoke keys in older copies",
    );
    expect(personal).toContain("where hooks are ignored");
    expect(personal).not.toContain("can't sync");
  });

  it("distinguishes Claude Code server policy from endpoint management and verifies beta telemetry", () => {
    const platform = AGENT_PLATFORMS.find(({ id }) => id === "claude")!;
    const copy = JSON.stringify(platform.setupSteps);
    expect(copy).toContain("Owner or Primary Owner");
    expect(copy).toContain(
      "Endpoint-managed policy files and MDM are separate",
    );
    expect(copy).toContain("eligible signed-in sessions");
    expect(copy).toContain("ANTHROPIC_BASE_URL");
    expect(copy).toContain("Higher-precedence policy");
    expect(copy).toContain("beta trace spans");
    expect(copy).toContain("claude_code.session.count");
    expect(copy).not.toContain("every install");
  });

  it("tells admins to key Claude Code settings by the marketplace name", () => {
    const platform = AGENT_PLATFORMS.find(({ id }) => id === "claude")!;
    const managed = platform.setupSteps.find(
      ({ title }) => title === "Update Managed settings on Claude.ai",
    );
    const personal = platformSteps(platform, false).find(({ code }) =>
      code?.includes("extraKnownMarketplaces"),
    );
    for (const step of [managed, personal]) {
      expect(step?.code).toContain('"{{GRAM_MARKETPLACE_NAME}}": {');
      expect(step?.code).toContain('"autoUpdate": true');
      expect(step?.code).toContain('"FORCE_AUTOUPDATE_PLUGINS": "1"');
      expect(step?.code).toContain(
        '"{{GRAM_CLAUDE_PLUGIN_NAME}}@{{GRAM_MARKETPLACE_NAME}}": true',
      );
      expect(JSON.stringify(step?.description)).toContain(
        "Use this exact marketplace name.",
      );
    }
    expect(managed?.helpLink?.url).toBe(
      "https://code.claude.com/docs/en/plugins/org#require-a-marketplace-and-its-plugins",
    );
  });

  it("uses the staged Cursor installer with required hook validation", () => {
    const platform = AGENT_PLATFORMS.find(({ id }) => id === "cursor")!;
    const step = platformSteps(platform, false).find(({ code }) =>
      code?.includes("GRAM_CURSOR_INSTALL"),
    );
    expect(step?.code).toBe(
      getCursorInstallCommand({
        pluginName: "{{GRAM_CURSOR_PLUGIN_NAME}}",
        archiveName: "observability-cursor.zip",
        requireHooks: true,
      }),
    );
    expect(step?.description).toContain("Bash with unzip and Python 3");
    expect(step?.code).toContain(
      "Resolve the Cursor plugin name before installing.",
    );
  });

  it("uses Cursor admin controls, installation modes, and fresh local updates", () => {
    const platform = AGENT_PLATFORMS.find(({ id }) => id === "cursor")!;
    const copy = JSON.stringify(platform.setupSteps);
    expect(copy).toContain(
      "Dashboard → Settings → Security & Identity → Marketplace and Plugins",
    );
    expect(copy).toContain("same name takes precedence");
    expect(copy).toContain(
      "Team Marketplaces → Add Marketplace → Import from Repo",
    );
    expect(copy).toContain("Default On");
    expect(copy).toContain("Default Off");
    expect(copy).toContain(
      "Enable Auto Refresh requires the Cursor GitHub App",
    );
    expect(copy).toContain("download a fresh ZIP");
    expect(copy).toContain("not a native PowerShell command");
    expect(copy).toContain(
      "A new download does not revoke keys in older copies",
    );
  });

  it.each(["claude", "claude-cowork", "cursor"])(
    "gives %s a per-user path for personal plans instead of the org rollout",
    (id) => {
      const platform = AGENT_PLATFORMS.find((p) => p.id === id)!;
      const [gate, ...orgSteps] = platform.setupSteps;

      expect(platformSteps(platform, null)).toEqual(platform.setupSteps);
      expect(platformSteps(platform, true)).toEqual(platform.setupSteps);
      const personal = platformSteps(platform, false);
      expect(personal[0]).toBe(gate);
      expect(personal.slice(1)).toEqual(gate!.eligibility!.personalSteps);
      expect(personal.slice(1).length).toBeGreaterThan(0);
      for (const step of orgSteps) expect(personal).not.toContain(step);
    },
  );

  it("names the host the reader is on in every copy-paste setup value", () => {
    const platforms = getAgentPlatforms("https://ai.speakeasy.com");

    const claudeSettings = JSON.parse(
      platforms
        .find(({ id }) => id === "claude")!
        .setupSteps.find(({ code }) =>
          code?.includes("OTEL_EXPORTER_OTLP_ENDPOINT"),
        )!.code!,
    ) as { env: Record<string, string> };
    expect(claudeSettings.env.OTEL_EXPORTER_OTLP_ENDPOINT).toBe(
      "https://ai.speakeasy.com/otel",
    );

    const cowork = platforms.find(({ id }) => id === "claude-cowork")!;
    const fieldValue = (title: string, label: string) =>
      cowork.setupSteps
        .find((step) => step.title === title)
        ?.fields?.find((field) => field.label === label)?.value;
    expect(fieldValue("Enable OTEL export", "OTLP endpoint")).toBe(
      "https://ai.speakeasy.com/rpc/hooks.otel",
    );
    // The allowlist takes bare hostnames. It lists every prod host, because
    // published plugin hooks send to whichever one the server URL named at
    // their last publish.
    expect(
      cowork.setupSteps
        .find(
          (step) => step.title === "Verify hook and bootstrap network access",
        )
        ?.fields?.map((field) => field.value),
    ).toEqual(["ai.speakeasy.com", "app.getgram.ai"]);
  });
});
