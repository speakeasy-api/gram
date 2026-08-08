import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { describe, expect, it } from "vitest";
import { exclusionOptions } from "./exclusion-options";

function result(overrides: Partial<RiskResult> = {}): RiskResult {
  return {
    id: "r1",
    policyId: "p1",
    policyVersion: 1,
    source: "presidio",
    ruleId: "pii.email_address",
    createdAt: new Date(0),
    ...overrides,
  };
}

const values = (results: RiskResult[], revealed?: string) =>
  exclusionOptions(results, revealed).map((o) => o.value);

describe("exclusionOptions", () => {
  it("offers only custom for an empty selection", () => {
    expect(values([])).toEqual(["custom"]);
  });

  it("offers exact first when a single finding holds its match", () => {
    const options = exclusionOptions([result({ match: "jane@acme.com" })]);
    expect(options.map((o) => o.value)).toEqual([
      "exact",
      "rule",
      "source",
      "custom",
    ]);
    expect(options[0]?.fields).toEqual({
      matchType: "exact",
      matchValue: "jane@acme.com",
      ruleIdFilter: "",
      sourceFilter: "",
    });
    expect(options[0]?.title).toContain("jane@acme.com");
  });

  it("offers exact from a revealed value on a masked finding", () => {
    const options = exclusionOptions([result()], "jane@acme.com");
    expect(options[0]).toMatchObject({
      value: "exact",
      fields: { matchType: "exact", matchValue: "jane@acme.com" },
    });
  });

  it("offers rule and source but never exact for a shared-rule batch", () => {
    const options = exclusionOptions([
      result({ id: "a", match: "one@acme.com" }),
      result({ id: "b", match: "two@acme.com" }),
    ]);
    expect(options.map((o) => o.value)).toEqual(["rule", "source", "custom"]);
    expect(options[0]?.fields).toMatchObject({
      matchType: "rule_id",
      matchValue: "pii.email_address",
    });
    expect(options[1]?.fields).toMatchObject({
      matchType: "source",
      matchValue: "presidio",
    });
  });

  it("offers only custom when rows share neither rule nor source", () => {
    expect(
      values([
        result({ id: "a" }),
        result({ id: "b", source: "gitleaks", ruleId: "generic-api-key" }),
      ]),
    ).toEqual(["custom"]);
  });
});
