import { describe, expect, it } from "vitest";
import type { RuleCategory } from "./policy-data";
import {
  ALL_CATEGORIES,
  AVAILABLE_CATEGORIES,
  CATEGORY_LEVEL_DETECTORS,
  PRESIDIO_CATEGORIES,
  allCategories,
  availableCategories,
  categoriesToPayload,
  categoryLevelDetectors,
  policyDetectionCategories,
  policyToCategories,
} from "./policy-form";

describe("policy category availability", () => {
  it("keeps off-policy visible but unavailable", () => {
    expect(ALL_CATEGORIES).toContain("off_policy");
    expect(AVAILABLE_CATEGORIES.has("off_policy")).toBe(false);
    expect(PRESIDIO_CATEGORIES).not.toContain("off_policy");
    expect(ALL_CATEGORIES.indexOf("off_policy")).toBe(
      ALL_CATEGORIES.indexOf("healthcare") + 1,
    );
  });
});

describe("llm detector mode", () => {
  it("collapses the personal-data categories into a single PII detector", () => {
    expect(allCategories("llm")).toEqual([
      "secrets",
      "pii",
      "shadow_mcp",
      "destructive_tool",
      "cli_destructive",
      "account_identity",
      "prompt_injection",
    ]);
    expect(allCategories("llm")).not.toContain("off_policy");
    for (const category of ["financial", "government_ids", "healthcare"]) {
      expect(availableCategories("llm").has(category as RuleCategory)).toBe(
        false,
      );
    }
    expect(availableCategories("llm").has("pii")).toBe(true);
    expect(categoryLevelDetectors("llm").has("pii")).toBe(true);
    expect(categoryLevelDetectors("presidio").has("pii")).toBe(false);
  });

  it("keeps the presidio mode as the default", () => {
    expect(allCategories()).toEqual(ALL_CATEGORIES);
    expect(availableCategories()).toBe(AVAILABLE_CATEGORIES);
    expect(categoryLevelDetectors()).toBe(CATEGORY_LEVEL_DETECTORS);
  });

  it("emits an entity-less presidio source when PII is on", () => {
    expect(
      categoriesToPayload(
        new Set<RuleCategory>(["secrets", "pii"]),
        new Set(),
        new Set(),
        "llm",
      ),
    ).toEqual({
      sources: ["gitleaks", "presidio"],
      presidioEntities: [],
      promptInjectionRules: [],
      disabledRules: [],
    });
    expect(
      categoriesToPayload(
        new Set<RuleCategory>(["secrets"]),
        new Set(),
        new Set(),
        "llm",
      ).sources,
    ).not.toContain("presidio");
  });

  it("keeps stored personal-data rule overrides across an edit", () => {
    expect(
      categoriesToPayload(
        new Set<RuleCategory>(["pii"]),
        new Set([
          "pii.credit_card",
          "pii.email_address",
          "secret.aws_access_token",
        ]),
        new Set(),
        "llm",
      ).disabledRules.sort(),
    ).toEqual(["pii.credit_card", "pii.email_address"]);
  });

  it("reads any presidio policy as PII, entities or not", () => {
    expect(policyToCategories(["presidio"], [], "llm")).toEqual(
      new Set(["pii"]),
    );
    expect(
      policyToCategories(["gitleaks", "presidio"], ["CREDIT_CARD"], "llm"),
    ).toEqual(new Set(["secrets", "pii"]));
    expect(policyToCategories(["gitleaks"], [], "llm")).toEqual(
      new Set(["secrets"]),
    );
  });

  it("round-trips a PII policy through the payload", () => {
    const selected = new Set<RuleCategory>(["pii", "prompt_injection"]);
    const payload = categoriesToPayload(selected, new Set(), new Set(), "llm");
    expect(
      policyToCategories(payload.sources, payload.presidioEntities, "llm"),
    ).toEqual(selected);
  });

  it("resolves detection categories to PII without off-policy", () => {
    expect(
      policyDetectionCategories(
        { sources: ["presidio"], presidioEntities: [] },
        "llm",
      ),
    ).toEqual(new Set(["pii"]));
    expect(
      policyDetectionCategories(
        {
          sources: ["presidio"],
          presidioEntities: ["US_SSN"],
          customRuleIds: ["custom.x"],
        },
        "llm",
      ),
    ).toEqual(new Set(["pii", "custom"]));
  });

  it("still expands an entity-less presidio policy under the presidio mode", () => {
    expect(
      policyDetectionCategories({
        sources: ["presidio"],
        presidioEntities: [],
      }),
    ).toEqual(
      new Set([
        "financial",
        "pii",
        "government_ids",
        "healthcare",
        "off_policy",
      ]),
    );
  });
});
