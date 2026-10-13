import { describe, expect, it } from "vitest";
import {
  BUILTIN_RULES_BY_CATEGORY,
  detectionRulesTabCategories,
  detectionRulesTabRuleIds,
} from "./detection-rules-data";

describe("Detection Rules tab catalog", () => {
  it("drops Shadow AI once the fold-in is on; its targets live on the Shadow AI targets tab", () => {
    expect(detectionRulesTabCategories(true)).not.toContain("shadow_mcp");
    expect(detectionRulesTabRuleIds(true)).not.toContain("shadow_mcp.default");
  });

  it("keeps the Shadow AI detector listed while the fold-in is off", () => {
    expect(detectionRulesTabCategories(false)).toContain("shadow_mcp");
    expect(detectionRulesTabRuleIds(false)).toContain("shadow_mcp.default");
  });

  it("lists every built-in rule of the categories it shows", () => {
    for (const folded of [true, false]) {
      const expected = detectionRulesTabCategories(folded).flatMap((category) =>
        BUILTIN_RULES_BY_CATEGORY[category].map((rule) => rule.id),
      );
      expect(detectionRulesTabRuleIds(folded)).toEqual(expected);
      expect(detectionRulesTabRuleIds(folded)).toContain(
        "destructive_tool.default",
      );
    }
  });
});
