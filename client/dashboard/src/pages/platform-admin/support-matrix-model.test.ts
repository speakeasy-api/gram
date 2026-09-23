import { describe, expect, it } from "vitest";
import {
  activeAgentCoverageLabel,
  capabilities,
  methods,
  surfaceForHookSource,
  surfaces,
} from "./support-matrix-model";

describe("support matrix model", () => {
  it.each([
    ["claude-code", "cc"],
    ["claude-code-web", "cc"],
    ["claudecode", "cc"],
    ["claude", "chat"],
    ["claude-desktop", "chat"],
    ["claude-chat", "chat"],
    ["claude-chat-web", "chat"],
    ["claude-web", "chat"],
    ["cowork-desktop", "cowork"],
    ["cursor", "cursor"],
    ["cursor-app", "cursor"],
    ["codex-cli", "codex"],
    ["chatgpt", "codex"],
    ["chatgpt-work", "codex"],
    ["gemini", "other"],
    ["opencode", "other"],
    ["github_copilot", "other"],
  ])("maps %s to %s", (source, expected) => {
    expect(surfaceForHookSource(source)).toBe(expected);
  });

  it.each([
    "gram",
    "openai",
    "chatgpt-web",
    "future-agent",
    "cursor-internal",
    "codex-experimental",
  ])("does not classify unknown source %s", (source) => {
    expect(surfaceForHookSource(source)).toBeNull();
  });

  it.each([
    ["device", 60, "devices running the agent within 60 minutes"],
    [
      "user",
      60,
      "devices whose assigned user has an active agent within 60 minutes",
    ],
    ["device", undefined, "devices running the agent"],
    [undefined, undefined, "devices whose assigned user has an active agent"],
  ] as const)(
    "formats %s attestation coverage",
    (attestation, minutes, expected) => {
      expect(activeAgentCoverageLabel(attestation, minutes)).toBe(expected);
    },
  );

  it("defines integration footprints using known surfaces and capabilities", () => {
    const surfaceIds = new Set(surfaces.map((surface) => surface.id));
    const capabilityIds = new Set(
      capabilities.map((capability) => capability.id),
    );

    for (const method of methods) {
      expect(method.surfaces.every((surface) => surfaceIds.has(surface))).toBe(
        true,
      );
      expect(
        method.capabilities.every((capability) =>
          capabilityIds.has(capability),
        ),
      ).toBe(true);
    }
  });
});
