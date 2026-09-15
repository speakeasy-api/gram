import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { describe, expect, it } from "vitest";
import { exactCandidate, exclusionOptions } from "./exclusion-options";

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

const values = (results: RiskResult[]) =>
  exclusionOptions(results).map((o) => o.value);

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
    const options = exclusionOptions([result()], {
      value: "jane@acme.com",
      label: "jane@acme.com",
    });
    expect(options[0]).toMatchObject({
      value: "exact",
      fields: { matchType: "exact", matchValue: "jane@acme.com" },
    });
  });

  it("labels a pending reveal but leaves it unsavable", () => {
    const options = exclusionOptions([result()], {
      label: "<redacted len=13 sha=ab12>",
    });
    expect(options[0]?.value).toBe("exact");
    expect(options[0]?.title).toContain("<redacted len=13 sha=ab12>");
    expect(options[0]?.fields).toBeUndefined();
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

  it("offers a preset rule before any findings load", () => {
    const options = exclusionOptions([], undefined, "pii.email_address");
    expect(options.map((o) => o.value)).toEqual(["rule", "custom"]);
    expect(options[0]?.fields).toMatchObject({
      matchType: "rule_id",
      matchValue: "pii.email_address",
    });
  });

  it("lets the selection's shared rule win over a preset", () => {
    const options = exclusionOptions(
      [result({ ruleId: "generic-api-key" })],
      undefined,
      "pii.email_address",
    );
    expect(options.find((o) => o.value === "rule")?.fields).toMatchObject({
      matchType: "rule_id",
      matchValue: "generic-api-key",
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

describe("exactCandidate", () => {
  const masked = result({ matchRedacted: "<redacted len=13 sha=ab12>" });

  it("prefers a match the finding already carries", () => {
    expect(
      exactCandidate(result({ match: "jane@acme.com" }), null, true),
    ).toEqual({ value: "jane@acme.com", label: "jane@acme.com" });
  });

  it("labels the redaction while the reveal is unfired", () => {
    expect(exactCandidate(masked, null, true)).toEqual({
      label: "<redacted len=13 sha=ab12>",
    });
  });

  it("takes the plaintext once the reveal resolves", () => {
    expect(exactCandidate(masked, "jane@acme.com", true)).toEqual({
      value: "jane@acme.com",
      label: "jane@acme.com",
    });
  });

  it("offers nothing without the reveal scope, or with nothing to reveal", () => {
    expect(exactCandidate(masked, null, false)).toBeUndefined();
    expect(exactCandidate(result(), null, true)).toBeUndefined();
    expect(exactCandidate(undefined, null, true)).toBeUndefined();
  });
});
