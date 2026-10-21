import { describe, expect, it } from "vitest";
import {
  catalogEntryForIssuer,
  platformEndpoints,
  valuesFromIssuer,
  valuesFromSubject,
  type CatalogEntry,
} from "./workloadCatalog";

const cluster =
  "https://container.googleapis.com/v1/projects/{project}/locations/{location}/clusters/{cluster}";

const gke: CatalogEntry = {
  key: "gke",
  displayName: "GKE",
  description: "",
  enabled: true,
  issuer: { value: cluster, visibility: "read-only" },
  jwksUri: { value: `${cluster}/jwks`, visibility: "hidden" },
  variables: [
    {
      key: "project",
      tier: "platform",
      label: "Project ID",
      pattern: "[a-z][a-z0-9-]{4,28}[a-z0-9]",
    },
    {
      key: "location",
      tier: "platform",
      label: "Location",
      pattern: "[a-z0-9-]+",
    },
    {
      key: "cluster",
      tier: "platform",
      label: "Cluster name",
      pattern: "[a-z0-9-]+",
    },
    {
      key: "namespace",
      tier: "rule",
      label: "Namespace",
      pattern: "[a-z0-9-]+",
    },
    {
      key: "service_account",
      tier: "rule",
      label: "Service account",
      pattern: "[a-z0-9.-]+",
    },
  ],
  subject: {
    template: "system:serviceaccount:{namespace}:{service_account}",
    wildcard: false,
  },
};

const claudeTag: CatalogEntry = {
  key: "claude-tag",
  displayName: "Claude Tag",
  description: "",
  enabled: true,
  issuer: {
    value: "https://identity.anthropic.com/agents",
    visibility: "hidden",
  },
  jwksUri: {
    value: "https://identity.anthropic.com/agents/jwks.json",
    visibility: "hidden",
  },
  variables: [
    {
      key: "org_id",
      tier: "rule",
      label: "Organization ID",
      pattern: "[A-Za-z0-9-]+",
    },
  ],
  subject: {
    template: "wimse://identity.anthropic.com/org/{org_id}/agent/",
    wildcard: true,
  },
};

const prodIssuer =
  "https://container.googleapis.com/v1/projects/acme-prod/locations/us-central1/clusters/payments";

describe("platformEndpoints", () => {
  it("fills a cluster's issuer and keys from its values", () => {
    expect(
      platformEndpoints(gke, {
        project: "acme-prod",
        location: "us-central1",
        cluster: "payments",
      }),
    ).toEqual({ issuer: prodIssuer, jwksUri: `${prodIssuer}/jwks` });
  });

  it("is null until every value is valid", () => {
    expect(
      platformEndpoints(gke, { project: "acme-prod", location: "us-central1" }),
    ).toBeNull();
    expect(
      platformEndpoints(gke, {
        project: "acme-prod",
        location: "us-central1",
        cluster: "pay/ments",
      }),
    ).toBeNull();
  });

  it("returns a constant issuer as it is", () => {
    expect(platformEndpoints(claudeTag, {})?.issuer).toBe(
      "https://identity.anthropic.com/agents",
    );
  });
});

describe("catalogEntryForIssuer", () => {
  it("recognizes each cluster's issuer as the entry's", () => {
    expect(catalogEntryForIssuer([claudeTag, gke], prodIssuer)?.key).toBe(
      "gke",
    );
  });

  it("still matches a constant issuer exactly", () => {
    const entries = [gke, claudeTag];
    expect(
      catalogEntryForIssuer(entries, "https://identity.anthropic.com/agents")
        ?.key,
    ).toBe("claude-tag");
    expect(
      catalogEntryForIssuer(entries, "https://identity.anthropic.com/agentsX"),
    ).toBeUndefined();
  });

  it("does not let a placeholder span a path segment", () => {
    expect(
      catalogEntryForIssuer(
        [gke],
        "https://container.googleapis.com/v1/projects/a/b/locations/l/clusters/c",
      ),
    ).toBeUndefined();
  });
});

describe("reading values back for editing", () => {
  it("reads a cluster from its issuer and a workload from its subject", () => {
    expect({
      ...valuesFromIssuer(gke, prodIssuer),
      ...valuesFromSubject(gke, "system:serviceaccount:billing:invoice-worker"),
    }).toEqual({
      project: "acme-prod",
      location: "us-central1",
      cluster: "payments",
      namespace: "billing",
      service_account: "invoice-worker",
    });
  });

  it("reads a wildcard rule's stem", () => {
    expect(
      valuesFromSubject(
        claudeTag,
        "wimse://identity.anthropic.com/org/org-123/agent/*",
      ),
    ).toEqual({ org_id: "org-123" });
  });
});
