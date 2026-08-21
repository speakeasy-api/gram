import { describe, expect, it } from "vitest";
import type { RuleCategory } from "./policy-data";
import { categoriesToPayload } from "./policy-form";
import {
  categoriesSummaryForName,
  policySummary,
  promptSummaryForName,
  type PolicySummaryPolicy,
} from "./policy-summary";

describe("categoriesSummaryForName", () => {
  it("hides categories the auto-generated name already names", () => {
    expect(
      categoriesSummaryForName("Secrets Exposure Flagger", ["secrets"]),
    ).toBeNull();
    expect(
      categoriesSummaryForName("Prompt Injection Scanner", [
        "prompt_injection",
      ]),
    ).toBeNull();
    expect(
      categoriesSummaryForName("Non-Corporate Account Flagger", [
        "account_identity",
      ]),
    ).toBeNull();
    expect(
      categoriesSummaryForName("Shadow MCP Server Policy (Allow-All)", [
        "shadow_mcp",
      ]),
    ).toBeNull();
  });

  it("ignores filler words in the category label", () => {
    expect(
      categoriesSummaryForName("Healthcare Scanner", ["healthcare"]),
    ).toBeNull();
    expect(
      categoriesSummaryForName("Financial Blocker", ["financial"]),
    ).toBeNull();
  });

  it("accepts the abbreviation a name uses for a long label", () => {
    expect(categoriesSummaryForName("PII Scanner", ["pii"])).toBeNull();
    expect(
      categoriesSummaryForName("Government ID Blocker", ["government_ids"]),
    ).toBeNull();
  });

  it("shows the labels when the name leaves a category out", () => {
    expect(categoriesSummaryForName("Secret Blocker", ["secrets", "pii"])).toBe(
      "Secrets, Personal Identifiable Information",
    );
    expect(categoriesSummaryForName("Compliance Sweep", ["custom"])).toBe(
      "Custom Rules",
    );
  });

  it("shows nothing when the policy has no categories", () => {
    expect(categoriesSummaryForName("Secret Blocker", [])).toBeNull();
  });
});

describe("promptSummaryForName", () => {
  const prompt =
    "Flag only tool calls that perform irreversible destructive actions on production data.";

  it("hides a prompt the name is an excerpt of", () => {
    expect(
      promptSummaryForName("Flag only tool calls that perform", prompt),
    ).toBeNull();
    expect(promptSummaryForName(prompt, prompt)).toBeNull();
  });

  it("hides a prompt behind a name excerpt cut mid-word", () => {
    expect(
      promptSummaryForName(
        "Flag only tool calls that perform irreversible destructive a",
        prompt,
      ),
    ).toBeNull();
  });

  it("hides a prompt behind the server's derived name prefix", () => {
    expect(
      promptSummaryForName("Prompt Policy: Flag only tool calls", prompt),
    ).toBeNull();
  });

  it("shows the prompt next to a name that summarizes it", () => {
    expect(promptSummaryForName("Destructive Delete Guard", prompt)).toBe(
      prompt,
    );
  });

  it("keeps a prompt when only a one-word name partially matches it", () => {
    const anyPrompt = "Any tool call that writes to a production database.";
    expect(promptSummaryForName("A", anyPrompt)).toBe(anyPrompt);
  });

  it("shows the prompt when the policy has no name yet", () => {
    expect(promptSummaryForName("", prompt)).toBe(prompt);
  });

  it("collapses to nothing when there is no prompt", () => {
    expect(promptSummaryForName("Destructive Delete Guard", "   ")).toBeNull();
  });
});

function standardPolicy(
  name: string,
  categories: RuleCategory[],
  customRuleIds: string[] = [],
): PolicySummaryPolicy {
  const { sources, presidioEntities } = categoriesToPayload(
    new Set(categories),
    new Set(),
  );
  return {
    name,
    policyType: "standard",
    sources,
    presidioEntities,
    customRuleIds,
  };
}

describe("policySummary", () => {
  it("summarizes a standard policy by the categories its name omits", () => {
    expect(
      policySummary(standardPolicy("Secrets Exposure Flagger", ["secrets"])),
    ).toBeNull();
    expect(
      policySummary(standardPolicy("Secret Blocker", ["secrets", "pii"])),
    ).toEqual({
      kind: "categories",
      text: "Secrets, Personal Identifiable Information",
    });
  });

  it("counts attached custom rules as a category", () => {
    expect(
      policySummary(standardPolicy("Secret Blocker", ["secrets"], ["rule-1"])),
    ).toEqual({ kind: "categories", text: "Secrets, Custom Rules" });
  });

  it("summarizes a prompt policy by its guardrail, on one line", () => {
    expect(
      policySummary({
        name: "Destructive Delete Guard",
        policyType: "prompt_based",
        prompt: "Any tool call that performs\na destructive operation.",
        sources: [],
      }),
    ).toEqual({
      kind: "prompt",
      text: "Any tool call that performs a destructive operation.",
    });
  });

  it("drops the guardrail when the prompt policy name is an excerpt of it", () => {
    expect(
      policySummary({
        name: "Any tool call that performs",
        policyType: "prompt_based",
        prompt: "Any tool call that performs a destructive operation.",
        sources: [],
      }),
    ).toBeNull();
  });
});
