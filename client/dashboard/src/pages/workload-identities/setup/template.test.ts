import { expect, it } from "vitest";
import { toCatalogEntry } from "./platforms";
import { testPlatform } from "./testPlatform";
import { subjectRule, variableProblem } from "./template";

const claudeTag = toCatalogEntry(testPlatform);
const orgId = claudeTag.variables[0]!;

it("pins a Claude Tag rule to one organization with a trailing wildcard", () => {
  expect(subjectRule(claudeTag, { org_id: " org-123 " })).toEqual({
    subject: "wimse://identity.anthropic.com/org/org-123/agent/*",
    matchKind: "wildcard",
  });
});

it("produces no rule until every variable is usable", () => {
  expect(subjectRule(claudeTag, {})).toBeNull();
  expect(subjectRule(claudeTag, { org_id: "" })).toBeNull();
});

it("refuses a value that would reach past the organization segment", () => {
  // A "/" would let the stem cover another organization's agents.
  expect(variableProblem(orgId, "org-123/agent/x")).not.toBeNull();
  expect(subjectRule(claudeTag, { org_id: "a/b" })).toBeNull();
  expect(variableProblem(orgId, "*")).not.toBeNull();
});

it("reads a value as invalid when the browser cannot compile the pattern", () => {
  expect(variableProblem({ ...orgId, pattern: "(?<" }, "org-123")).toBe(
    orgId.patternMessage,
  );
});

it("matches the pattern in full, not as a substring", () => {
  expect(variableProblem(orgId, "ok-1 bad")).not.toBeNull();
  expect(variableProblem(orgId, "ok-1")).toBeNull();
});
