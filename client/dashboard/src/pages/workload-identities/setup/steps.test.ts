import { expect, it } from "vitest";
import { toCatalogEntry } from "./platforms";
import { testPlatform } from "./testPlatform";
import { stepComplete, stepIndexById, stepReachable } from "./steps";

const entry = toCatalogEntry(testPlatform);
const definition = entry.setup!;
const complete = {
  entry,
  values: { org_id: "org-1" },
  agentId: "agent-1",
  tags: [],
  conflictingRuleAgent: null,
};
const empty = {
  entry,
  values: {},
  agentId: "",
  tags: [],
  conflictingRuleAgent: null,
};

it("requires a step's fields and agent", () => {
  const organization =
    definition.steps[stepIndexById(definition, "organization")]!;
  const agent = definition.steps[stepIndexById(definition, "agent")]!;
  expect(stepComplete(organization, empty)).toBe(false);
  expect(stepComplete(organization, complete)).toBe(true);
  expect(stepComplete(agent, empty)).toBe(false);
  expect(stepComplete(agent, complete)).toBe(true);
});

it("holds back values that repeat an existing access rule", () => {
  const organization =
    definition.steps[stepIndexById(definition, "organization")]!;
  expect(
    stepComplete(organization, {
      ...complete,
      conflictingRuleAgent: "Support bot",
    }),
  ).toBe(false);
});

it("keeps the create step out of reach until everything is collected", () => {
  const create = stepIndexById(definition, "agent");
  expect(stepReachable(definition, create, empty, false)).toBe(false);
  expect(stepReachable(definition, create, complete, false)).toBe(true);
});

it("unlocks the platform-side steps only once the rows exist", () => {
  const consoleStep = stepIndexById(definition, "console");
  expect(stepReachable(definition, consoleStep, complete, false)).toBe(false);
  expect(stepReachable(definition, consoleStep, empty, true)).toBe(true);
});

it("lets a trusted platform revisit every step", () => {
  const organization = stepIndexById(definition, "organization");
  const create = stepIndexById(definition, "agent");
  expect(stepReachable(definition, create, empty, false)).toBe(false);
  expect(stepReachable(definition, organization, empty, true)).toBe(true);
  expect(stepReachable(definition, create, empty, true)).toBe(true);
});

it("treats an unknown step id as absent", () => {
  expect(stepIndexById(definition, "nope")).toBe(-1);
  expect(stepIndexById(definition, null)).toBe(-1);
});
