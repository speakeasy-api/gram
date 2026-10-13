import { describe, expect, it } from "vitest";
import {
  BUILTIN_RULES_BY_CATEGORY,
  DETECTION_RULES_TAB_CATEGORIES,
  DETECTION_RULES_TAB_RULE_IDS,
} from "./detection-rules-data";

describe("Detection Rules tab catalog", () => {
  it("does not list Shadow AI; its targets live on the Shadow AI targets tab", () => {
    expect(DETECTION_RULES_TAB_CATEGORIES).not.toContain("shadow_mcp");
    expect(DETECTION_RULES_TAB_RULE_IDS).not.toContain("shadow_mcp.default");
  });

  it("lists every built-in rule of the categories it shows", () => {
    const expected = DETECTION_RULES_TAB_CATEGORIES.flatMap((category) =>
      BUILTIN_RULES_BY_CATEGORY[category].map((rule) => rule.id),
    );
    expect(DETECTION_RULES_TAB_RULE_IDS).toEqual(expected);
    expect(DETECTION_RULES_TAB_RULE_IDS).toContain("destructive_tool.default");
  });
});
