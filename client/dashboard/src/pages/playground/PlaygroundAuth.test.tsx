import { renderToStaticMarkup } from "react-dom/server";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Toolset } from "@/lib/toolTypes";
import type { useToolsetMcpTarget } from "@/hooks/useToolsetUrl";

type Target = ReturnType<typeof useToolsetMcpTarget>;
const mocks = vi.hoisted(() => ({
  target: {
    status: "ready" as Target["status"],
    userSessionIssuerId: undefined as string | undefined,
    serverId: "server-s",
  },
  connection: {
    connected: false,
    needsAuth: true,
    isLoading: false,
    isError: false,
    errorMessage: undefined as string | undefined,
    refetch: vi.fn(),
    connect: vi.fn(),
    canConnect: true,
  },
  issuerConnection: vi.fn(),
  sessions: vi.fn(),
}));

vi.mock("@/hooks/useToolsetUrl", () => ({
  useToolsetMcpTarget: () => mocks.target,
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: {
      details: {
        Link: ({ children }: { children: React.ReactNode }) => (
          <a>{children}</a>
        ),
      },
    },
  }),
}));
vi.mock("@gram/client/react-query/getMcpMetadata.js", () => ({
  useGetMcpMetadata: () => ({}),
}));
vi.mock("@gram/client/react-query/listEnvironments.js", () => ({
  useListEnvironments: () => ({}),
}));
vi.mock("@/hooks/useMissingEnvironmentVariables", () => ({
  useMissingRequiredEnvVars: () => 0,
}));
vi.mock("../mcp/useEnvironmentVariables", () => ({
  useEnvironmentVariables: () => [],
}));
vi.mock("./usePlaygroundEnvironment", () => ({
  usePlaygroundEnvironment: () => ({ exists: false, storedEntries: [] }),
}));
vi.mock("./usePlaygroundIssuerConnection", () => ({
  usePlaygroundIssuerConnection: () => {
    mocks.issuerConnection();
    return mocks.connection;
  },
}));
vi.mock("@/components/sessions/AttachedUserSessions", () => ({
  ToolsetAttachedUserSessions: () => null,
}));
vi.mock("@/components/sessions/ClientsAndSessionsTab", () => ({
  ClientsAndSessionsTab: (props: {
    issuerId?: string;
    originatingMcpServerId?: string;
  }) => {
    mocks.sessions(props);
    return (
      <div>
        {props.issuerId
          ? `Sessions for ${props.issuerId}`
          : "No connections. Enable authentication"}
      </div>
    );
  },
}));

import { PlaygroundAuth } from "./PlaygroundAuth";
import { ToolsetSessionsTab } from "../mcp/ToolsetSessionsTab";

const toolset = {
  id: "toolset-t",
  slug: "legacy-t",
  name: "Test server",
  userSessionIssuerId: "issuer-t",
} as Toolset;

beforeEach(() => {
  vi.clearAllMocks();
  mocks.target.status = "ready";
  mocks.target.userSessionIssuerId = undefined;
  mocks.connection.isLoading = false;
  mocks.connection.isError = false;
  mocks.connection.errorMessage = undefined;
});

describe("authoritative MCP auth and session presentation", () => {
  it.each([
    ["idle", "Select an MCP server"],
    ["loading", "Loading MCP server authentication"],
    ["error", "Unable to load MCP server authentication"],
    ["unavailable", "This MCP server is disabled"],
  ] as const)(
    "does not make auth decisions while target is %s",
    (status, message) => {
      mocks.target.status = status;
      const auth = renderToStaticMarkup(<PlaygroundAuth toolset={toolset} />);
      const sessions = renderToStaticMarkup(
        <ToolsetSessionsTab toolset={toolset} />,
      );
      for (const html of [auth, sessions]) {
        expect(html).toContain(message);
        expect(html).not.toContain("No authentication required");
        expect(html).not.toContain("No connections");
        expect(html).not.toContain("Enable authentication");
        expect(html).not.toContain("issuer-t");
      }
      expect(mocks.issuerConnection).not.toHaveBeenCalled();
      expect(mocks.sessions).not.toHaveBeenCalled();
    },
  );

  it("renders selected server auth and sessions, not the toolset issuer", () => {
    mocks.target.userSessionIssuerId = "issuer-s";
    const auth = renderToStaticMarkup(<PlaygroundAuth toolset={toolset} />);
    const sessions = renderToStaticMarkup(
      <ToolsetSessionsTab toolset={toolset} />,
    );
    expect(auth).toContain("Not Connected");
    expect(auth).not.toContain("No authentication required");
    expect(mocks.issuerConnection).toHaveBeenCalledOnce();
    expect(sessions).toContain("Sessions for issuer-s");
    expect(sessions).not.toContain("issuer-t");
    expect(mocks.sessions).toHaveBeenCalledWith(
      expect.objectContaining({
        issuerId: "issuer-s",
        originatingMcpServerId: "server-s",
      }),
    );
  });

  it("shows ungated state only after authoritative resolution", () => {
    expect(
      renderToStaticMarkup(<PlaygroundAuth toolset={toolset} />),
    ).toContain("No authentication required");
    expect(
      renderToStaticMarkup(<ToolsetSessionsTab toolset={toolset} />),
    ).toContain("Enable authentication");
    expect(mocks.issuerConnection).not.toHaveBeenCalled();
    expect(mocks.sessions).toHaveBeenCalledWith(
      expect.objectContaining({ issuerId: undefined }),
    );
  });

  it("keeps a pending connection distinct from not connected", () => {
    mocks.target.userSessionIssuerId = "issuer-s";
    mocks.connection.isLoading = true;
    const html = renderToStaticMarkup(<PlaygroundAuth toolset={toolset} />);
    expect(html).not.toContain("Not Connected");
    expect(html).not.toContain("Connection unavailable");
  });

  it("shows probe failure rather than not connected", () => {
    mocks.target.userSessionIssuerId = "issuer-s";
    mocks.connection.isError = true;
    mocks.connection.errorMessage =
      "Unable to check the selected server connection.";
    const html = renderToStaticMarkup(<PlaygroundAuth toolset={toolset} />);
    expect(html).toContain("Connection unavailable");
    expect(html).toContain(mocks.connection.errorMessage);
    expect(html).not.toContain("Not Connected");
    expect(html).not.toContain("<button");
  });
});
