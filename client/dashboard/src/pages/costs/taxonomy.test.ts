import { Dimension } from "@gram/client/models/components/queryfilter.js";
import { describe, expect, it } from "vitest";
import { displayName, llmTokens } from "./taxonomy";

describe("displayName", () => {
  it("normalizes product surfaces in cost breakdowns", () => {
    expect(displayName(Dimension.HookSource, "claude")).toBe(
      "Claude Chat Desktop",
    );
    expect(displayName(Dimension.HookSource, "claude-code")).toBe(
      "Claude Code",
    );
    expect(displayName(Dimension.HookSource, "cowork")).toBe("Claude Cowork");
  });

  it("normalizes providers independently of product surfaces", () => {
    expect(displayName(Dimension.Provider, "anthropic")).toBe("Anthropic");
    expect(displayName(Dimension.Provider, "openai")).toBe("OpenAI");
  });

  it("keeps an empty dimension value visible as the unset bucket", () => {
    expect(displayName(Dimension.DepartmentName, "")).toBe("(unset)");
  });
});

describe("llmTokens", () => {
  it("shows input + output only, never the TUM cache-write population", () => {
    expect(llmTokens({ totalInputTokens: 100, totalOutputTokens: 50 })).toBe(
      150,
    );
    expect(llmTokens({})).toBe(0);
  });
});
