import { describe, expect, it } from "vitest";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { getMatchStrings, matchShownInDescription } from "./chatHelpers";

// Minimal RiskResult factory — only the fields the match-display helpers read
// matter; the rest satisfy the required shape.
function result(
  source: string,
  match: string | undefined,
  extra: Partial<RiskResult> = {},
): RiskResult {
  return {
    id: `${source}:${match ?? ""}`,
    chatMessageId: "m1",
    policyId: "p1",
    policyVersion: 1,
    createdAt: new Date("2026-07-06T00:00:00Z"),
    source,
    match,
    ...extra,
  };
}

describe("matchShownInDescription", () => {
  it("is true for account_identity, whose match already appears in the finding description", () => {
    expect(
      matchShownInDescription(result("account_identity", "jane@gmail.com")),
    ).toBe(true);
  });

  it("is false for content findings whose match must be surfaced separately", () => {
    expect(matchShownInDescription(result("gitleaks", "AKIAEXAMPLE"))).toBe(
      false,
    );
    expect(matchShownInDescription(result("presidio", "x"))).toBe(false);
  });
});

describe("getMatchStrings", () => {
  it("returns distinct, non-empty matches, longest first", () => {
    expect(
      getMatchStrings([
        result("gitleaks", "short"),
        result("gitleaks", "a-longer-secret"),
        result("gitleaks", "short"), // duplicate collapses
        result("presidio", ""), // empty ignored
      ]),
    ).toEqual(["a-longer-secret", "short"]);
  });

  it("excludes account_identity matches so the authenticated email is never highlighted or shown as an out-of-text flagged value", () => {
    expect(
      getMatchStrings([result("account_identity", "jane@gmail.com")]),
    ).toEqual([]);
  });

  it("drops account_identity while keeping real content matches in a mixed message", () => {
    expect(
      getMatchStrings([
        result("account_identity", "jane@gmail.com"),
        result("gitleaks", "AKIAEXAMPLE"),
      ]),
    ).toEqual(["AKIAEXAMPLE"]);
  });

  it("returns [] for empty or undefined input", () => {
    expect(getMatchStrings([])).toEqual([]);
    expect(getMatchStrings(undefined)).toEqual([]);
  });
});
