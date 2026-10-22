import { describe, expect, it } from "vitest";
import {
  BUILTIN_RULES_BY_CATEGORY,
  detectionRulesTabCategories,
  detectionRulesTabRuleIds,
} from "./detection-rules-data";

describe("Detection Rules tab catalog", () => {
  it("leaves Shadow AI out; its targets live on the Shadow AI targets tab", () => {
    expect(detectionRulesTabCategories()).not.toContain("shadow_mcp");
    expect(detectionRulesTabRuleIds()).not.toContain("shadow_mcp.default");
  });

  it("lists every built-in rule of the categories it shows", () => {
    const expected = detectionRulesTabCategories().flatMap((category) =>
      BUILTIN_RULES_BY_CATEGORY[category].map((rule) => rule.id),
    );
    expect(detectionRulesTabRuleIds()).toEqual(expected);
    expect(detectionRulesTabRuleIds()).toContain("destructive_tool.default");
  });
});
