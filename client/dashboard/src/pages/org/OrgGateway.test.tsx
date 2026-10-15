import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";

const state = vi.hoisted(() => ({ mutate: vi.fn(), disabled: true }));
vi.mock("@/components/page-templates", () => ({
  SettingsPage: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "test-org" }),
}));
vi.mock("@/hooks/useIsCurrentOrganization", () => ({
  useIsCurrentOrganization: () => () => true,
}));
vi.mock("@/routes", () => ({ useOrgRoutes: () => ({}) }));
vi.mock("@tanstack/react-query", () => ({ useQueryClient: () => ({}) }));
vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: () => ({
    data: { organizationGatewayDisabled: state.disabled },
  }),
  invalidateAllProductFeatures: vi.fn(),
}));
vi.mock("@gram/client/react-query/organizationGateway.js", () => ({
  useOrganizationGateway: () => ({
    data: { available: false, unavailableReason: "turned_off" },
  }),
  invalidateAllOrganizationGateway: vi.fn(),
}));
vi.mock("@gram/client/react-query/featuresSet.js", () => ({
  useFeaturesSetMutation: () => ({ mutate: state.mutate, isPending: false }),
}));
vi.mock("./GatewayOnboarding", () => ({
  GatewayOnboarding: () => <div>Client setup cards</div>,
}));
vi.mock("../access/MCPAssignments", () => ({
  MCPAssignments: ({ onReady }: { onReady: (ready: boolean) => void }) => (
    <>
      <button onClick={() => onReady(true)}>Assignments loaded</button>
      <button onClick={() => onReady(false)}>Assignments failed</button>
    </>
  ),
}));
import OrgGateway from "./OrgGateway";

afterEach(cleanup);
beforeEach(() => {
  state.disabled = true;
  vi.clearAllMocks();
});

it("requires a completed assignment review and explicit sign-off before enabling", () => {
  render(
    <MemoryRouter>
      <OrgGateway />
    </MemoryRouter>,
  );
  expect(screen.getByText("Client setup cards")).toBeDefined();
  fireEvent.click(
    screen.getByRole("button", { name: "Review access and enable" }),
  );
  const enable = screen.getByRole("button", {
    name: "Enable Org Gateway",
  }) as HTMLButtonElement;
  expect(enable.disabled).toBe(true);
  fireEvent.click(enable);
  expect(state.mutate).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Assignments loaded" }));
  expect(enable.disabled).toBe(true);
  fireEvent.click(screen.getByRole("checkbox"));
  expect(enable.disabled).toBe(false);
  fireEvent.click(enable);
  expect(state.mutate).toHaveBeenCalledWith({
    request: {
      setProductFeatureRequestBody: {
        organizationId: "test-org",
        featureName: "organization_gateway_disabled",
        enabled: false,
      },
    },
  });
});

it("invalidates sign-off if the complete review becomes unavailable", () => {
  render(
    <MemoryRouter>
      <OrgGateway />
    </MemoryRouter>,
  );
  fireEvent.click(
    screen.getByRole("button", { name: "Review access and enable" }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Assignments loaded" }));
  fireEvent.click(screen.getByRole("checkbox"));
  fireEvent.click(screen.getByRole("button", { name: "Assignments failed" }));
  fireEvent.click(screen.getByRole("button", { name: "Assignments loaded" }));
  expect(
    (
      screen.getByRole("button", {
        name: "Enable Org Gateway",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  expect(screen.getByRole("checkbox").getAttribute("aria-checked")).toBe(
    "false",
  );
});
