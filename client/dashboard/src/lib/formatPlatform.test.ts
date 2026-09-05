import { describe, expect, it } from "vitest";
import { formatPlatform } from "./formatPlatform";

describe("formatPlatform", () => {
  it("normalizes legacy Claude Desktop sources", () => {
    expect(formatPlatform("claude")).toBe("Claude Chat Desktop");
    expect(formatPlatform("Claude Chat Desktop")).toBe("Claude Chat Desktop");
    expect(formatPlatform("claude-chat-desktop")).toBe("Claude Chat Desktop");
  });

  it("keeps Claude product surfaces distinct", () => {
    expect(formatPlatform("claude-code")).toBe("Claude Code");
    expect(formatPlatform("ClaudeCode")).toBe("Claude Code");
    expect(formatPlatform("claude-code-desktop")).toBe("Claude Code Desktop");
    expect(formatPlatform("cowork")).toBe("Claude Cowork");
    expect(formatPlatform("Claude Web")).toBe("Claude Chat Web");
    expect(formatPlatform("Claude Chat Web")).toBe("Claude Chat Web");
  });

  it("uses canonical labels for other known surfaces", () => {
    expect(formatPlatform("cursor")).toBe("Cursor");
    expect(formatPlatform("codex")).toBe("Codex");
    // Codex cloud tasks import under their own surface, kept separate from
    // the device-captured Codex agent.
    expect(formatPlatform("codex-web")).toBe("Codex Web");
    expect(formatPlatform("Codex Web")).toBe("Codex Web");
    expect(formatPlatform("CODEX_WEB")).toBe("Codex Web");
    expect(formatPlatform("chatgpt")).toBe("ChatGPT");
    expect(formatPlatform("ChatGPT")).toBe("ChatGPT");
    expect(formatPlatform("chatgpt-work")).toBe("ChatGPT Work");
    expect(formatPlatform("opencode")).toBe("opencode");
    expect(formatPlatform("litellm")).toBe("LiteLLM");
    expect(formatPlatform("copilot-cli")).toBe("GitHub Copilot CLI");
    expect(formatPlatform("vscode-copilot")).toBe("GitHub Copilot in VS Code");
    expect(formatPlatform("aws-bedrock")).toBe("AWS Bedrock");
  });

  it("title-cases unknown delimited sources", () => {
    expect(formatPlatform("new_agent-client")).toBe("New Agent Client");
  });
});
