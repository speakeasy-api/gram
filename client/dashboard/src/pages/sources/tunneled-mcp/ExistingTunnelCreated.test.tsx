import type { GatewayCreationFlow } from "@/pages/mcp/gateway/useGatewayCreation";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ExistingTunnelCreated } from "./ExistingTunnelCreated";

const nav = vi.hoisted(() => ({
  navigate: vi.fn(),
  goToOverview: vi.fn(),
}));

vi.mock("react-router", async (original) => ({
  ...(await original<typeof import("react-router")>()),
  useNavigate: () => nav.navigate,
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: {
      x: {
        overview: { goTo: nav.goToOverview },
        settings: { href: (id: string) => `/mcp/x/${id}/settings` },
        guardrails: { goTo: vi.fn() },
      },
    },
  }),
}));
vi.mock("@/components/page-templates", () => ({
  FormPage: ({
    title,
    description,
    children,
  }: {
    title: string;
    description: string;
    children: ReactNode;
  }) => (
    <section>
      <h1>{title}</h1>
      <p>{description}</p>
      {children}
    </section>
  ),
}));

const mcpServer = { id: "server-1", slug: "jamf-sandbox" } as McpServer;

function tunnel(
  connectionStatus: TunneledMcpServer["connectionStatus"],
): TunneledMcpServer {
  return {
    id: "tunnel-1",
    name: "JAMF",
    keyPrefix: "gram_tun_x",
    connectionStatus,
  } as TunneledMcpServer;
}

const flow: GatewayCreationFlow = {
  gatewayId: null,
  createdServerId: null,
  attachmentError: null,
  attachmentRefused: false,
  isAttaching: false,
  complete: vi.fn(),
  retry: vi.fn(),
  cancel: () => false,
};

function renderCreated(
  connectionStatus: TunneledMcpServer["connectionStatus"],
  endpointCreated = true,
) {
  render(
    <MemoryRouter>
      <ExistingTunnelCreated
        mcpServer={mcpServer}
        tunnel={tunnel(connectionStatus)}
        endpointCreated={endpointCreated}
        flow={flow}
        guardrailOutcome={null}
      />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  nav.navigate.mockReset();
  nav.goToOverview.mockReset();
});

afterEach(cleanup);

describe("ExistingTunnelCreated", () => {
  it.each(["connected", "inactive", "never_connected"] as const)(
    "links to the tunnel's agent setup when the agent is %s",
    (status) => {
      renderCreated(status);
      fireEvent.click(screen.getByRole("button", { name: "Agent setup" }));
      expect(nav.navigate).toHaveBeenCalledWith(
        "/mcp/x/jamf-sandbox/settings#agent-setup",
      );
    },
  );

  it("issues no key and says the existing agent serves the server", () => {
    renderCreated("never_connected");
    expect(screen.getByText(/No new key or agent is needed/)).toBeTruthy();
    expect(screen.getByText(/No tunnel agent has connected yet/)).toBeTruthy();
    expect(screen.queryByText(/Tunnel key/i)).toBeNull();
    expect(screen.queryByText("gram_tun_x")).toBeNull();
  });

  it("opens the new server", () => {
    renderCreated("connected");
    fireEvent.click(screen.getByRole("button", { name: "Open MCP server" }));
    expect(nav.goToOverview).toHaveBeenCalledWith("jamf-sandbox");
  });

  it("points to Server URL settings when the endpoint was not created", () => {
    renderCreated("connected", false);
    fireEvent.click(
      screen.getByRole("button", { name: "Open Server URL settings" }),
    );
    expect(nav.navigate).toHaveBeenCalledWith(
      "/mcp/x/jamf-sandbox/settings#server-url",
    );
  });
});
