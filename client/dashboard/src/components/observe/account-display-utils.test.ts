import { describe, expect, it } from "vitest";
import { providerLabel } from "./account-display-utils";

describe("providerLabel", () => {
  it("uses provider names rather than product-surface names", () => {
    expect(providerLabel("anthropic")).toBe("Anthropic");
    expect(providerLabel("openai")).toBe("OpenAI");
    expect(providerLabel("cursor")).toBe("Cursor");
  });

  it("matches known providers case-insensitively", () => {
    expect(providerLabel("ANTHROPIC")).toBe("Anthropic");
  });

  it("falls back safely for unknown and missing providers", () => {
    expect(providerLabel("other")).toBe("Other");
    expect(providerLabel("")).toBe("Unknown");
  });
});
