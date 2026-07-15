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
    expect(formatPlatform("cowork")).toBe("Claude Cowork");
    expect(formatPlatform("Claude Chat Web")).toBe("Claude Chat Web");
  });

  it("uses canonical labels for other known surfaces", () => {
    expect(formatPlatform("cursor")).toBe("Cursor");
    expect(formatPlatform("codex")).toBe("Codex");
    expect(formatPlatform("aws-bedrock")).toBe("AWS Bedrock");
  });

  it("title-cases unknown delimited sources", () => {
    expect(formatPlatform("new_agent-client")).toBe("New Agent Client");
  });
});
