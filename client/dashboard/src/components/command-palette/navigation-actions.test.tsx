import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter, useLocation } from "react-router";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
}));

import { useOrgRoutes } from "@/routes";
import { orgRoutesToNavActions } from "./navigation-actions";

function wrapper({ children }: { children: ReactNode }) {
  return <MemoryRouter initialEntries={["/org"]}>{children}</MemoryRouter>;
}

describe("organization navigation registration", () => {
  it("registers both billing destinations without navigating until selected", () => {
    const { result } = renderHook(
      () => {
        const routes = useOrgRoutes();
        const location = useLocation();
        return {
          actions: orgRoutesToNavActions(routes, false, true),
          pathname: location.pathname,
        };
      },
      { wrapper },
    );

    expect(result.current.pathname).toBe("/org");
    const billing = result.current.actions.filter((action) =>
      action.id.startsWith("nav-org-billing"),
    );
    expect(
      billing.map(({ id, label, group }) => ({ id, label, group })),
    ).toEqual([
      { id: "nav-org-billing", label: "Billing", group: "Organization" },
      {
        id: "nav-org-billing-setup",
        label: "Billing setup",
        group: "Organization",
      },
      {
        id: "nav-org-billing-payment-method",
        label: "Payment method",
        group: "Organization",
      },
    ]);
    act(() => {
      billing[1]!.onSelect();
    });
    expect(result.current.pathname).toBe("/org/billing/setup");
    act(() => {
      billing[2]!.onSelect();
    });
    expect(result.current.pathname).toBe("/org/billing/payment-method");
  });

  it("requires ordinary billing route access and does not expand unrelated nested routes", () => {
    const { result } = renderHook(() => useOrgRoutes(), { wrapper });
    const denied = orgRoutesToNavActions(result.current, false, false);
    const allowed = orgRoutesToNavActions(result.current, false, true);
    expect(denied.some(({ id }) => id === "nav-org-billing-setup")).toBe(false);
    expect(
      denied.some(({ id }) => id === "nav-org-billing-payment-method"),
    ).toBe(false);
    expect(
      allowed
        .filter(({ id }) => !denied.some((action) => action.id === id))
        .map(({ id }) => id),
    ).toEqual(["nav-org-billing-setup", "nav-org-billing-payment-method"]);
  });
});
