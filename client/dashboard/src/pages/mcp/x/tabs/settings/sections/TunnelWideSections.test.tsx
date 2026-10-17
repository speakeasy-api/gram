import type { SharedTunnelImpactState } from "@/components/mcp/use-shared-tunnel-impact";
import { TooltipProvider } from "@/components/ui/Tooltip";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { STORED_VALUE_CHANGED_MESSAGE } from "./EditableSourceFieldSection";
import { AgentSetupSection } from "./AgentSetupSection";
import { PublicAccessSection } from "./PublicAccessSection";
import { PublicRateLimitsSection } from "./PublicRateLimitsSection";
import { ResourceIdentifierSection } from "./ResourceIdentifierSection";
import { TunnelKeySection } from "./TunnelKeySection";

// Every tunnel-wide control reads the shared tunnel's servers when its
// confirmation opens; these tests drive that read's state per control.
const state = vi.hoisted(() => ({
  impact: {} as SharedTunnelImpactState,
  activeCalls: [] as boolean[],
  updateTunnel: vi.fn(),
  rotate: vi.fn(),
  invalidate: vi.fn(),
  rateTunnel: undefined as TunneledMcpServer | undefined,
  tunneledMcpEnabled: true,
}));

vi.mock("@/components/mcp/use-shared-tunnel-impact", async (original) => ({
  ...(await original<
    typeof import("@/components/mcp/use-shared-tunnel-impact")
  >()),
  useSharedTunnelImpact: (_id: string, { active }: { active: boolean }) => {
    state.activeCalls.push(active);
    return state.impact;
  },
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: () => true,
    hasAnyScope: () => true,
    hasAllScopes: () => true,
    isLoading: false,
  }),
}));
vi.mock("@gram/client/react-query/updateTunneledMcpServer.js", () => ({
  useUpdateTunneledMcpServerMutation: () => ({
    mutateAsync: state.updateTunnel,
    reset: vi.fn(),
    isPending: false,
    isError: false,
  }),
}));
vi.mock("@/pages/sources/tunneled-mcp/hooks", () => ({
  useRotateTunneledMcpServerKey: () => ({
    mutateAsync: state.rotate,
    reset: vi.fn(),
    isPending: false,
  }),
}));
vi.mock("./sourceInvalidation", () => ({
  invalidateTunneledMcpSourceViews: state.invalidate,
}));
vi.mock("@/pages/sources/tunneled-mcp/TunneledMcpSetupTabs", () => ({
  TunneledMcpSetupTabs: () => null,
}));
vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ isFeatureEnabled: () => state.tunneledMcpEnabled }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: { add: { tunneled: { href: () => "/mcp/add/tunneled" } } },
  }),
}));
vi.mock("@gram/client/react-query/getTunneledMcpServer.js", () => ({
  useGetTunneledMcpServer: () => ({ data: state.rateTunnel }),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const tunnel = {
  id: "tunnel-1",
  projectId: "project-1",
  name: "JAMF",
  keyPrefix: "gram_tun_x",
  allowPublic: false,
  resourceIdentifier: "",
} as TunneledMcpServer;

function server(id: string, visibility: McpServer["visibility"]): McpServer {
  return { id, name: `Server ${id}`, visibility } as McpServer;
}

function setImpact(overrides: Partial<SharedTunnelImpactState>) {
  state.impact = {
    servers: [server("a", "private"), server("b", "public")],
    isReady: true,
    isLoading: false,
    isError: false,
    retry: vi.fn<() => void>(),
    ...overrides,
  };
}

function wrapped(children: ReactNode) {
  return (
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>
        <TooltipProvider>{children}</TooltipProvider>
      </MemoryRouter>
    </QueryClientProvider>
  );
}

function renderWith(children: ReactNode) {
  const view = render(wrapped(children));
  return { rerender: (next: ReactNode) => view.rerender(wrapped(next)) };
}

function dialogButton(name: string): HTMLButtonElement {
  return within(screen.getByRole("dialog")).getByRole("button", {
    name,
  }) as HTMLButtonElement;
}

function button(name: string): HTMLButtonElement {
  return screen.getByRole("button", { name }) as HTMLButtonElement;
}

beforeEach(() => {
  setImpact({});
  state.activeCalls = [];
  state.updateTunnel.mockReset();
  state.rotate.mockReset();
  state.invalidate.mockReset();
  state.invalidate.mockResolvedValue(undefined);
  state.rateTunnel = undefined;
  state.tunneledMcpEnabled = true;
});

afterEach(cleanup);

describe("TunnelKeySection", () => {
  it("reads the tunnel's servers only once the rotate dialog opens", () => {
    renderWith(<TunnelKeySection tunneledMcpServer={tunnel} mcpServerId="a" />);
    expect(state.activeCalls.every((active) => !active)).toBe(true);
    fireEvent.click(button("Rotate key"));
    expect(state.activeCalls.at(-1)).toBe(true);
    expect(screen.getByText("Server b")).toBeTruthy();
  });

  it("keeps Rotate disabled until that read lands, and offers a retry on failure", () => {
    setImpact({ isReady: false, isError: true });
    renderWith(<TunnelKeySection tunneledMcpServer={tunnel} mcpServerId="a" />);
    fireEvent.click(button("Rotate key"));
    expect(button("Rotate").disabled).toBe(true);
    fireEvent.click(button("Retry"));
    expect(state.impact.retry).toHaveBeenCalledOnce();
  });

  it("warns about public servers the user may not see when the tunnel allows public access", () => {
    setImpact({ servers: [server("a", "private")] });
    renderWith(
      <TunnelKeySection
        tunneledMcpServer={{ ...tunnel, allowPublic: true }}
        mcpServerId="a"
      />,
    );
    fireEvent.click(button("Rotate key"));
    expect(screen.getByText(/bypass per-tool access control/)).toBeTruthy();
  });

  it("omits the public warning when the tunnel cannot serve anonymous callers", () => {
    setImpact({ servers: [server("a", "private")] });
    renderWith(<TunnelKeySection tunneledMcpServer={tunnel} mcpServerId="a" />);
    fireEvent.click(button("Rotate key"));
    expect(screen.queryByText(/bypass per-tool access control/)).toBeNull();
  });
});

describe("PublicAccessSection", () => {
  it("always warns about public siblings when enabling public access", () => {
    setImpact({ servers: [server("a", "private")] });
    renderWith(
      <PublicAccessSection tunneledMcpServer={tunnel} mcpServerId="a" />,
    );
    fireEvent.click(button("Enable public access"));
    expect(screen.getByText(/bypass per-tool access control/)).toBeTruthy();
  });

  it("keeps Enable disabled while the servers are loading", () => {
    setImpact({ isReady: false, isLoading: true });
    renderWith(
      <PublicAccessSection tunneledMcpServer={tunnel} mcpServerId="a" />,
    );
    fireEvent.click(button("Enable public access"));
    fireEvent.change(
      screen.getByLabelText("Type ALLOW PUBLIC ACCESS to confirm"),
      { target: { value: "ALLOW PUBLIC ACCESS" } },
    );
    const enable = screen
      .getAllByRole("button", { name: "Enable public access" })
      .at(-1) as HTMLButtonElement;
    expect(enable.disabled).toBe(true);
  });

  function disableButton(): HTMLButtonElement {
    return screen
      .getAllByRole("button", { name: "Disable public access" })
      .at(-1) as HTMLButtonElement;
  }

  it("keeps Disable disabled while the servers are loading", () => {
    setImpact({ isReady: false, isLoading: true });
    renderWith(
      <PublicAccessSection
        tunneledMcpServer={{ ...tunnel, allowPublic: true }}
        mcpServerId="a"
      />,
    );
    fireEvent.click(button("Disable public access"));
    expect(disableButton().disabled).toBe(true);
  });

  it("still allows disabling when the servers could not be read", async () => {
    setImpact({ isReady: false, isLoading: false, isError: true });
    state.updateTunnel.mockResolvedValue({});
    renderWith(
      <PublicAccessSection
        tunneledMcpServer={{ ...tunnel, allowPublic: true }}
        mcpServerId="a"
      />,
    );
    fireEvent.click(button("Disable public access"));
    expect(screen.getByRole("button", { name: "Retry" })).toBeTruthy();
    expect(disableButton().disabled).toBe(false);
    await act(async () => {
      fireEvent.click(disableButton());
    });
    expect(state.updateTunnel).toHaveBeenCalledWith({
      request: {
        updateTunneledMcpServerForm: { id: "tunnel-1", allowPublic: false },
      },
    });
  });

  it("keeps Enable disabled when the servers could not be read", () => {
    setImpact({ isReady: false, isLoading: false, isError: true });
    renderWith(
      <PublicAccessSection tunneledMcpServer={tunnel} mcpServerId="a" />,
    );
    fireEvent.click(button("Enable public access"));
    fireEvent.change(
      screen.getByLabelText("Type ALLOW PUBLIC ACCESS to confirm"),
      { target: { value: "ALLOW PUBLIC ACCESS" } },
    );
    const enable = screen
      .getAllByRole("button", { name: "Enable public access" })
      .at(-1) as HTMLButtonElement;
    expect(enable.disabled).toBe(true);
  });
});

describe("ResourceIdentifierSection", () => {
  it("confirms the frozen value against the tunnel's servers before saving", async () => {
    state.updateTunnel.mockResolvedValue({
      resourceIdentifier: "https://mcp.internal.example.com/mcp",
    });
    renderWith(
      <ResourceIdentifierSection tunneledMcpServer={tunnel} mcpServerId="a" />,
    );
    fireEvent.change(screen.getByLabelText("Protected resource identifier"), {
      target: { value: "https://mcp.internal.example.com/mcp" },
    });
    fireEvent.click(button("Save"));

    expect(state.updateTunnel).not.toHaveBeenCalled();
    expect(screen.getByText("Change resource identifier?")).toBeTruthy();
    expect(
      screen.getByText("https://mcp.internal.example.com/mcp"),
    ).toBeTruthy();

    await act(async () => {
      fireEvent.click(button("Save identifier"));
    });
    expect(state.updateTunnel).toHaveBeenCalledWith({
      request: {
        updateTunneledMcpServerForm: {
          id: "tunnel-1",
          resourceIdentifier: "https://mcp.internal.example.com/mcp",
        },
      },
    });
  });

  it("does not save while the servers on the tunnel are unknown", () => {
    setImpact({ isReady: false, isError: true });
    renderWith(
      <ResourceIdentifierSection tunneledMcpServer={tunnel} mcpServerId="a" />,
    );
    fireEvent.change(screen.getByLabelText("Protected resource identifier"), {
      target: { value: "https://mcp.internal.example.com/mcp" },
    });
    fireEvent.click(button("Save"));
    expect(button("Save identifier").disabled).toBe(true);
  });

  it("refuses to save when the identifier changed while confirming", () => {
    const { rerender } = renderWith(
      <ResourceIdentifierSection tunneledMcpServer={tunnel} mcpServerId="a" />,
    );
    fireEvent.change(screen.getByLabelText("Protected resource identifier"), {
      target: { value: "https://mcp.internal.example.com/mcp" },
    });
    fireEvent.click(button("Save"));

    rerender(
      <ResourceIdentifierSection
        tunneledMcpServer={{
          ...tunnel,
          resourceIdentifier: "https://other.example.com/mcp",
        }}
        mcpServerId="a"
      />,
    );
    fireEvent.click(button("Save identifier"));

    expect(state.updateTunnel).not.toHaveBeenCalled();
    expect(screen.getByText(STORED_VALUE_CHANGED_MESSAGE)).toBeTruthy();
  });
});

describe("PublicRateLimitsSection", () => {
  function requestLimit(value: string) {
    fireEvent.change(screen.getByLabelText("Requests per second"), {
      target: { value },
    });
    fireEvent.click(button("Save limit"));
  }

  it("keeps the confirmation locked until the saved limit is read back", async () => {
    state.rateTunnel = tunnel;
    state.updateTunnel.mockResolvedValue({});
    let finishRefetch: () => void = () => {};
    state.invalidate.mockReturnValue(
      new Promise<void>((done) => {
        finishRefetch = done;
      }),
    );
    renderWith(
      <PublicRateLimitsSection
        tunneledMcpServerId="tunnel-1"
        projectId="project-1"
        mcpServerId="a"
      />,
    );
    requestLimit("10");

    await act(async () => {
      fireEvent.click(dialogButton("Save limit"));
    });
    expect(state.updateTunnel).toHaveBeenCalledOnce();
    expect(dialogButton("Saving").disabled).toBe(true);
    expect(dialogButton("Cancel").disabled).toBe(true);

    await act(async () => finishRefetch());
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("refuses to save when the limit changed while confirming", () => {
    state.rateTunnel = tunnel;
    const { rerender } = renderWith(
      <PublicRateLimitsSection
        tunneledMcpServerId="tunnel-1"
        projectId="project-1"
        mcpServerId="a"
      />,
    );
    requestLimit("10");

    state.rateTunnel = { ...tunnel, publicRequestRatePerSecond: 20 };
    rerender(
      <PublicRateLimitsSection
        tunneledMcpServerId="tunnel-1"
        projectId="project-1"
        mcpServerId="a"
      />,
    );
    fireEvent.click(dialogButton("Save limit"));

    expect(state.updateTunnel).not.toHaveBeenCalled();
    expect(screen.getByText(STORED_VALUE_CHANGED_MESSAGE)).toBeTruthy();
  });
});

describe("AgentSetupSection", () => {
  it("links to adding another MCP server on the tunnel", () => {
    renderWith(
      <AgentSetupSection tunneledMcpServer={tunnel} mcpServerId="a" />,
    );
    const link = screen.getByRole("link", {
      name: "Add MCP server on this tunnel",
    });
    expect(link.getAttribute("href")).toBe("/mcp/add/tunneled?tunnel=tunnel-1");
  });

  it("hides that link while tunneled MCP is off for the organization", () => {
    state.tunneledMcpEnabled = false;
    renderWith(
      <AgentSetupSection tunneledMcpServer={tunnel} mcpServerId="a" />,
    );
    expect(
      screen.queryByRole("link", { name: "Add MCP server on this tunnel" }),
    ).toBeNull();
  });
});
