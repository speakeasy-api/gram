import { describe, expect, it } from "vitest";
import {
  parseExclusionExpression,
  serializeExclusionExpression,
} from "./exclusion-expression";

describe("parseExclusionExpression", () => {
  it("parses an exact match", () => {
    expect(parseExclusionExpression('match == "jane.doe@acme.com"')).toEqual({
      ok: true,
      value: {
        matchType: "exact",
        matchValue: "jane.doe@acme.com",
        ruleIdFilter: "",
        sourceFilter: "",
      },
    });
  });

  it("parses a regex match", () => {
    const result = parseExclusionExpression(
      'match ~= "noreply@.*\\.acme\\.com"',
    );
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.value.matchType).toBe("regex");
      expect(result.value.matchValue).toBe("noreply@.*\\.acme\\.com");
    }
  });

  it("parses rule_id/source/entity_type primaries", () => {
    expect(parseExclusionExpression('rule_id == "pii.email"')).toMatchObject({
      ok: true,
      value: { matchType: "rule_id", matchValue: "pii.email" },
    });
    expect(parseExclusionExpression('source == "presidio"')).toMatchObject({
      ok: true,
      value: { matchType: "source", matchValue: "presidio" },
    });
    expect(parseExclusionExpression('entity_type == "EMAIL"')).toMatchObject({
      ok: true,
      value: { matchType: "entity_type", matchValue: "EMAIL" },
    });
  });

  it("treats a trailing rule_id/source clause as a filter", () => {
    expect(
      parseExclusionExpression(
        'match == "x" && rule_id == "pii.email" && source == "test"',
      ),
    ).toEqual({
      ok: true,
      value: {
        matchType: "exact",
        matchValue: "x",
        ruleIdFilter: "pii.email",
        sourceFilter: "test",
      },
    });
  });

  it("rejects ~= on a non-match field", () => {
    expect(parseExclusionExpression('rule_id ~= "x"').ok).toBe(false);
  });

  it("rejects an empty expression", () => {
    expect(parseExclusionExpression("   ").ok).toBe(false);
  });

  it("rejects unparseable input", () => {
    expect(parseExclusionExpression("match = value").ok).toBe(false);
  });

  it("rejects an invalid regex", () => {
    expect(parseExclusionExpression('match ~= "("').ok).toBe(false);
  });

  it("rejects two match clauses", () => {
    expect(parseExclusionExpression('match == "a" && match == "b"').ok).toBe(
      false,
    );
  });
});

describe("serializeExclusionExpression", () => {
  it("round-trips an exact match with filters", () => {
    const expr = serializeExclusionExpression({
      matchType: "exact",
      matchValue: "x",
      ruleIdFilter: "pii.email",
      sourceFilter: "test",
    });
    expect(expr).toBe(
      'match == "x" && rule_id == "pii.email" && source == "test"',
    );
    expect(parseExclusionExpression(expr)).toMatchObject({
      ok: true,
      value: {
        matchType: "exact",
        matchValue: "x",
        ruleIdFilter: "pii.email",
        sourceFilter: "test",
      },
    });
  });

  it("serializes a regex with ~=", () => {
    expect(
      serializeExclusionExpression({ matchType: "regex", matchValue: "a.*b" }),
    ).toBe('match ~= "a.*b"');
  });

  it("does not duplicate a rule_id primary as a filter", () => {
    expect(
      serializeExclusionExpression({
        matchType: "rule_id",
        matchValue: "pii.email",
        ruleIdFilter: "pii.email",
      }),
    ).toBe('rule_id == "pii.email"');
  });
});
