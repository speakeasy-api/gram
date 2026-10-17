import { unrecognized } from "@gram/client/types/unrecognized.js";
import type { WorkloadPlatform } from "@gram/client/models/components/workloadplatform.js";
import type { WorkloadIssuer } from "@gram/client/models/components/workloadissuer.js";
import type { WorkloadAdmission } from "@gram/client/models/components/workloadadmission.js";
import { expect, it } from "vitest";
import {
  connectedIssuer,
  toCatalogItem,
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

it.each([
  [
    "phase",
    (platform: WorkloadPlatform) => {
      platform.steps[0]!.phase = unrecognized("future_phase");
    },
  ],
  [
    "tier",
    (platform: WorkloadPlatform) => {
      platform.variables[0]!.tier = unrecognized("future_tier");
    },
  ],
  [
    "issuer visibility",
    (platform: WorkloadPlatform) => {
      platform.issuer.visibility = unrecognized("future_visibility");
    },
  ],
  [
    "JWKS visibility",
    (platform: WorkloadPlatform) => {
      platform.jwksUri.visibility = unrecognized("future_visibility");
    },
  ],
  [
    "block type",
    (platform: WorkloadPlatform) => {
      platform.steps[0]!.blocks[0]!.type = unrecognized("future_block");
    },
  ],
  [
    "block value",
    (platform: WorkloadPlatform) => {
      platform.steps[0]!.blocks[0]!.value = unrecognized("future_value");
    },
  ],
] as const)(
  "keeps a platform with unknown %s visible without inventing setup metadata",
  (_name, change) => {
    const platform = structuredClone(claudeTagPlatform);
    change(platform);
    const item = toCatalogItem(platform);
    expect(item).toMatchObject({
      key: platform.key,
      displayName: platform.displayName,
      enabled: platform.enabled,
      unsupported: true,
      issuer: { value: platform.issuer.value },
    });
    expect(item.setup).toBeUndefined();
    expect(item).not.toHaveProperty("variables");
    expect(item.issuer).not.toHaveProperty("visibility");
    const issuer = { issuer: platform.issuer.value } as WorkloadIssuer;
    expect(connectedIssuer(item, [issuer])).toBe(issuer);
  },
);

it("keeps supported catalog setup unchanged", () => {
  expect(toCatalogItem(claudeTagPlatform)).toEqual(
    toCatalogEntry(claudeTagPlatform),
  );
});
