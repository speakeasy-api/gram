import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import IdentityConnections from "./IdentityConnections";

vi.mock("@/components/connections/ConnectionsListSection", () => ({
  ConnectionsListSection: () => null,
}));
vi.mock("@/components/observe/EmployeeShadowAISection", () => ({
  EmployeeShadowAISection: ({ userEmail }: { userEmail: string | null }) => (
    <div data-testid="shadow-ai">{userEmail}</div>
  ),
}));
vi.mock("@/components/observe/employee-data-flow", () => ({
  IdentityDataFlowGraphCard: () => null,
}));
vi.mock("@/components/observe/identity-data-flow-query", () => ({
  fetchIdentityDataFlowGraph: vi.fn(),
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("@gram/client/react-query/userSessions.js", () => ({
  useUserSessions: () => ({
    data: { result: { items: [] } },
    isError: false,
    isPending: false,
    refetch: vi.fn(),
  }),
}));
vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({
    data: { edges: [], nodes: [] },
    error: null,
    isLoading: false,
  }),
}));
vi.mock("react-router", () => ({
  useLocation: () => ({ search: "" }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({}),
  useRoutes: () => ({}),
}));
vi.mock("./identityHandoffs", () => ({
  identityHandoffs: () => ({ mcpSessions: "/sessions" }),
}));
vi.mock("./IdentityPanel", () => ({
  IdentityPanel: ({ children }: { children: React.ReactNode }) => (
    <div data-testid="active-connections">{children}</div>
  ),
}));
vi.mock("./identityRoute", () => ({
  useIdentityOutlet: () => ({
    identity: {
      displayName: "Employee",
      emails: ["employee@example.com"],
      externalUserIds: [],
      kind: "user",
      photoUrl: null,
      userIds: ["user_1"],
    },
  }),
}));
vi.mock("./IdentitySection", () => ({
  IdentitySection: ({ children }: { children: React.ReactNode }) => (
    <main>{children}</main>
  ),
}));
vi.mock("./sectionMeta", () => ({
  sectionMeta: () => "",
}));
vi.mock("./useIdentityQueries", () => ({
  retryFailed: () => vi.fn(),
  useIdentityProject: () => ({ slug: "project" }),
  useIdentityWindow: () => ({
    from: new Date("2026-08-01T00:00:00Z"),
    to: new Date("2026-09-01T00:00:00Z"),
  }),
}));

describe("IdentityConnections", () => {
  it("shows employee Shadow AI directly after active MCP connections", () => {
    render(<IdentityConnections />);

    const connections = screen.getByTestId("active-connections");
    const shadowAI = screen.getByTestId("shadow-ai");

    expect(shadowAI.textContent).toBe("employee@example.com");
    expect(
      connections.compareDocumentPosition(shadowAI) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });
});
