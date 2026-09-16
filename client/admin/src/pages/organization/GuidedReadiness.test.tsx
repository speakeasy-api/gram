import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { IdentityProviderReadiness } from "@gram/admin-client/models/components/identityproviderreadiness";
import type { GuidedReadinessQuery } from "@/lib/guidedReadiness";

const readiness = vi.hoisted(() => ({
  current: {} as GuidedReadinessQuery,
  readFor: [] as string[],
}));

vi.mock("@/lib/guidedReadiness", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/guidedReadiness")>();
  return {
    ...actual,
    useOrganizationGuidedReadiness: (organizationID: string) => {
      readiness.readFor.push(organizationID);
      return readiness.current;
    },
  };
});

import { GuidedReadinessFacts } from "./GuidedReadiness";

const ORG_ID = "00000000-0000-4000-8000-000000000000";
const CHECKED_AT = new Date("2026-09-15T10:00:00Z");

function aReadiness(
  overrides: Partial<IdentityProviderReadiness> = {},
): IdentityProviderReadiness {
  return {
    provider: "okta",
    eligible: true,
    checkedAt: CHECKED_AT,
    checks: [
      {
        key: "workos_organization_linked",
        ok: true,
        detail: "The organization is linked.",
        remedy: "",
        owner: "platform_admin",
        checkedAt: CHECKED_AT,
      },
    ],
    ...overrides,
  };
}

function aQuery(
  overrides: Partial<GuidedReadinessQuery> = {},
): GuidedReadinessQuery {
  return {
    data: aReadiness(),
    isPending: false,
    isFetching: false,
    error: null,
    refetch: vi.fn<() => void>(),
    ...overrides,
  };
}

afterEach(cleanup);
beforeEach(() => {
  readiness.current = aQuery();
  readiness.readFor = [];
});

describe("GuidedReadinessFacts", () => {
  it("reads readiness for the record on screen", () => {
    render(<GuidedReadinessFacts organizationID={ORG_ID} />);

    expect(readiness.readFor).toEqual([ORG_ID]);
    expect(screen.getByText("Eligible")).toBeTruthy();
  });

  it("shows each check with its detail, remedy and owner", () => {
    readiness.current = aQuery({
      data: aReadiness({
        eligible: false,
        checks: [
          {
            key: "workos_organization_linked",
            ok: true,
            detail: "The organization is linked.",
            remedy: "",
            owner: "platform_admin",
            checkedAt: CHECKED_AT,
          },
          {
            key: "connections_api_available",
            ok: false,
            detail: "The create-connection capability is off.",
            remedy: "Ask for the capability on this environment.",
            owner: "speakeasy",
            checkedAt: CHECKED_AT,
          },
        ],
      }),
    });

    render(<GuidedReadinessFacts organizationID={ORG_ID} />);

    expect(screen.getByText("Not eligible")).toBeTruthy();
    expect(screen.getByText("workos_organization_linked")).toBeTruthy();
    expect(screen.getByText("connections_api_available")).toBeTruthy();
    expect(
      screen.getByText("Ask for the capability on this environment."),
    ).toBeTruthy();
    expect(screen.getByText("Platform admin")).toBeTruthy();
    expect(screen.getByText("Speakeasy")).toBeTruthy();
    expect(screen.getByText("Passed")).toBeTruthy();
    expect(screen.getByText("Did not pass")).toBeTruthy();
  });

  it("re-checks on demand", () => {
    const refetch = vi.fn<() => void>();
    readiness.current = aQuery({ refetch });

    render(<GuidedReadinessFacts organizationID={ORG_ID} />);
    fireEvent.click(screen.getByRole("button", { name: "Re-check" }));

    expect(refetch).toHaveBeenCalledTimes(1);
  });

  it("says so when the read itself failed", () => {
    readiness.current = aQuery({
      data: undefined,
      error: new Error("readiness endpoint unreachable"),
    });

    render(<GuidedReadinessFacts organizationID={ORG_ID} />);

    expect(screen.getByText("Readiness could not be checked.")).toBeTruthy();
    expect(screen.getByText("readiness endpoint unreachable")).toBeTruthy();
  });
});
