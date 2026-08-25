import type { IssuerConvergenceCandidate } from "@gram/client/models/components/issuerconvergencecandidate.js";
import type { IssuerFieldMismatch } from "@gram/client/models/components/issuerfieldmismatch.js";
import { describe, expect, it } from "vitest";
import {
  candidateBlockerSummary,
  candidateIsBlocked,
  candidateOwnerLabel,
} from "./convergenceBlockers";

function scalarMismatch(
  field: string,
  sourceValue: string,
  targetValue: string,
): IssuerFieldMismatch {
  return { field, sourceValue, targetValue };
}

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
      candidate({
        endpointMismatches: [
          scalarMismatch(
            "token_endpoint",
            "https://a.example.com/token",
            "https://b.example.com/token",
          ),
        ],
      }),
    );
    expect(summary).toContain("token_endpoint");
    expect(summary).toContain("Different authorization server");
  });

  // The status column has room for a field name and not for two URLs. The
  // dialog is where the values belong, so a summary that started quoting them
  // would be the regression, not the improvement.
  it("leaves the values to the dialog", () => {
    const summary = candidateBlockerSummary(
      candidate({
        endpointMismatches: [
          scalarMismatch(
            "token_endpoint",
            "https://a.example.com/token",
            "https://b.example.com/token",
          ),
        ],
      }),
    );
    expect(summary).not.toContain("https://a.example.com/token");
  });

  it("surfaces warnings when nothing blocks", () => {
    expect(
      candidateBlockerSummary(
        candidate({ warnings: [scalarMismatch("oidc", "true", "false")] }),
      ),
    ).toBe("oidc differs; the platform provider's values become authoritative");
  });

  it("names every warning field", () => {
    const summary = candidateBlockerSummary(
      candidate({
        warnings: [
          scalarMismatch("oidc", "true", "false"),
          { field: "scopes_supported", sourceValues: ["openid"] },
        ],
      }),
    );
    expect(summary).toBe(
      "oidc, scopes_supported differ; the platform provider's values become authoritative",
    );
  });

  // A mismatch outranks a warning: one refuses the migration, the other only
  // describes what the target will overwrite.
  it("prefers blockers over warnings", () => {
    expect(
      candidateBlockerSummary(
        candidate({
          endpointMismatches: [
            scalarMismatch(
              "issuer",
              "https://a.example.com",
              "https://b.example.com",
            ),
          ],
          warnings: [{ field: "scopes_supported", sourceValues: ["openid"] }],
        }),
      ),
    ).toContain("issuer");
  });
});

describe("candidateIsBlocked", () => {
  it("treats an endpoint mismatch as blocking", () => {
    expect(
      candidateIsBlocked(
        candidate({
          endpointMismatches: [
            scalarMismatch(
              "issuer",
              "https://a.example.com",
              "https://b.example.com",
            ),
          ],
        }),
      ),
    ).toBe(true);
  });

  it("does not treat a warning as blocking", () => {
    expect(
      candidateIsBlocked(
        candidate({
          warnings: [{ field: "scopes_supported", sourceValues: ["openid"] }],
        }),
      ),
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
