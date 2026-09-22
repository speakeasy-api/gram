import { describe, expect, it } from "vitest";
import {
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
    ["claude-chat-web", "chat"],
    ["cowork-desktop", "cowork"],
    ["cursor", "cursor"],
    ["codex-cli", "codex"],
    ["openai", "codex"],
    ["chatgpt-web", "codex"],
    ["gemini", "other"],
  ])("maps %s to %s", (source, expected) => {
    expect(surfaceForHookSource(source)).toBe(expected);
  });

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
