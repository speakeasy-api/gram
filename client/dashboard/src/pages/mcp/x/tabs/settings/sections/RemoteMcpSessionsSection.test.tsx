import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { RemoteMcpSessionsSection } from "./RemoteMcpSessionsSection";

vi.mock("./authentication/authTarget", () => ({
  useMcpServerAuthTarget: () => ({}),
}));
vi.mock("./authentication/useSelectableUserSessionIssuers", () => ({
  useSelectableUserSessionIssuers: () => ({
    issuers: [],
    isLoading: false,
    isError: false,
  }),
}));
vi.mock("./authentication/UserSessionIssuerField", () => ({
  UserSessionIssuerField: () => <span>User session issuer</span>,
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

afterEach(cleanup);

describe("RemoteMcpSessionsSection", () => {
  it("shows only the provider picker; its settings live in MCP Sessions", () => {
    render(<RemoteMcpSessionsSection mcpServer={mcpServer} />);

    expect(screen.getByText("User session issuer")).toBeDefined();
    expect(screen.queryByText(/Sessions last/)).toBeNull();
    expect(screen.queryByText("Shared authorization server")).toBeNull();
    expect(screen.queryByText("Session length")).toBeNull();
    expect(screen.queryByText("Client access")).toBeNull();
  });
});
