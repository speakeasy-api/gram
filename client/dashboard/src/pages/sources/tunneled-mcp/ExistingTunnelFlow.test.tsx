import { TooltipProvider } from "@/components/ui/Tooltip";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ExistingTunnelFlow } from "./ExistingTunnelFlow";

const state = vi.hoisted(() => ({
  tunnels: [] as TunneledMcpServer[],
  servers: [] as McpServer[],
  members: [] as { mcpServerId: string }[],
  membersError: false,
  gatewayId: null as string | null,
  mutateAsync: vi.fn(),
  createError: undefined as Error | undefined,
  onNewTunnel: vi.fn<() => void>(),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: () => true,
    hasAnyScope: () => true,
    hasAllScopes: () => true,
    isLoading: false,
  }),
}));
vi.mock("@/components/page-templates", () => ({
  FormPage: ({ title, children }: { title: string; children: unknown }) => (
    <section>
      <h1>{title}</h1>
      {children as never}
    </section>
  ),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: {
      add: { goTo: vi.fn() },
      x: {
        settings: { href: (id: string) => `/mcp/x/${id}/settings` },
      },
    },
  }),
}));
vi.mock("@/pages/mcp/gateway/useGatewayCreation", () => ({
  useGatewayCreation: () => ({
    gatewayId: state.gatewayId,
    createdServerId: null,
    attachmentError: null,
    attachmentRefused: false,
    isAttaching: false,
    complete: vi.fn(),
    retry: vi.fn(),
    cancel: () => false,
  }),
}));
vi.mock("@/pages/mcp/gateway/GatewayAttachmentStatus", () => ({
  GatewayAttachmentStatus: () => null,
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
vi.mock("@/components/user-session-issuer-select.utils", () => ({
  PROJECT_SPECIFIC_ISSUER_VALUE: "project-specific",
  defaultCreationUserSessionIssuerValue: () => "project-specific",
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
vi.mock("./ExistingTunnelCreated", () => ({
  ExistingTunnelCreated: ({ tunnel }: { tunnel: TunneledMcpServer }) => (
    <p>Created on {tunnel.name}</p>
  ),
}));
vi.mock("./hooks", () => ({
  useCreateMcpServerOnExistingTunnel: () => ({
    mutateAsync: state.mutateAsync,
    isPending: false,
    isError: state.createError !== undefined,
    error: state.createError,
    reset: vi.fn(),
  }),
  useDeleteUnusedTunnel: () => ({
    mutateAsync: vi.fn(),
    isPending: false,
    isError: false,
  }),
}));
vi.mock("@gram/client/react-query/tunneledMcpServers.js", () => ({
  useTunneledMcpServers: () => ({
    data: { tunneledMcpServers: state.tunnels },
    isSuccess: true,
    isPending: false,
    isError: false,
  }),
}));
vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  useMcpServers: () => ({
    data: { mcpServers: state.servers },
    isSuccess: true,
    isPending: false,
    isError: false,
  }),
}));
vi.mock("@gram/client/react-query/metaMcpMembers.js", () => ({
  useMetaMcpMembers: () =>
    state.membersError
      ? { data: undefined, isSuccess: false, isPending: false, isError: true }
      : {
          data: { members: state.members },
          isSuccess: true,
          isPending: false,
          isError: false,
        },
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

function tunnel(id: string, name: string): TunneledMcpServer {
  return {
    id,
    name,
    projectId: "project-1",
    keyPrefix: `gram_tun_${id}`,
    connectionStatus: "connected",
  } as TunneledMcpServer;
}

function renderFlow(requestedTunnelId: string | null = null) {
  render(
    <MemoryRouter>
      <TooltipProvider>
        <ExistingTunnelFlow
          modeSwitch={() => null}
          onNewTunnel={state.onNewTunnel}
          requestedTunnelId={requestedTunnelId}
        />
      </TooltipProvider>
    </MemoryRouter>,
  );
}

function submitButton(name = "Add server"): HTMLButtonElement {
  return screen.getByRole("button", { name }) as HTMLButtonElement;
}

beforeEach(() => {
  state.tunnels = [tunnel("t1", "JAMF"), tunnel("t2", "Okta")];
  state.servers = [
    {
      id: "s1",
      name: "JAMF prod",
      tunneledMcpServerId: "t1",
    } as McpServer,
  ];
  state.members = [];
  state.membersError = false;
  state.gatewayId = null;
  state.mutateAsync.mockReset();
  state.createError = undefined;
});

afterEach(cleanup);

describe("ExistingTunnelFlow", () => {
  it("offers creating a new tunnel when the project has none", () => {
    state.tunnels = [];
    state.servers = [];
    renderFlow();
    expect(screen.getByText("No tunnels in this project")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "New tunnel" }));
    expect(state.onNewTunnel).toHaveBeenCalledOnce();
  });

  it("adds a server to the chosen tunnel without issuing a key", async () => {
    state.mutateAsync.mockResolvedValue({
      mcpServer: { id: "s2" },
      endpointCreated: true,
    });
    renderFlow("t1");

    expect(screen.getByText("gram_tun_t1")).toBeTruthy();
    expect(screen.getByText("1 MCP server you can view:")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Display name"), {
      target: { value: "JAMF sandbox" },
    });
    await act(async () => {
      fireEvent.click(submitButton());
    });

    expect(state.mutateAsync).toHaveBeenCalledWith({
      tunneledMcpServerId: "t1",
      name: "JAMF sandbox",
      userSessionIssuerId: undefined,
    });
    expect(screen.getByText("Created on JAMF")).toBeTruthy();
  });

  it("links each tunnel's visible servers so a tunnel in use can be finished", () => {
    renderFlow();
    const link = screen.getByRole("link", { name: "JAMF prod" });
    expect(link.getAttribute("href")).toBe("/mcp/x/s1/settings");
  });

  it("refuses a requested tunnel that is not in the project", () => {
    renderFlow("missing");
    expect(
      screen.getByText(/requested tunnel is not in this project/),
    ).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Display name"), {
      target: { value: "Name" },
    });
    expect(submitButton().disabled).toBe(true);
  });

  it("disables a tunnel the target gateway already fronts", () => {
    state.gatewayId = "gateway-1";
    state.members = [{ mcpServerId: "s1" }];
    renderFlow("t1");

    expect(
      screen.getByText(
        "This gateway already includes an MCP server on this tunnel.",
      ),
    ).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Display name"), {
      target: { value: "Name" },
    });
    expect(submitButton().disabled).toBe(true);
  });

  it("waits for the gateway's members before creating for a gateway", () => {
    state.gatewayId = "gateway-1";
    state.membersError = true;
    renderFlow("t2");
    fireEvent.change(screen.getByLabelText("Display name"), {
      target: { value: "Name" },
    });
    expect(submitButton().disabled).toBe(true);
    expect(screen.getByRole("button", { name: "Retry" })).toBeTruthy();
  });

  it("offers deleting only tunnels no visible server uses", () => {
    renderFlow();
    expect(
      screen.getAllByRole("button", { name: "Delete tunnel" }),
    ).toHaveLength(1);
    expect(screen.getByText("No MCP servers you can view")).toBeTruthy();
  });

  it("makes retrying an explicit choice when the outcome is unknown", async () => {
    state.mutateAsync.mockRejectedValue(new TypeError("Failed to fetch"));
    renderFlow("t1");
    fireEvent.change(screen.getByLabelText("Display name"), {
      target: { value: "JAMF sandbox" },
    });
    await act(async () => {
      fireEvent.click(submitButton());
    });

    expect(screen.getByText(/may have succeeded/)).toBeTruthy();
    // Once in the picker, once in the outcome notice, both to its settings.
    const links = screen.getAllByRole("link", { name: "JAMF prod" });
    expect(links.map((link) => link.getAttribute("href"))).toEqual([
      "/mcp/x/s1/settings",
      "/mcp/x/s1/settings",
    ]);
    expect(submitButton("Create anyway")).toBeTruthy();
  });
});
