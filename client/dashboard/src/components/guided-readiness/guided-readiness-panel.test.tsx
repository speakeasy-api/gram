import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { IdentityProviderReadiness } from "@gram/client/models/components/identityproviderreadiness.js";
import type { GuidedReadinessQuery } from "./use-guided-readiness";

const isPlatformAdmin = vi.fn();
const readiness = vi.hoisted(() => ({
  current: {} as GuidedReadinessQuery,
  enabledWith: [] as boolean[],
}));

vi.mock("@/contexts/Auth", () => ({
  useIsPlatformAdmin: () => isPlatformAdmin(),
}));
vi.mock("./use-guided-readiness", () => ({
  useGuidedReadiness: (enabled: boolean) => {
    readiness.enabledWith.push(enabled);
    return readiness.current;
  },
}));

import { GuidedReadinessPanel } from "./guided-readiness-panel";

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
  isPlatformAdmin.mockReset();
  isPlatformAdmin.mockReturnValue(true);
  readiness.current = aQuery();
  readiness.enabledWith = [];
});

describe("GuidedReadinessPanel", () => {
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
            key: "directory_handoff_stored",
            ok: false,
            detail: "No directory endpoint is stored.",
            remedy: "Store the directory handoff for this organization.",
            owner: "platform_admin",
            checkedAt: CHECKED_AT,
          },
        ],
      }),
    });

    render(<GuidedReadinessPanel />);

    expect(screen.getByText("Guided setup readiness")).toBeTruthy();
    expect(screen.getByText("workos_organization_linked")).toBeTruthy();
    expect(screen.getByText("The organization is linked.")).toBeTruthy();
    expect(screen.getByText("directory_handoff_stored")).toBeTruthy();
    expect(
      screen.getByText("Store the directory handoff for this organization."),
    ).toBeTruthy();
    expect(screen.getAllByText("Platform admin").length).toBe(2);
    expect(screen.getByText("Passed")).toBeTruthy();
    expect(screen.getByText("Did not pass")).toBeTruthy();
    expect(screen.getByText("Not eligible")).toBeTruthy();
  });

  it("says eligible when every check passed", () => {
    render(<GuidedReadinessPanel />);

    expect(screen.getByText("Eligible")).toBeTruthy();
    expect(screen.queryByText("Not eligible")).toBeNull();
  });

  it("renders nothing for a non-admin and does not read readiness", () => {
    isPlatformAdmin.mockReturnValue(false);

    render(<GuidedReadinessPanel />);

    expect(screen.queryByText("Guided setup readiness")).toBeNull();
    expect(screen.queryByText("Eligible")).toBeNull();
    expect(readiness.enabledWith).toEqual([false]);
  });

  // The panel sits above the provider grid on a page whose job is the grid, so
  // what it costs when nobody is reading it is the whole point.
  it("reports in one row and opens only on request", () => {
    readiness.current = aQuery({ data: aReadiness({ eligible: false }) });
    const { container } = render(<GuidedReadinessPanel />);

    const disclosure = container.querySelector("details");
    expect(disclosure).toBeTruthy();
    expect(disclosure!.open).toBe(false);

    // The verdict is on the closed row: the answer most readers want is the
    // one thing they should not have to open anything to get.
    expect(screen.getByText("Guided setup readiness")).toBeTruthy();
    expect(screen.getByText("Not eligible")).toBeTruthy();
    expect(screen.getByText(/checked /)).toBeTruthy();

    fireEvent.click(container.querySelector("summary")!);
    expect(disclosure!.open).toBe(true);
  });

  it("stays closed when the organization is eligible", () => {
    readiness.current = aQuery({ data: aReadiness({ eligible: true }) });
    const { container } = render(<GuidedReadinessPanel />);

    expect(container.querySelector("details")!.open).toBe(false);
    expect(screen.getByText("Eligible")).toBeTruthy();
  });

  it("re-checks on demand", () => {
    const refetch = vi.fn<() => void>();
    readiness.current = aQuery({ refetch });

    render(<GuidedReadinessPanel />);
    fireEvent.click(screen.getByRole("button", { name: "Re-check" }));

    expect(refetch).toHaveBeenCalledTimes(1);
  });

  it("says so when the read itself failed", () => {
    readiness.current = aQuery({
      data: undefined,
      error: new Error("readiness endpoint unreachable"),
    });

    render(<GuidedReadinessPanel />);

    expect(screen.getByText("Readiness could not be checked.")).toBeTruthy();
    expect(screen.getByText("readiness endpoint unreachable")).toBeTruthy();
  });
});
