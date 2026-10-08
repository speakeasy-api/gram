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
} from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { PublicAccessSection } from "./PublicAccessSection";
import { ResourceIdentifierSection } from "./ResourceIdentifierSection";
import { TunnelKeySection } from "./TunnelKeySection";

// Every tunnel-wide control reads the shared tunnel's servers when its
// confirmation opens; these tests drive that read's state per control.
const state = vi.hoisted(() => ({
  impact: {} as SharedTunnelImpactState,
  activeCalls: [] as boolean[],
  updateTunnel: vi.fn(),
  rotate: vi.fn(),
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
  invalidateTunneledMcpSourceViews: vi.fn(async () => {}),
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

function renderWith(children: ReactNode) {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>
        <TooltipProvider>{children}</TooltipProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function button(name: string): HTMLButtonElement {
  return screen.getByRole("button", { name }) as HTMLButtonElement;
}

beforeEach(() => {
  setImpact({});
  state.activeCalls = [];
  state.updateTunnel.mockReset();
  state.rotate.mockReset();
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
});
