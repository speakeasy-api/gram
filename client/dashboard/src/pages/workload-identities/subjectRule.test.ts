import { expect, it } from "vitest";
import { inferMatchKind, subjectRuleWarning } from "./subjectRule";

const STEM = "wimse://identity.example.com/org/acme/agent/";

it("says nothing about an empty field", () => {
  expect(subjectRuleWarning("exact", "")).toBeNull();
  expect(subjectRuleWarning("wildcard", "   ")).toBeNull();
});

it("accepts an exact subject and a well-formed wildcard rule", () => {
  expect(subjectRuleWarning("exact", `${STEM}a-1`)).toBeNull();
  expect(subjectRuleWarning("wildcard", `${STEM}*`)).toBeNull();
});

it("warns about a star in an exact subject", () => {
  // The one that reads as correct in the table afterwards: the subject is
  // compared in full, so the rule admits nothing at all.
  const warning = subjectRuleWarning("exact", `${STEM}*`);
  expect(warning).toContain("in full, literally");
  expect(warning).toContain("Wildcard");
});

it("warns about a wildcard rule with no terminator", () => {
  // A bare stem matches the same subjects while hiding that it does.
  expect(subjectRuleWarning("wildcard", STEM)).toContain('must end in "*"');
});

it("warns about a bare star", () => {
  // It would admit every subject the issuer signs, including other tenants of it.
  expect(subjectRuleWarning("wildcard", "*")).toContain("every subject");
});

it("warns about an interior star", () => {
  expect(
    subjectRuleWarning(
      "wildcard",
      "wimse://identity.example.com/org/*/agent/*",
    ),
  ).toContain("Only a trailing");
});

it("ignores surrounding whitespace, which the page trims before submitting", () => {
  expect(subjectRuleWarning("wildcard", `  ${STEM}*  `)).toBeNull();
});

it("warns when the stem ends in whitespace before its star", () => {
  // The same silent failure as a missing terminator and invisible in the field:
  // only the outer whitespace is trimmed, so the space stays part of the stem
  // and the rule matches nothing. Refused by ValidateSubjectRule too, so the
  // client and the server share one grammar.
  const warning = subjectRuleWarning("wildcard", `${STEM} *`);

  expect(warning).toContain("whitespace");
});

it("reads the match kind off the subject", () => {
  // No control states this: the terminator is how a rule states its own breadth,
  // so a separate selector could only disagree with the value.
  expect(inferMatchKind(`${STEM}a-1`)).toBe("exact");
  expect(inferMatchKind(`${STEM}*`)).toBe("wildcard");
  expect(inferMatchKind("")).toBe("exact");
  expect(inferMatchKind("  ")).toBe("exact");
});

it("reads a misplaced star as a wildcard, so the message names the real problem", () => {
  // Lossless, because an exact subject may never contain a star — the server
  // refuses one outright. So a star anywhere means a wildcard was intended, and
  // reporting a misplaced terminator is more useful than reporting a literal.
  expect(inferMatchKind("wimse://x/*/agent/a-1")).toBe("wildcard");
  expect(subjectRuleWarning("wildcard", "wimse://x/*/agent/a-1")).toContain(
    'must end in "*"',
  );
  // A bare star still says what it would do, rather than being read as exact.
  expect(inferMatchKind("*")).toBe("wildcard");
  expect(subjectRuleWarning("wildcard", "*")).toContain("bare");
});
