import type { WorkloadAdmission } from "@gram/client/models/components/workloadadmission.js";
import { expect, it } from "vitest";
import {
  duplicateRuleMessage,
  existingRule,
  toCatalogEntry,
} from "./platforms";
import { claudeTagPlatform } from "./catalogFixture";

const entry = toCatalogEntry(claudeTagPlatform);

function admission(overrides: Partial<WorkloadAdmission>): WorkloadAdmission {
  return {
    agentId: "agent-1",
    agentName: "Support bot",
    createdAt: new Date(0),
    id: "admission-1",
    issuer: entry.issuer.value,
    issuerName: entry.displayName,
    matchKind: "wildcard",
    name: "",
    organizationId: "org",
    projectId: "",
    subject: "wimse://example/org-1/*",
    tags: [],
    updatedAt: new Date(0),
    wildcardActive: true,
    workloadIssuerId: "issuer-1",
    ...overrides,
  };
}

const wildcard = {
  subject: "wimse://example/org-1/*",
  matchKind: "wildcard",
} as const;

it("finds the organization-wide rule the values would repeat", () => {
  const rule = admission({});
  expect(existingRule(entry, [rule], wildcard)).toBe(rule);
  expect(
    existingRule(entry, [rule], {
      ...wildcard,
      subject: "wimse://example/org-2/*",
    }),
  ).toBeUndefined();
});

it("does not take an exact rule ending in * for the wildcard rule", () => {
  expect(
    existingRule(entry, [admission({ matchKind: "exact" })], wildcard),
  ).toBeUndefined();
});

it("ignores a rule under another issuer or in a project", () => {
  expect(
    existingRule(
      entry,
      [admission({ issuer: "https://other.example.com" })],
      wildcard,
    ),
  ).toBeUndefined();
  expect(
    existingRule(entry, [admission({ projectId: "project-1" })], wildcard),
  ).toBeUndefined();
});

it("names the value that is already connected, and its agent", () => {
  const label = entry.variables.find((v) => v.tier === "rule")!.label;
  expect(duplicateRuleMessage(entry, "Support bot")).toBe(
    `This ${label} is already connected to ${entry.displayName}, under Support bot. Use a different one, or change its agent from the access list.`,
  );
});
