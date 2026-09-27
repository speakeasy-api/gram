import { describe, expect, it } from "vitest";
import {
  humanizeRuleId,
  isLlmAnalyzerRuleId,
  llmAnalyzerRuleLabel,
  ruleIdCategoryLabel,
} from "./rule-ids";

const LLM_RULE_IDS = [
  "secret.llm",
  "pii.llm",
  "prompt_injection.llm",
  "destructive_tool.llm",
  "cli_destructive.llm",
  "llm_analyzer.dead_letter",
];

describe("LLM analyzer rule ids", () => {
  it.each([
    ["secret.llm", "Secret"],
    ["pii.llm", "PII"],
    ["prompt_injection.llm", "Prompt injection"],
    ["destructive_tool.llm", "Destructive tool"],
    ["cli_destructive.llm", "Destructive command"],
    ["llm_analyzer.dead_letter", "Analysis unavailable"],
  ])("humanizes %s as its category name", (ruleId, label) => {
    expect(humanizeRuleId(ruleId)).toBe(label);
    expect(llmAnalyzerRuleLabel(ruleId)).toBe(label);
    expect(isLlmAnalyzerRuleId(ruleId)).toBe(true);
  });

  it.each([
    ["secret.llm", "SECRET"],
    ["pii.llm", "PII"],
    ["prompt_injection.llm", "PROMPT_INJECTION"],
    ["destructive_tool.llm", "DESTRUCTIVE_TOOL"],
    ["cli_destructive.llm", "CLI_DESTRUCTIVE"],
    ["llm_analyzer.dead_letter", "ANALYSIS_UNAVAILABLE"],
  ])("badges %s without naming the engine", (ruleId, badge) => {
    expect(ruleIdCategoryLabel(ruleId)).toBe(badge);
  });

  it("names neither the model nor the analyzer in any label", () => {
    for (const ruleId of LLM_RULE_IDS) {
      for (const text of [
        humanizeRuleId(ruleId),
        ruleIdCategoryLabel(ruleId),
      ]) {
        expect(text.toLowerCase()).not.toMatch(/llm|model|analyzer/);
      }
    }
  });

  it("leaves every other rule id on the generic humanizer", () => {
    expect(isLlmAnalyzerRuleId("pii.us_ssn")).toBe(false);
    expect(llmAnalyzerRuleLabel("pii.us_ssn")).toBeUndefined();
    expect(humanizeRuleId("pii.us_ssn")).toBe("PII US SSN");
    expect(ruleIdCategoryLabel("pii.us_ssn")).toBe("PII");
    expect(isLlmAnalyzerRuleId(undefined)).toBe(false);
  });
});
