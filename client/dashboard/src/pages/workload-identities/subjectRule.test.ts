import { expect, it } from "vitest";
import {
  composeSubject,
  shouldSwitchToWildcard,
  subjectRuleWarning,
  typedSubjectForMatchKind,
} from "./subjectRule";

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

it("owns the wildcard terminator so it is never typed", () => {
  // The whole point: selecting Wildcard is the only place breadth is stated, so
  // the stem the operator types becomes a terminated rule without them adding it.
  expect(composeSubject("wildcard", STEM)).toBe(`${STEM}*`);
  // Idempotent, so a pasted rule that already carries its terminator does not
  // end up with two.
  expect(composeSubject("wildcard", `${STEM}*`)).toBe(`${STEM}*`);
  // An empty field stays empty rather than becoming a bare "*", which would
  // admit every subject the issuer signs.
  expect(composeSubject("wildcard", "")).toBe("");
  expect(composeSubject("wildcard", "   ")).toBe("");
});

it("passes an exact subject through verbatim, star included", () => {
  // Stripping it would hide the mistake the warning exists to surface.
  expect(composeSubject("exact", `${STEM}*`)).toBe(`${STEM}*`);
});

it("lifts a typed terminator out of the field when switching to wildcard", () => {
  // Otherwise the rendered suffix would double it.
  expect(typedSubjectForMatchKind(`${STEM}*`, "wildcard")).toBe(STEM);
  expect(typedSubjectForMatchKind(STEM, "wildcard")).toBe(STEM);
  // Switching to exact leaves the text alone: the operator may have meant it.
  expect(typedSubjectForMatchKind(`${STEM}*`, "exact")).toBe(`${STEM}*`);
});

it("switches to wildcard when a terminated rule is pasted into an exact field", () => {
  expect(shouldSwitchToWildcard(`${STEM}*`, "exact", true)).toBe(true);
  // Never to a kind the issuer forbids, so the dialog cannot select something
  // the server would refuse.
  expect(shouldSwitchToWildcard(`${STEM}*`, "exact", false)).toBe(false);
  // A bare "*" is not a rule worth switching for; it would admit everything.
  expect(shouldSwitchToWildcard("*", "exact", true)).toBe(false);
  expect(shouldSwitchToWildcard(STEM, "exact", true)).toBe(false);
});

it("makes the terminator warnings unreachable through the dialog", () => {
  // These stay in subjectRuleWarning because the server still refuses them for
  // any caller that does not come through this dialog. What changed is that a
  // wildcard rule composed from a typed stem cannot land in those states.
  for (const stem of [STEM, "repo:acme/payments-api:ref:refs/heads/", "a"]) {
    expect(
      subjectRuleWarning("wildcard", composeSubject("wildcard", stem)),
    ).toBeNull();
  }
  // Still caught for anything that reaches the function another way.
  expect(subjectRuleWarning("wildcard", STEM)).toContain('must end in "*"');
  expect(subjectRuleWarning("wildcard", "*")).toContain("bare");
});
