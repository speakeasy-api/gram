import type { IdentityProviderConnectionChecklistItem } from "@gram/client/models/components/identityproviderconnectionchecklistitem.js";

import type { LiveConnection } from "../../connectionView";

export function makeConnection(
  overrides: Partial<LiveConnection> = {},
): LiveConnection {
  return {
    id: "connection-id",
    organizationId: "organization-id",
    provider: "okta",
    status: "pending",
    orgUrl: "https://acme.okta.com",
    issuerUrl: "https://acme.okta.com",
    jwksUrl: "https://speakeasy.test/jwks.json",
    listingMode: "custom_app",
    clientIdSubmitted: true,
    dpopRequired: true,
    requiredScopes: [],
    grantedScopes: [],
    missingScopes: [],
    verificationReasons: [],
    checklist: [],
    applicationsSync: {},
    createdAt: new Date("2026-01-01T00:00:00Z"),
    updatedAt: new Date("2026-01-02T00:00:00Z"),
    ...overrides,
  };
}

export function makeChecklistItem(
  key: IdentityProviderConnectionChecklistItem["key"],
  group: IdentityProviderConnectionChecklistItem["group"],
  completed?: boolean,
): IdentityProviderConnectionChecklistItem {
  return {
    key,
    group,
    title: `Step ${key}`,
    description: `Do ${key}.`,
    details: [],
    ...(completed === undefined ? {} : { completed }),
  };
}
