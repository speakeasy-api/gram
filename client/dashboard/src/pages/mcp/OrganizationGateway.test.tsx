import userEvent from "@testing-library/user-event";
import { TooltipProvider } from "@/components/ui/Tooltip";
import {
  cleanup,
  fireEvent,
  render as rtlRender,
  screen,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MemoryRouter, useSearchParams } from "react-router";
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
  orgRead: true,
  permissionsLoading: false,
  gatewayLoading: false,
  available: true,
  on: false as boolean | undefined,
}));
vi.mock("@/components/sessions/ClientsAndSessionsTab", () => ({
  ClientsAndSessionsTab: ({
    gateway,
  }: {
    gateway?: { projectId?: string };
  }) => {
    const [params] = useSearchParams();
    return (
      <div>
        Shared gateway connections<output>{params.toString()}</output>
        <span aria-label="Connections scope">{gateway?.projectId}</span>
      </div>
    );
  },
}));
vi.mock("./gateway/GatewayActivitySection", () => ({
  GatewayActivitySection: ({
    scope,
  }: {
    scope: { kind: string; projectId?: string };
  }) => (
    <div>
      Project gateway activity
      <output aria-label="Activity scope">
        {scope.kind}:{scope.projectId}
      </output>
    </div>
  ),
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
    hasScope: (scope: string, resourceId: string) =>
      (scope === "org:admin" && resourceId === "test-org" && state.admin) ||
      (scope === "org:read" && resourceId === "test-org" && state.orgRead) ||
      scope === "project:read",
    isLoading: state.permissionsLoading,
  }),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "test-org", slug: "test" }),
  useProject: () => ({ id: "test-project", slug: "sales", name: "Sales" }),
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
vi.mock("@gram/client/react-query/projectGateway.js", () => ({
  useProjectGateway: () => ({
    isLoading: state.gatewayLoading,
    isFetching: false,
    error: null,
    refetch: vi.fn(),
    data:
      state.on && state.available
        ? {
            available: true,
            url: "https://api.example.test/mcp/gateway/test/projects/sales",
          }
        : { available: false, unavailableReason: "turned_off" },
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
vi.mock("@/components/mcp/ProjectGatewayCard", () => ({
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
import OrganizationGateway, { ProjectGatewayPage } from "./OrganizationGateway";
afterEach(cleanup);
beforeEach(() => {
  state.error = false;
  state.admin = true;
  state.orgRead = true;
  state.permissionsLoading = false;
  state.gatewayLoading = false;
  state.available = true;
  state.on = false;
  vi.clearAllMocks();
});

it("confirms before turning the gateway on", () => {
  render(<OrganizationGateway />);
  fireEvent.click(screen.getByRole("switch", { name: "Organization Gateway" }));
  expect(state.setOn).not.toHaveBeenCalled();
  expect(screen.getByText(/recommend reviewing MCP assignments/)).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Turn on" }));
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
    render(<ProjectGatewayPage />);
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
  render(<ProjectGatewayPage />);
  expect(screen.getByRole("region", { name: "Gateway endpoint" })).toBeTruthy();
  expect(
    screen.getByText(
      "https://api.example.test/mcp/gateway/test/projects/sales",
    ),
  ).toBeTruthy();
  expect(screen.getByRole("button", { name: "Copy endpoint" })).toBeTruthy();
  await userEvent.click(screen.getByRole("tab", { name: "View assignments" }));
  expect(
    screen.getByText(
      "https://api.example.test/mcp/gateway/test/projects/sales",
    ),
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
  const { rerender } = render(<ProjectGatewayPage />);
  state.on = false;
  rerender(<ProjectGatewayPage />);
  expect(
    screen
      .getByRole("tab", { name: "View assignments" })
      .getAttribute("aria-selected"),
  ).toBe("true");
  await userEvent.click(screen.getByRole("tab", { name: "Install" }));
  rerender(<ProjectGatewayPage />);
  expect(
    screen.getByRole("tab", { name: "Install" }).getAttribute("aria-selected"),
  ).toBe("true");
});

it("keeps non-admin members on install after the disabled setting loads", async () => {
  state.admin = false;
  state.on = undefined;
  const { rerender } = render(<ProjectGatewayPage />);
  state.on = false;
  rerender(<ProjectGatewayPage />);
  expect(
    screen.getByRole("tab", { name: "Install" }).getAttribute("aria-selected"),
  ).toBe("true");
  expect(screen.getByText(/An organization admin must turn on/)).toBeTruthy();
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
  const { rerender } = render(<ProjectGatewayPage />);
  expect(
    screen.getByRole("tab", { name: "Install" }).getAttribute("aria-selected"),
  ).toBe("true");
  state.permissionsLoading = false;
  rerender(<ProjectGatewayPage />);
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
    render(<ProjectGatewayPage />);
    expect(screen.queryByText(/Turn on gateways to get/)).toBeNull();
    expect(
      screen.getByText(
        loading
          ? /Loading Project Gateway installation/
          : /installation is unavailable/,
      ),
    ).toBeTruthy();
  },
);

it("offers shared connection history to non-admins while the gateway is off", async () => {
  state.on = false;
  state.admin = false;
  render(<ProjectGatewayPage />);
  await userEvent.click(screen.getByRole("tab", { name: "Connections" }));
  expect(screen.getByText("Shared gateway connections")).toBeTruthy();
  expect(screen.getByLabelText("Connections scope").textContent).toBe(
    "test-project",
  );
});

it("restores a shared Connections URL while the gateway is disabled", () => {
  render(<ProjectGatewayPage />, "/?tab=connections&q=example&sort=expiry");
  expect(screen.getByRole("status").textContent).toBe(
    "tab=connections&q=example&sort=expiry",
  );
  expect(screen.getByText("Shared gateway connections")).toBeTruthy();
  expect(
    screen
      .getByRole("tab", { name: "Connections" })
      .getAttribute("aria-selected"),
  ).toBe("true");
});

it("shows the project's activity to non-admins", async () => {
  state.admin = false;
  render(<ProjectGatewayPage />);
  await userEvent.click(screen.getByRole("tab", { name: "Activity" }));
  expect(screen.getByText("Project gateway activity")).toBeTruthy();
  expect(screen.getByLabelText("Activity scope").textContent).toBe(
    "project:test-project",
  );
});

it("restores a shared Activity URL", () => {
  render(<ProjectGatewayPage />, "/?tab=activity&range=7d");
  expect(
    screen.getByRole("tab", { name: "Activity" }).getAttribute("aria-selected"),
  ).toBe("true");
  expect(screen.getByText("Project gateway activity")).toBeTruthy();
});

it("lets an admin turn every gateway off from a project page", () => {
  state.on = true;
  render(<ProjectGatewayPage />);
  fireEvent.click(screen.getByRole("switch", { name: "Organization Gateway" }));
  expect(
    screen.getByText(/turns off every project's Project Gateway/),
  ).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Turn off" }));
  expect(state.setOn).toHaveBeenCalledWith(
    false,
    expect.objectContaining({ onSuccess: expect.any(Function) }),
  );
});

it("explains the project page while every gateway is off", () => {
  render(<ProjectGatewayPage />);
  expect(screen.getByText(/Gateways are off for every project/)).toBeTruthy();
});

it("points the Organization Gateway page at Project Gateways", () => {
  state.on = true;
  render(<OrganizationGateway />);
  expect(
    screen.getByText("https://api.example.test/mcp/gateway/test"),
  ).toBeTruthy();
  expect(screen.getByText("Use Project Gateways instead")).toBeTruthy();
  expect(
    screen.getByRole("button", { name: "Open Project Gateway" }),
  ).toBeTruthy();
  expect(screen.queryByRole("tab")).toBeNull();
});

it("serves a project member without organization access their project's gateway", async () => {
  state.on = true;
  state.admin = false;
  state.orgRead = false;
  render(<ProjectGatewayPage />);
  expect(
    screen.getByText(
      "https://api.example.test/mcp/gateway/test/projects/sales",
    ),
  ).toBeTruthy();
  // The switch turns every gateway on or off, so it needs organization access.
  expect(
    screen.queryByRole("switch", { name: "Organization Gateway" }),
  ).toBeNull();
  await userEvent.click(screen.getByRole("tab", { name: "Activity" }));
  expect(screen.getByText("Project gateway activity")).toBeTruthy();
});
