import { expect, it } from "vitest";
import { subjectRuleWarning } from "./subjectRule";

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
