import type { Gram } from "@gram/client";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { RemoteMcpServer } from "@gram/client/models/components/remotemcpserver.js";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { configureCreatedRemoteMcpIdentity } from "./configureCreatedIdentity";

const mocks = vi.hoisted(() => ({
  createHeader: vi.fn(),
  discover: vi.fn(),
  fetchIssuer: vi.fn(),
  getIssuer: vi.fn(),
  commit: vi.fn(),
  updateServer: vi.fn(),
}));

const client = {
  remoteMcp: {
    createServerHeader: mocks.createHeader,
    discoverProtectedResourceMetadata: mocks.discover,
  },
  remoteSessionIssuers: {
    fetchMetadata: mocks.fetchIssuer,
    get: mocks.getIssuer,
  },
  remoteSessions: {
    commitServerUserIdentityConfiguration: mocks.commit,
  },
  mcpServers: { update: mocks.updateServer },
} as unknown as Gram;

beforeEach(() => {
  vi.clearAllMocks();
  mocks.updateServer.mockResolvedValue({
    ...mcpServer(),
    visibility: "private",
  });
  mocks.createHeader.mockResolvedValue({ id: "header-1" });
  mocks.discover.mockResolvedValue({
    available: true,
    metadata: {
      authorizationServers: ["https://id.example.com"],
      scopesSupported: ["resource.read"],
    },
  });
  mocks.fetchIssuer.mockResolvedValue({
    issuer: "https://id.example.com",
    authorizationEndpoint: "https://id.example.com/authorize",
    tokenEndpoint: "https://id.example.com/token",
    registrationEndpoint: "https://id.example.com/register",
    tokenEndpointAuthMethodsSupported: ["client_secret_basic"],
  });
  mocks.getIssuer.mockRejectedValue(
    Object.assign(new Error("not found"), { statusCode: 404 }),
  );
  mocks.commit.mockResolvedValue({
    status: "registered",
    registrationMethod: "dcr",
    manualSetupRequired: false,
  });
});

describe("configureCreatedRemoteMcpIdentity", () => {
  it("enables No Identity without creating credentials", async () => {
    const result = await configureCreatedRemoteMcpIdentity({
      client,
      remoteMcpServer: remoteServer(),
      mcpServer: mcpServer(),
      identityMode: "none",
    });

    expect(result.status).toBe("configured");
    expect(mocks.updateServer).toHaveBeenCalledWith(
      {
        updateMcpServerForm: expect.objectContaining({
          id: "mcp-server-1",
          visibility: "private",
        }),
      },
      undefined,
      undefined,
    );
    expect(mocks.commit).not.toHaveBeenCalled();
  });

  it("stores Agent Identity before enabling the server", async () => {
    const result = await configureCreatedRemoteMcpIdentity({
      client,
      remoteMcpServer: remoteServer(),
      mcpServer: mcpServer(),
      identityMode: "agent",
      agentAuthorization: "Bearer secret",
    });

    expect(result.status).toBe("configured");
    expect(mocks.createHeader).toHaveBeenCalledWith(
      {
        createServerHeaderForm: {
          remoteMcpServerId: "remote-1",
          name: "Authorization",
          isRequired: true,
          isSecret: true,
          value: "Bearer secret",
        },
      },
      undefined,
      undefined,
    );
    expect(mocks.createHeader.mock.invocationCallOrder[0]).toBeLessThan(
      mocks.updateServer.mock.invocationCallOrder[0]!,
    );
  });

  it("reports an Agent Identity enable failure without exposing the SDK error", async () => {
    mocks.updateServer.mockRejectedValue(new Error("sensitive backend detail"));

    const result = await configureCreatedRemoteMcpIdentity({
      client,
      remoteMcpServer: remoteServer(),
      mcpServer: mcpServer(),
      identityMode: "agent",
      agentAuthorization: "Bearer secret",
    });

    expect(result).toMatchObject({
      status: "setup-required",
      message:
        "Agent Identity was configured, but the server could not be enabled. Enable it from Settings.",
    });
    expect(mocks.createHeader).toHaveBeenCalledOnce();
  });

  it("uses one atomic User Identity commit before enabling", async () => {
    const result = await configureCreatedRemoteMcpIdentity({
      client,
      remoteMcpServer: remoteServer(),
      mcpServer: mcpServer(),
      identityMode: "user",
    });

    expect(result.status).toBe("configured");
    expect(mocks.commit).toHaveBeenCalledWith(
      {
        commitServerUserIdentityConfigurationForm: expect.objectContaining({
          mcpServerId: "mcp-server-1",
          clientMode: "auto",
          createProvider: expect.objectContaining({
            issuer: "https://id.example.com",
          }),
          clientConfiguration: expect.objectContaining({
            scope: ["resource.read"],
          }),
        }),
      },
      undefined,
      undefined,
    );
    expect(mocks.commit.mock.invocationCallOrder[0]).toBeLessThan(
      mocks.updateServer.mock.invocationCallOrder[0]!,
    );
  });

  it("retains the server disabled when registration is refused", async () => {
    mocks.commit.mockResolvedValue({
      manualSetupRequired: false,
      registrationMethod: "dcr",
      failure: {
        outcome: "refused",
        reason: "authorization_rejected",
        retryable: false,
      },
    });

    const result = await configureCreatedRemoteMcpIdentity({
      client,
      remoteMcpServer: remoteServer(),
      mcpServer: mcpServer(),
      identityMode: "user",
    });

    expect(result).toMatchObject({
      status: "setup-required",
      mcpServer: { visibility: "disabled" },
      userIdentity: { failure: { outcome: "refused" } },
    });
    expect(mocks.updateServer).not.toHaveBeenCalled();
    expect(mocks.commit).toHaveBeenCalledOnce();
  });

  it("retains the server disabled when the atomic commit is rejected", async () => {
    mocks.commit.mockRejectedValue(new Error("project:write is required."));

    const result = await configureCreatedRemoteMcpIdentity({
      client,
      remoteMcpServer: remoteServer(),
      mcpServer: mcpServer(),
      identityMode: "user",
    });

    expect(result).toMatchObject({
      status: "setup-required",
      mcpServer: { visibility: "disabled" },
      message: expect.stringContaining("Settings > Identity"),
    });
    expect(mocks.updateServer).not.toHaveBeenCalled();
  });

  it("does not retry a successful registration when enabling fails", async () => {
    mocks.updateServer.mockRejectedValue(new Error("update failed"));

    const result = await configureCreatedRemoteMcpIdentity({
      client,
      remoteMcpServer: remoteServer(),
      mcpServer: mcpServer(),
      identityMode: "user",
    });

    expect(result).toMatchObject({
      status: "setup-required",
      userIdentity: { status: "registered" },
    });
    expect(mocks.commit).toHaveBeenCalledOnce();
  });
});

function remoteServer(): RemoteMcpServer {
  return {
    id: "remote-1",
    projectId: "project-1",
    url: "https://mcp.example.com/mcp",
    transportType: "streamable-http",
    createdAt: new Date(0),
    updatedAt: new Date(0),
  };
}

function mcpServer(): McpServer {
  return {
    id: "mcp-server-1",
    projectId: "project-1",
    name: "Example",
    slug: "example",
    remoteMcpServerId: "remote-1",
    userSessionIssuerId: "usi-1",
    networkAccessMode: "public_only",
    visibility: "disabled",
    createdAt: new Date(0),
    updatedAt: new Date(0),
  };
}
