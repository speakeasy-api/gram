import userEvent from "@testing-library/user-event";
import { TooltipProvider } from "@/components/ui/Tooltip";
import {
  cleanup,
  fireEvent,
  render as rtlRender,
  screen,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import type { ReactNode } from "react";

function render(ui: ReactNode, entry = "/") {
  return rtlRender(ui, {
    wrapper: ({ children }) => (
      <MemoryRouter initialEntries={[entry]}>{children}</MemoryRouter>
    ),
  });
}
const state = vi.hoisted(() => ({
  setOn: vi.fn(),
  error: false,
  admin: true,
  permissionsLoading: false,
  gatewayLoading: false,
  available: true,
  on: false as boolean | undefined,
}));
vi.mock("@/components/connections/GatewayConnectionsTab", () => ({
  GatewayConnectionsTab: () => <div>Shared gateway connections</div>,
}));
vi.mock("@/components/page-templates", () => ({
  SettingsPage: ({
    children,
    primaryAction,
    renderHeader,
  }: {
    children: ReactNode;
    primaryAction: ReactNode;
    renderHeader?: (header: ReactNode) => ReactNode;
  }) => (
    <TooltipProvider>
      <div>
        {renderHeader ? renderHeader(primaryAction) : primaryAction}
        {children}
      </div>
    </TooltipProvider>
  ),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({
    children,
    scope,
    level,
  }: {
    children: ReactNode;
    scope: string;
    level: string;
  }) => {
    if (scope !== "org:admin" || (state.admin && !state.permissionsLoading))
      return <>{children}</>;
    if (level === "component")
      return (
        <div
          aria-disabled="true"
          onClickCapture={(event) => {
            event.preventDefault();
            event.stopPropagation();
          }}
        >
          {children}
        </div>
      );
    return null;
  },
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string, organizationId: string) =>
      scope === "org:admin" && organizationId === "test-org" && state.admin,
    isLoading: state.permissionsLoading,
  }),
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
    gateway: {
      isLoading: state.gatewayLoading,
      data:
        state.on && state.available
          ? {
              available: true,
              url: "https://api.example.test/mcp/gateway/test",
            }
          : { available: false, unavailableReason: "turned_off" },
    },
    on: state.on,
    setOn: state.setOn,
    isPending: false,
  }),
}));
vi.mock(
  "@/components/mcp/organization-gateway-setting",
  async (importOriginal) => ({
    ...(await importOriginal<
      typeof import("@/components/mcp/organization-gateway-setting")
    >()),
    GatewayNotServed: () => null,
  }),
);
vi.mock("@/components/mcp/OrganizationGatewayCard", () => ({
  SpeakeasyMark: () => null,
}));
vi.mock("@/pages/setup/components/platform-instrumentation-sheet", () => ({
  PlatformInstrumentationSheet: () => null,
}));
vi.mock("../access/MCPAssignments", () => ({
  MCPAssignments: ({ onReady }: { onReady?: (ready: boolean) => void }) => (
    <button onClick={() => onReady?.(true)}>Assignments loaded</button>
  ),
}));
import OrganizationGateway from "./OrganizationGateway";
afterEach(cleanup);
beforeEach(() => {
  state.error = false;
  state.admin = true;
  state.permissionsLoading = false;
  state.gatewayLoading = false;
  state.available = true;
  state.on = false;
  vi.clearAllMocks();
});

it("routes the MCP page switch through access review before mutating", () => {
  render(<OrganizationGateway />);
  fireEvent.click(screen.getByRole("switch", { name: "Organization Gateway" }));
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
  expect(
    screen.queryByRole("switch", { name: "Organization Gateway" }),
  ).toBeNull();
  expect(screen.getByText(/load the gateway setting/)).toBeDefined();
});

it.each([false, true])(
  "shows assignments from the gateway card destination when enabled=%s",
  async (on) => {
    state.on = on;
    render(<OrganizationGateway />);
    expect(
      screen
        .getByRole("tab", { name: on ? "Install" : "View assignments" })
        .getAttribute("aria-selected"),
    ).toBe("true");
    await userEvent.click(
      screen.getByRole("tab", { name: "View assignments" }),
    );
    expect(
      screen.getByRole("button", { name: "Assignments loaded" }),
    ).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Enable Organization Gateway" }),
    ).toBeNull();
  },
);

