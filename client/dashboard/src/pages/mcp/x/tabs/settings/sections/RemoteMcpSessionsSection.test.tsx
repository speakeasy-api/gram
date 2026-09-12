import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { RemoteMcpSessionsSection } from "./RemoteMcpSessionsSection";

const mocks = vi.hoisted(() => ({
  clients: vi.fn(),
  issuer: vi.fn(),
}));

vi.mock("@gram/client/react-query/userSessionIssuer.js", () => ({
  useUserSessionIssuer: (...args: unknown[]) => mocks.issuer(...args),
}));
vi.mock("./authentication/useAllRemoteSessionClients", () => ({
  useAllRemoteSessionClients: (...args: unknown[]) => mocks.clients(...args),
}));
vi.mock("./authentication/UserIdentitySessionControls", () => ({
  UserIdentitySessionControls: () => (
    <div>
      <span>Session length</span>
      <span>Client access</span>
    </div>
  ),
}));

const mcpServer = {
  id: "mcp-server-1",
  projectId: "project-1",
  remoteMcpServerId: "remote-source-1",
  userSessionIssuerId: "user-session-issuer-1",
  visibility: "private",
  createdAt: new Date(0),
  updatedAt: new Date(0),
} as McpServer;

beforeEach(() => {
  mocks.issuer.mockReturnValue({
    data: { id: "user-session-issuer-1" },
    isLoading: false,
    isError: false,
  });
  mocks.clients.mockReturnValue({
    items: [],
    isLoading: false,
    isError: false,
  });
});

afterEach(cleanup);

describe("RemoteMcpSessionsSection", () => {
  it("shows existing session controls for User Identity", () => {
    mocks.clients.mockReturnValue({
      items: [{ id: "remote-session-client-1" }],
      isLoading: false,
      isError: false,
    });

    render(<RemoteMcpSessionsSection mcpServer={mcpServer} />);

    expect(screen.getByText("Session length")).toBeDefined();
    expect(screen.getByText("Client access")).toBeDefined();
  });

  it("does not present User Identity controls for other modes", () => {
    render(<RemoteMcpSessionsSection mcpServer={mcpServer} />);

    expect(
      screen.getByText(/apply when User Identity is configured/i),
    ).toBeDefined();
    expect(screen.queryByText("Session length")).toBeNull();
    expect(screen.queryByText("Client access")).toBeNull();
  });

  it("shows the non-User-Identity state when the server has no issuer", () => {
    render(
      <RemoteMcpSessionsSection
        mcpServer={{ ...mcpServer, userSessionIssuerId: undefined }}
      />,
    );

    expect(
      screen.getByText(/apply when User Identity is configured/i),
    ).toBeDefined();
    expect(screen.queryByText(/could not be loaded/i)).toBeNull();
  });

  it("keeps controls mounted during a background client refetch", () => {
    mocks.clients.mockReturnValue({
      items: [{ id: "remote-session-client-1" }],
      isLoading: true,
      isError: false,
    });

    render(<RemoteMcpSessionsSection mcpServer={mcpServer} />);

    expect(screen.getByText("Session length")).toBeDefined();
    expect(screen.queryByText(/Loading session settings/i)).toBeNull();
    expect(mocks.issuer).toHaveBeenCalledWith(
      { id: "user-session-issuer-1" },
      undefined,
      { enabled: true, throwOnError: false },
    );
  });
});
