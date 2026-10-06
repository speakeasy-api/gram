import type { WorkloadAdmission } from "@gram/client/models/components/workloadadmission.js";
import { expect, it } from "vitest";
import {
  duplicateRuleMessage,
  existingRule,
  toCatalogEntry,
} from "./platforms";
import { testPlatform } from "./testPlatform";

const entry = toCatalogEntry(testPlatform);

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

it("finds the organization-wide rule a subject would repeat", () => {
  const rule = admission({});
  expect(existingRule(entry, [rule], rule.subject)).toBe(rule);
  expect(
    existingRule(entry, [rule], "wimse://example/org-2/*"),
  ).toBeUndefined();
});

it("ignores a rule under another issuer or in a project", () => {
  const subject = "wimse://example/org-1/*";
  expect(
    existingRule(
      entry,
      [admission({ issuer: "https://other.example.com" })],
      subject,
    ),
  ).toBeUndefined();
  expect(
    existingRule(entry, [admission({ projectId: "project-1" })], subject),
  ).toBeUndefined();
});

it("names the value that is already connected, and its agent", () => {
  const label = entry.variables.find((v) => v.tier === "rule")!.label;
  expect(duplicateRuleMessage(entry, "Support bot")).toBe(
    `This ${label} is already connected to ${entry.displayName}, under Support bot. Use a different one, or change its agent from the access list.`,
  );
});