it("keeps the copyable endpoint above both tabs", async () => {
  state.on = true;
  render(<OrganizationGateway />);
  expect(screen.getByRole("region", { name: "Gateway endpoint" })).toBeTruthy();
  expect(
    screen.getByText("https://api.example.test/mcp/gateway/test"),
  ).toBeTruthy();
  expect(screen.getByRole("button", { name: "Copy endpoint" })).toBeTruthy();
  await userEvent.click(screen.getByRole("tab", { name: "View assignments" }));
  expect(
    screen.getByText("https://api.example.test/mcp/gateway/test"),
  ).toBeTruthy();
  expect(
    screen.getByRole("tab", { name: "Install" }).getAttribute("aria-selected"),
  ).toBe("false");
  expect(
    screen.getByRole("button", { name: "Assignments loaded" }),
  ).toBeTruthy();
});

it("selects assignments after a disabled setting loads, preserving an explicit tab choice", async () => {
  state.on = undefined;
  const { rerender } = render(<OrganizationGateway />);
  state.on = false;
  rerender(<OrganizationGateway />);
  expect(
    screen
      .getByRole("tab", { name: "View assignments" })
      .getAttribute("aria-selected"),
  ).toBe("true");
  await userEvent.click(screen.getByRole("tab", { name: "Install" }));
  rerender(<OrganizationGateway />);
  expect(
    screen.getByRole("tab", { name: "Install" }).getAttribute("aria-selected"),
  ).toBe("true");
});

it("keeps non-admin members on install after the disabled setting loads", async () => {
  state.admin = false;
  state.on = undefined;
  const { rerender } = render(<OrganizationGateway />);
  state.on = false;
  rerender(<OrganizationGateway />);
  expect(
    screen.getByRole("tab", { name: "Install" }).getAttribute("aria-selected"),
  ).toBe("true");
  expect(screen.getByText(/An organization admin must enable/)).toBeTruthy();
  const gatewaySwitch = screen.getByRole("switch", {
    name: "Organization Gateway",
  });
  expect(gatewaySwitch.closest('[aria-disabled="true"]')).toBeTruthy();
  await userEvent.click(gatewaySwitch);
  expect(state.setOn).not.toHaveBeenCalled();
  expect(
    screen.queryByRole("button", { name: "Enable Organization Gateway" }),
  ).toBeNull();
  expect(
    screen.queryByRole("button", { name: "Assignments loaded" }),
  ).toBeNull();
});

it("waits for administrator permissions before defaulting to assignments", () => {
  state.permissionsLoading = true;
  const { rerender } = render(<OrganizationGateway />);
  expect(
    screen.getByRole("tab", { name: "Install" }).getAttribute("aria-selected"),
  ).toBe("true");
  state.permissionsLoading = false;
  rerender(<OrganizationGateway />);
  expect(
    screen
      .getByRole("tab", { name: "View assignments" })
      .getAttribute("aria-selected"),
  ).toBe("true");
});

it.each([true, false])(
  "shows loading or unavailable installation status while enabled (loading=%s)",
  async (loading) => {
    state.on = true;
    state.gatewayLoading = loading;
    state.available = false;
    render(<OrganizationGateway />);
    expect(screen.queryByText(/Enable Organization Gateway to get/)).toBeNull();
    expect(
      screen.getByText(
        loading
          ? /Loading Organization Gateway installation/
          : /installation is unavailable/,
      ),
    ).toBeTruthy();
  },
);

it("offers shared connection history to non-admins while the gateway is off", async () => {
  state.on = false;
  state.admin = false;
  render(<OrganizationGateway />);
  await userEvent.click(screen.getByRole("tab", { name: "Connections" }));
  expect(screen.getByText("Shared gateway connections")).toBeTruthy();
});

it("restores a shared Connections URL while the gateway is disabled", () => {
  render(<OrganizationGateway />, "/?tab=connections&q=example&sort=expiry");
  expect(screen.getByText("Shared gateway connections")).toBeTruthy();
  expect(
    screen
      .getByRole("tab", { name: "Connections" })
      .getAttribute("aria-selected"),
  ).toBe("true");
});
