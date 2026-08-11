import { describe, expect, it } from "vitest";
import type { RuleCategory } from "./policy-data";
import {
  OTHER_BUILT_IN_RULE_DISABLED_REASON,
  SHADOW_MCP_DISABLED_REASON,
  builtInRuleDisabledReason,
} from "./policy-built-in-rule-exclusivity";

const selected = (...categories: RuleCategory[]) =>
  new Set<RuleCategory>(categories);

describe("builtInRuleDisabledReason", () => {
  it("allows every built-in rule when none is selected", () => {
    expect(builtInRuleDisabledReason("shadow_mcp", selected())).toBeUndefined();
    expect(builtInRuleDisabledReason("secrets", selected())).toBeUndefined();
  });

  it("disables Shadow MCP when another built-in rule is selected", () => {
    expect(
      builtInRuleDisabledReason("shadow_mcp", selected("secrets", "pii")),
    ).toBe(SHADOW_MCP_DISABLED_REASON);
  });

  it("disables other built-in rules when Shadow MCP is selected", () => {
    expect(builtInRuleDisabledReason("secrets", selected("shadow_mcp"))).toBe(
      OTHER_BUILT_IN_RULE_DISABLED_REASON,
    );
    expect(builtInRuleDisabledReason("pii", selected("shadow_mcp"))).toBe(
      OTHER_BUILT_IN_RULE_DISABLED_REASON,
    );
  });

  it("keeps non-Shadow MCP built-in rules combinable", () => {
    expect(
      builtInRuleDisabledReason("pii", selected("secrets")),
    ).toBeUndefined();
  });

  it("does not treat the custom category marker as another built-in rule", () => {
    expect(
      builtInRuleDisabledReason("shadow_mcp", selected("custom")),
    ).toBeUndefined();
  });

  it("keeps a selected rule enabled so it can always be turned off", () => {
    const conflicting = selected("shadow_mcp", "secrets");
    expect(
      builtInRuleDisabledReason("shadow_mcp", conflicting),
    ).toBeUndefined();
    expect(builtInRuleDisabledReason("secrets", conflicting)).toBeUndefined();
  });
});
