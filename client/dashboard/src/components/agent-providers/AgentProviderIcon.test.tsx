import { render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ACTIVE_AGENT_PROVIDER_IDS, AGENT_PROVIDERS } from "./agent-providers";
import { agentProviderIconKind } from "./agent-provider-icon-kind";
import { AgentProviderIcon, CopilotIcon } from "./AgentProviderIcon";

afterEach(() => {
  vi.restoreAllMocks();
});

describe("CopilotIcon", () => {
  it("renders without invalid SVG property warnings", () => {
    const consoleError = vi
      .spyOn(console, "error")
      .mockImplementation(() => undefined);

    render(<CopilotIcon />);

    expect(consoleError).not.toHaveBeenCalled();
  });
});

describe("AgentProviderIcon", () => {
  it("keeps Claude Code available for plugins", () => {
    expect(ACTIVE_AGENT_PROVIDER_IDS.plugins).toEqual([
      "claude",
      "claude-cowork",
      "cursor",
      "codex",
      "opencode",
      "openclaw",
      "copilot",
    ]);
  });

  it("includes LiteLLM in the shared provider catalog", () => {
    expect(AGENT_PROVIDERS.litellm).toMatchObject({
      name: "LiteLLM",
      iconSource: "litellm",
    });
  });

  it.each([
    ["anthropic", "claude"],
    ["claude", "claude"],
    ["claude-chat-desktop", "claude"],
    ["claude-code-desktop", "claude"],
    ["cowork", "claude"],
    ["openai", "codex"],
    ["codex-web", "codex"],
    ["chatgpt-work", "codex"],
    ["google", "gemini"],
    ["gemini", "gemini"],
    ["aws", "bedrock"],
    ["aws-bedrock", "bedrock"],
    ["github-copilot", "copilot"],
    ["microsoft", "copilot"],
    ["opencode", "opencode"],
    ["LiteLLM", "litellm"],
    ["devin", "devin"],
    ["mistral", "mistral"],
    ["glean", "glean"],
  ])("maps %s to the %s icon", (source, expected) => {
    expect(agentProviderIconKind(source)).toBe(expected);
  });

  it("has an icon mapping for every catalog provider", () => {
    for (const provider of Object.values(AGENT_PROVIDERS)) {
      expect(agentProviderIconKind(provider.iconSource)).not.toBe("unknown");
    }
  });

  it("renders the LiteLLM logo", () => {
    const { container } = render(<AgentProviderIcon source="litellm" />);

    expect(
      container.querySelector('img[src="/icons/platforms/litellm.png"]'),
    ).toBeTruthy();
  });

  it("renders the mapped provider icon and globe fallback", () => {
    const { container, rerender } = render(
      <AgentProviderIcon source="aws-bedrock" />,
    );

    expect(container.querySelector("svg")).toBeTruthy();

    rerender(<AgentProviderIcon source="unknown-provider" />);
    expect(container.querySelector("svg")).toBeTruthy();
  });
});
