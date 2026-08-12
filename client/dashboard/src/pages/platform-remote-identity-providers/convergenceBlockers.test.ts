import type { IssuerConvergenceCandidate } from "@gram/client/models/components/issuerconvergencecandidate.js";
import { describe, expect, it } from "vitest";
import {
  candidateBlockerSummary,
  candidateIsBlocked,
  candidateOwnerLabel,
} from "./convergenceBlockers";

function candidate(
  overrides: Partial<IssuerConvergenceCandidate> = {},
): IssuerConvergenceCandidate {
  return {
    issuer: {
      id: "11111111-1111-1111-1111-111111111111",
      slug: "acme-idp",
      issuer: "https://idp.example.com",
    },
    organizationId: "org_123",
    organizationName: "Acme Inc",
    clientCount: 2,
    endpointMismatches: [],
    warnings: [],
    ...overrides,
  } as IssuerConvergenceCandidate;
}

describe("candidateBlockerSummary", () => {
  it("reports no known blockers for a matching candidate", () => {
    expect(candidateBlockerSummary(candidate())).toBe("No blockers found");
  });

  // The whole point of listing near-misses rather than filtering them out: the
  // admin has to learn which field disagrees, not just that something does.
  it("names the fields that disagree", () => {
    const summary = candidateBlockerSummary(
      candidate({ endpointMismatches: ["token_endpoint"] }),
    );
    expect(summary).toContain("token_endpoint");
    expect(summary).toContain("Different authorization server");
  });

  it("surfaces warnings when nothing blocks", () => {
    expect(
      candidateBlockerSummary(
        candidate({ warnings: ["oidc changes from true to false"] }),
      ),
    ).toBe("oidc changes from true to false");
  });

  // A mismatch outranks a warning: one refuses the migration, the other only
  // describes what the target will overwrite.
  it("prefers blockers over warnings", () => {
    expect(
      candidateBlockerSummary(
        candidate({
          endpointMismatches: ["issuer"],
          warnings: ["scopes_supported differs"],
        }),
      ),
    ).toContain("issuer");
  });
});

describe("candidateIsBlocked", () => {
  it("treats an endpoint mismatch as blocking", () => {
    expect(
      candidateIsBlocked(candidate({ endpointMismatches: ["issuer"] })),
    ).toBe(true);
  });

  it("does not treat a warning as blocking", () => {
    expect(
      candidateIsBlocked(candidate({ warnings: ["scopes_supported differs"] })),
    ).toBe(false);
  });
});

describe("candidateOwnerLabel", () => {
  it("prefers the organization name", () => {
    expect(candidateOwnerLabel(candidate())).toBe("Acme Inc");
  });

  // WorkOS metadata may not have synced; the id still identifies the tenant.
  it("falls back to the organization id", () => {
    expect(candidateOwnerLabel(candidate({ organizationName: "" }))).toBe(
      "org_123",
    );
  });

  // A legacy project issuer predating the organization_id column has neither.
  it("never renders a blank owner", () => {
    expect(
      candidateOwnerLabel(
        candidate({ organizationName: "", organizationId: "" }),
      ),
    ).toBe("Unknown organization");
  });
});
