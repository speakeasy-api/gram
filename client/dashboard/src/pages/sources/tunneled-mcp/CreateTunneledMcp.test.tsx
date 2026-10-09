import { TooltipProvider } from "@/components/ui/Tooltip";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import CreateTunneledMcp from "./CreateTunneledMcp";

const state = vi.hoisted(() => ({
  // Resource ids the caller holds mcp:write on.
  writable: new Set<string>(),
  tunnels: [] as TunneledMcpServer[],
  tunnelListError: false,
  tunnelListCalls: 0,
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: () => true,
    hasAnyScope: (_scopes: unknown, resourceId?: string) =>
      resourceId !== undefined && state.writable.has(resourceId),
    hasAllScopes: (_scopes: unknown, resourceId?: string) =>
      resourceId !== undefined && state.writable.has(resourceId),
    isLoading: false,
  }),
}));
// The denial page offers a request-access action; only its presence matters.
vi.mock("@gram/client/react-query/requestAccess.js", () => ({
  useRequestAccessMutation: () => ({ mutate: vi.fn(), isPending: false }),
}));
vi.mock("@/contexts/Auth", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/contexts/Auth")>()),
  useProject: () => ({ id: "project-1" }),
}));
vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ isFeatureEnabled: () => true }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: { add: { href: () => "/mcp/add", goTo: vi.fn() } },
  }),
}));
vi.mock("@/components/page-templates", () => ({
  FormPage: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));
vi.mock("@/pages/mcp/gateway/useGatewayCreation", () => ({
  useGatewayCreation: () => ({
    gatewayId: null,
    createdServerId: null,
    attachmentError: null,
    attachmentRefused: false,
    isAttaching: false,
    complete: vi.fn(),
    retry: vi.fn(),
    cancel: () => false,
  }),
}));
vi.mock("@/pages/security/server-guardrails/useNewServerGuardrail", () => ({
  useNewServerGuardrail: () => ({
    validation: { ok: true },
    createFor: async () => ({ status: "skipped" }),
  }),
  guardrailFailureMessage: () => "",
}));
vi.mock("@/pages/security/server-guardrails/NewServerGuardrailSection", () => ({
  NewServerGuardrailSection: () => null,
}));
vi.mock("@/hooks/useEffectiveUserSessionIssuers", () => ({
  useEffectiveUserSessionIssuers: () => ({
    organizationIssuers: [],
    isLoading: false,
    isError: false,
  }),
}));
vi.mock("@/components/user-session-issuer-select", () => ({
  UserSessionIssuerSelect: () => null,
}));
vi.mock("./hooks", () => ({
  useCreateTunneledMcpSource: () => ({ mutateAsync: vi.fn() }),
}));
// The existing-tunnel flow has its own tests; here it only has to show the
// page's mode switch the way the real one does.
vi.mock("./ExistingTunnelFlow", () => ({
  ExistingTunnelFlow: ({
    modeSwitch,
  }: {
    modeSwitch: (disabled: boolean) => ReactNode;
  }) => (
    <div>
      {modeSwitch(false)}
      <p>Existing tunnel flow</p>
    </div>
  ),
}));
vi.mock("@gram/client/react-query/tunneledMcpServers.js", () => ({
  useTunneledMcpServers: () => {
    state.tunnelListCalls++;
    if (state.tunnelListError) return { data: undefined, isError: true };
    return { data: { tunneledMcpServers: state.tunnels }, isError: false };
  },
}));

function page() {
  return (
    <MemoryRouter initialEntries={["/mcp/add/tunneled"]}>
      <TooltipProvider>
        <CreateTunneledMcp />
      </TooltipProvider>
    </MemoryRouter>
  );
}

beforeEach(() => {
  state.writable = new Set(["project-1"]);
  state.tunnels = [{ id: "t1", name: "JAMF" } as TunneledMcpServer];
  state.tunnelListError = false;
  state.tunnelListCalls = 0;
});

afterEach(cleanup);

describe("CreateTunneledMcp", () => {
  it.each([
    ["no grants", []],
    ["write on one MCP server", ["server-1"]],
    ["write in another project", ["project-2"]],
  ])("loads nothing for a caller with %s", (_case, writable) => {
    state.writable = new Set(writable);
    render(page());
    expect(state.tunnelListCalls).toBe(0);
    expect(screen.queryByLabelText("Display name")).toBeNull();
    expect(screen.getByText("Access restricted")).toBeTruthy();
  });

  it("keeps a way back to a new tunnel after the last tunnel is deleted", () => {
    const view = render(page());
    fireEvent.click(screen.getByRole("button", { name: "Existing tunnel" }));
    expect(screen.getByText("Existing tunnel flow")).toBeTruthy();

    state.tunnels = [];
    view.rerender(page());

    fireEvent.click(screen.getByRole("button", { name: "New tunnel" }));
    expect(screen.getByLabelText("Display name")).toBeTruthy();
  });

  it("offers no mode choice when the project has no tunnels", () => {
    state.tunnels = [];
    render(page());
    expect(
      screen.queryByRole("button", { name: "Existing tunnel" }),
    ).toBeNull();
    expect(screen.getByLabelText("Display name")).toBeTruthy();
  });

  it("keeps the existing-tunnel choice when the tunnel list cannot be read", () => {
    state.tunnelListError = true;
    render(page());
    fireEvent.click(screen.getByRole("button", { name: "Existing tunnel" }));
    expect(screen.getByText("Existing tunnel flow")).toBeTruthy();
  });
});
