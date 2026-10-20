import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";

const state = vi.hoisted(() => ({ setOn: vi.fn(), error: false }));
vi.mock("@/components/page-templates", () => ({
  SettingsPage: ({
    children,
    primaryAction,
  }: {
    children: ReactNode;
    primaryAction: ReactNode;
  }) => (
    <div>
      {primaryAction}
      {children}
    </div>
  ),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "test-org", slug: "test" }),
}));
vi.mock("@/hooks/useIsCurrentOrganization", () => ({
  useIsCurrentOrganization: () => () => true,
}));
vi.mock("@/components/mcp/useOrganizationGatewaySetting", () => ({
  useOrganizationGatewaySetting: () => ({
    features: { data: {}, error: state.error },
    gateway: { data: { available: false, unavailableReason: "turned_off" } },
    on: false,
    setOn: state.setOn,
    isPending: false,
  }),
}));
vi.mock("@/components/mcp/organization-gateway-setting", () => ({
  OrganizationGatewaySwitch: ({ setOn }: { setOn: (on: boolean) => void }) => (
    <button onClick={() => setOn(true)}>Turn on</button>
  ),
  GatewayNotServed: () => null,
}));
vi.mock("@/components/mcp/OrganizationGatewayCard", () => ({
  SpeakeasyMark: () => null,
}));
vi.mock("@/pages/setup/components/platform-instrumentation-sheet", () => ({
  PlatformInstrumentationSheet: () => null,
}));
vi.mock("../access/MCPAssignments", () => ({
  MCPAssignments: ({ onReady }: { onReady: (ready: boolean) => void }) => (
    <button onClick={() => onReady(true)}>Assignments loaded</button>
  ),
}));
import OrganizationGateway from "./OrganizationGateway";
afterEach(cleanup);
beforeEach(() => {
  state.error = false;
  vi.clearAllMocks();
});

it("routes the MCP page switch through access review before mutating", () => {
  render(<OrganizationGateway />);
  fireEvent.click(screen.getByRole("button", { name: "Turn on" }));
  expect(state.setOn).not.toHaveBeenCalled();
  const enable = screen.getByRole("button", {
    name: "Enable Organization Gateway",
  }) as HTMLButtonElement;
  expect(enable.disabled).toBe(true);
  fireEvent.click(screen.getByRole("button", { name: "Assignments loaded" }));
  fireEvent.click(screen.getByRole("checkbox"));
  fireEvent.click(enable);
  expect(state.setOn).toHaveBeenCalledWith(
    true,
    expect.objectContaining({ onSuccess: expect.any(Function) }),
  );
});

it("does not offer mutation when the gateway setting failed to load", () => {
  state.error = true;
  render(<OrganizationGateway />);
  expect(screen.queryByRole("button", { name: "Turn on" })).toBeNull();
  expect(screen.getByText(/load the gateway setting/)).toBeDefined();
});
