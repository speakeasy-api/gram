import type { WorkloadAdmission } from "@gram/client/models/components/workloadadmission.js";
import { expect, it } from "vitest";
import { admissionMatches } from "./search";

function admission(
  overrides: Partial<WorkloadAdmission> = {},
): WorkloadAdmission {
  return {
    id: "33333333-3333-3333-3333-333333333333",
    organizationId: "org",
    projectId: "",
    workloadIssuerId: "11111111-1111-1111-1111-111111111111",
    issuer: "https://ci-identity.example.com",
    issuerName: "Example CI",
    subject: "repo:acme/payments-api:ref:refs/heads/main",
    matchKind: "exact",
    name: "Payments deploy",
    tags: ["payments", "canary"],
    agentId: "22222222-2222-2222-2222-222222222222",
    agentName: "Release assistant",
    wildcardActive: false,
    createdAt: new Date("2026-09-28T00:00:00Z"),
    updatedAt: new Date("2026-09-28T00:00:00Z"),
    ...overrides,
  };
}

it("matches a machine by subject, label, tag or agent, ignoring case", () => {
  const machine = admission();

  expect(admissionMatches(machine, "payments-api")).toBe(true);
  expect(admissionMatches(machine, "PAYMENTS DEPLOY")).toBe(true);
  expect(admissionMatches(machine, "deploy")).toBe(true);
  expect(admissionMatches(machine, "release")).toBe(true);
  // Only the tag carries "canary", so this match proves tags are searched.
  expect(admissionMatches(machine, "CANARY")).toBe(true);
});

it("matches everything on an empty query and nothing on an unrelated one", () => {
  const machine = admission();

  expect(admissionMatches(machine, "   ")).toBe(true);
  expect(admissionMatches(machine, "docs-site")).toBe(false);
});
