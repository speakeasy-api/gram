import { describe, expect, it } from "vitest";
import {
  evidenceShowsRuleTitle,
  getCategoryCodeForFinding,
  getCategoryForFinding,
  hasOnlyRationaleSources,
  isJudgeSource,
  isLlmAnalyzerSource,
  isRationaleSource,
} from "./risk-utils";

describe("getCategoryForFinding for LLM analyzer findings", () => {
  // Mirrors the classification table in server/internal/risk/categories:
  // every rule id the analyzer writes lands in the same category the
  // scanner-backed detectors feed, and the dead-letter sentinel falls
  // through to custom.
  it.each([
    ["secret.llm", "secrets", "SECRETS"],
    ["pii.llm", "pii", "PII"],
    ["prompt_injection.llm", "prompt_injection", "PROMPT_INJECTION"],
    ["destructive_tool.llm", "destructive_tool", "DESTRUCTIVE_TOOL"],
    ["llm_analyzer.dead_letter", "custom", "CUSTOM"],
  ])("classifies %s by its rule id", (ruleId, category, code) => {
    expect(getCategoryForFinding("llm_analyzer", ruleId)).toBe(category);
    expect(getCategoryCodeForFinding("llm_analyzer", ruleId)).toBe(code);
  });

  it("never resolves the bare source to a category", () => {
    // Nothing classifies without a rule id: the analyzer's source carries no
    // category of its own, unlike the single-purpose scanners.
    expect(getCategoryForFinding("llm_analyzer")).toBeNull();
    expect(getCategoryCodeForFinding("llm_analyzer")).toBe("FLAGGED");
  });

  it("leaves the scanner-source fallbacks untouched", () => {
    expect(getCategoryForFinding("gitleaks", "generic-api-key")).toBe(
      "secrets",
    );
    expect(getCategoryForFinding("presidio", "pii.credit_card")).toBe(
      "financial",
    );
    expect(getCategoryForFinding("prompt_injection", "prompt_injection")).toBe(
      "prompt_injection",
    );
  });
});

describe("source predicates", () => {
  it("does not treat the LLM analyzer as a judge source, so its rule label renders", () => {
    expect(isLlmAnalyzerSource("llm_analyzer")).toBe(true);
    expect(isJudgeSource("llm_analyzer")).toBe(false);
  });

  it("routes judge and analyzer findings to the rationale evidence cell", () => {
    expect(isRationaleSource("llm_analyzer")).toBe(true);
    expect(isRationaleSource("llm_judge")).toBe(true);
    expect(isRationaleSource("prompt_injection")).toBe(true);
    expect(isRationaleSource("gitleaks")).toBe(false);
    expect(isRationaleSource(undefined)).toBe(false);
  });

  it("hides the reveal-all toggle only for signals backed solely by rationale sources", () => {
    expect(hasOnlyRationaleSources(["llm_analyzer"])).toBe(true);
    expect(hasOnlyRationaleSources(["llm_judge", "llm_analyzer"])).toBe(true);
    expect(hasOnlyRationaleSources(["gitleaks"])).toBe(false);
    expect(hasOnlyRationaleSources(["llm_analyzer", "presidio"])).toBe(false);
    expect(hasOnlyRationaleSources([])).toBe(false);
  });
});

describe("evidenceShowsRuleTitle", () => {
  it("names the rule for scanner findings", () => {
    expect(evidenceShowsRuleTitle("gitleaks", "secret.aws_access_token")).toBe(
      true,
    );
  });

  it("omits the rule for judge and analyzer category findings", () => {
    expect(evidenceShowsRuleTitle("llm_judge", "llm_judge")).toBe(false);
    expect(evidenceShowsRuleTitle("llm_analyzer", "secret.llm")).toBe(false);
    expect(evidenceShowsRuleTitle("llm_analyzer", "pii.llm")).toBe(false);
  });

  it("keeps the dead-letter sentinel's title, the only hint the analysis never ran", () => {
    expect(
      evidenceShowsRuleTitle("llm_analyzer", "llm_analyzer.dead_letter"),
    ).toBe(true);
  });
});
