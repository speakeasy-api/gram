import { describe, expect, it } from "vitest";
import { formatBlockProvider } from "./blockProvider";

describe("formatBlockProvider", () => {
  it("labels the bare claude hook slug as Claude Code", () => {
    expect(formatBlockProvider("claude")).toBe("Claude Code");
    expect(formatBlockProvider(" Claude ")).toBe("Claude Code");
  });

  it("keeps resolved Claude surfaces", () => {
    expect(formatBlockProvider("claude-code")).toBe("Claude Code");
    expect(formatBlockProvider("claude-code-desktop")).toBe(
      "Claude Code Desktop",
    );
    expect(formatBlockProvider("cowork")).toBe("Claude Cowork");
  });

  it("formats other providers with the shared labels", () => {
    expect(formatBlockProvider("cursor")).toBe("Cursor");
    expect(formatBlockProvider("openclaw")).toBe("OpenClaw");
  });
});
