import { describe, expect, it } from "vitest";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { DETECTION_RULES, type DetectionRule } from "./policy-data";
import {
  getPolicyDeleteImpactText,
  getPolicyDeleteRuleActionLabel,
  getPolicyDeleteRuleListItems,
  getPolicyRuleGroupNamesForDeleteDialog,
} from "./policy-delete-dialog";

function visibleRuleIds(rules: DetectionRule[]): string[] {
  return rules.filter((rule) => !rule.hidden).map((rule) => rule.id);
}

function policy(overrides: Partial<RiskPolicy>): RiskPolicy {
  return {
    action: "block",
    audiencePrincipalUrns: ["user:all"],
    audienceType: "everyone",
    autoName: false,
    createdAt: new Date("2026-01-01T00:00:00Z"),
    enabled: true,
    id: "policy-1",
    name: "Sensitive Data",
    pendingMessages: 0,
    policyType: "standard",
    projectId: "project-1",
    sources: [],
    totalMessages: 0,
    updatedAt: new Date("2026-01-01T00:00:00Z"),
    version: 1,
    ...overrides,
  };
}

describe("getPolicyRuleGroupNamesForDeleteDialog", () => {
  it("returns readable detector groups for a standard policy", () => {
    const ruleGroups = getPolicyRuleGroupNamesForDeleteDialog(
      policy({
        customRuleIds: ["custom.password"],
        disabledRules: ["secret.1password_secret_key"],
        sources: ["gitleaks"],
      }),
    );

    expect(ruleGroups).toEqual(["Secrets", "Custom Rules"]);
    expect(ruleGroups).not.toContain("1Password Service Account Token");
    expect(ruleGroups).not.toContain("Password in Plain Text");
  });

  it("omits a category when every visible source rule is disabled", () => {
    expect(
      getPolicyRuleGroupNamesForDeleteDialog(
        policy({
          disabledRules: visibleRuleIds(DETECTION_RULES.secrets),
          sources: ["gitleaks"],
        }),
      ),
    ).toEqual([]);
  });

  it("keeps a category when at least one visible source rule is enabled", () => {
    const [, ...disabledRules] = visibleRuleIds(DETECTION_RULES.secrets);

    expect(
      getPolicyRuleGroupNamesForDeleteDialog(
        policy({
          disabledRules,
          sources: ["gitleaks"],
        }),
      ),
    ).toEqual(["Secrets"]);
  });

  it("returns each enabled Presidio-backed category", () => {
    expect(
      getPolicyRuleGroupNamesForDeleteDialog(
        policy({
          presidioEntities: ["EMAIL_ADDRESS", "CREDIT_CARD"],
          sources: ["presidio"],
        }),
      ),
    ).toEqual(["Financial Information", "Personal Identifiable Information"]);
  });

  it("returns all Presidio-backed categories when Presidio has no entity filter", () => {
    expect(
      getPolicyRuleGroupNamesForDeleteDialog(
        policy({
          sources: ["presidio"],
        }),
      ),
    ).toEqual([
      "Financial Information",
      "Personal Identifiable Information",
      "Government Identifiers",
      "Healthcare Information",
    ]);
  });

  it("returns no rule groups for prompt policies", () => {
    expect(
      getPolicyRuleGroupNamesForDeleteDialog(
        policy({
          policyType: "prompt_based",
          prompt: "Block unsafe support advice",
        }),
      ),
    ).toEqual([]);
  });
});

describe("getPolicyDeleteRuleActionLabel", () => {
  it("returns block for block policies", () => {
    expect(getPolicyDeleteRuleActionLabel(policy({ action: "block" }))).toBe(
      "block",
    );
  });

  it("returns flag for flag policies", () => {
    expect(getPolicyDeleteRuleActionLabel(policy({ action: "flag" }))).toBe(
      "flag",
    );
  });
});

describe("getPolicyDeleteImpactText", () => {
  it("uses the action-specific grouped-rule text when groups are present", () => {
    expect(getPolicyDeleteImpactText(policy({ action: "block" }), true)).toBe(
      "The following block rules will no longer be enforced.",
    );
  });

  it("uses action-specific fallback text when no groups are present", () => {
    expect(getPolicyDeleteImpactText(policy({ action: "flag" }), false)).toBe(
      "Any flag action this policy applies will stop immediately.",
    );
  });
});

describe("getPolicyDeleteRuleListItems", () => {
  it("shows up to four rules and appends an and-more item", () => {
    expect(
      getPolicyDeleteRuleListItems([
        "First Rule",
        "Second Rule",
        "Third Rule",
        "Fourth Rule",
        "Fifth Rule",
        "Sixth Rule",
      ]),
    ).toEqual([
      "First Rule",
      "Second Rule",
      "Third Rule",
      "Fourth Rule",
      "and 2 more",
    ]);
  });

  it("does not append an and-more item for four rules", () => {
    expect(
      getPolicyDeleteRuleListItems([
        "First Rule",
        "Second Rule",
        "Third Rule",
        "Fourth Rule",
      ]),
    ).toEqual(["First Rule", "Second Rule", "Third Rule", "Fourth Rule"]);
  });
});
