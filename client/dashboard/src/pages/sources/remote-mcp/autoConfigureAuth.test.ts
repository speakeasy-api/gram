import type { Gram } from "@gram/client";
import type {
  McpServer,
  RemoteMcpServer,
  RemoteSessionIssuer,
} from "@gram/client/models/components";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { proxyRegisterUpstreamClient } from "@/lib/proxyRegisterUpstreamClient";

import { autoConfigureRemoteMcpAuth } from "./autoConfigureAuth";

vi.mock("@/lib/proxyRegisterUpstreamClient", () => ({
  proxyRegisterUpstreamClient: vi.fn(),
}));

const proxyRegisterMock = vi.mocked(proxyRegisterUpstreamClient);

describe("autoConfigureRemoteMcpAuth", () => {
  beforeEach(() => {
    vi.resetAllMocks();
    proxyRegisterMock.mockResolvedValue({
      clientId: "client-from-dcr",
      clientSecret: "secret-from-dcr",
      tokenEndpointAuthMethod: "client_secret_post",
    });
  });

  it("reuses a project issuer before an org issuer and stores resource scopes", async () => {
    const orgIssuer = remoteSessionIssuer({
      id: "org-issuer",
      projectId: "",
      issuer: "https://idp.example.com/",
    });
    const projectIssuer = remoteSessionIssuer({
      id: "project-issuer",
      projectId: "project-1",
      issuer: "https://idp.example.com",
    });
    const client = mockClient({
      issuers: [orgIssuer, projectIssuer],
    });

    const result = await autoConfigureRemoteMcpAuth({
      client: client as unknown as Gram,
      authedFetch: vi.fn(),
      remoteMcpServer: remoteMcpServer(),
      mcpServer: mcpServer(),
    });

    expect(result.status).toBe("configured");
    expect(client.remoteSessionIssuers.create).not.toHaveBeenCalled();
    expect(proxyRegisterMock).toHaveBeenCalledWith(expect.any(Function), {
      registrationEndpoint: "https://idp.example.com/register",
      scope: "resource.read resource.write",
      tokenEndpointAuthMethod: "client_secret_post",
    });
    expect(client.remoteSessionClients.create).toHaveBeenCalledWith({
      createRemoteSessionClientForm: expect.objectContaining({
        remoteSessionIssuerId: "project-issuer",
        userSessionIssuerId: "user-session-issuer-1",
        clientId: "client-from-dcr",
        clientSecret: "secret-from-dcr",
        tokenEndpointAuthMethod: "client_secret_post",
        scope: ["resource.read", "resource.write"],
      }),
    });
    expect(client.mcpServers.update).toHaveBeenCalledWith({
      updateMcpServerForm: expect.objectContaining({
        id: "mcp-server-1",
        visibility: "private",
        userSessionIssuerId: "user-session-issuer-1",
      }),
    });
  });

  it("creates an issuer when no matching issuer exists", async () => {
    const client = mockClient({ issuers: [] });

    const result = await autoConfigureRemoteMcpAuth({
      client: client as unknown as Gram,
      authedFetch: vi.fn(),
      remoteMcpServer: remoteMcpServer(),
      mcpServer: mcpServer(),
    });

    expect(result.status).toBe("configured");
    expect(client.remoteSessionIssuers.create).toHaveBeenCalledWith({
      createRemoteSessionIssuerForm: expect.objectContaining({
        issuer: "https://idp.example.com",
        authorizationEndpoint: "https://idp.example.com/authorize",
        tokenEndpoint: "https://idp.example.com/token",
        registrationEndpoint: "https://idp.example.com/register",
      }),
    });
    expect(client.remoteSessionClients.create).toHaveBeenCalledWith({
      createRemoteSessionClientForm: expect.objectContaining({
        remoteSessionIssuerId: "created-issuer",
      }),
    });
  });

  it("does not normalize issuer matches beyond trailing slashes", async () => {
    const client = mockClient({
      issuers: [
        remoteSessionIssuer({
          id: "space-prefixed-issuer",
          projectId: "project-1",
          issuer: " https://idp.example.com",
        }),
      ],
    });

    const result = await autoConfigureRemoteMcpAuth({
      client: client as unknown as Gram,
      authedFetch: vi.fn(),
      remoteMcpServer: remoteMcpServer(),
      mcpServer: mcpServer(),
    });

    expect(result.status).toBe("configured");
    expect(client.remoteSessionIssuers.create).toHaveBeenCalled();
    expect(client.remoteSessionClients.create).toHaveBeenCalledWith({
      createRemoteSessionClientForm: expect.objectContaining({
        remoteSessionIssuerId: "created-issuer",
      }),
    });
  });

  it("skips when issuer discovery does not advertise DCR", async () => {
    const client = mockClient({
      issuerDraft: {
        issuer: "https://idp.example.com",
        authorizationEndpoint: "https://idp.example.com/authorize",
        tokenEndpoint: "https://idp.example.com/token",
        scopesSupported: ["profile"],
        tokenEndpointAuthMethodsSupported: ["client_secret_basic"],
        oidc: false,
        passthrough: false,
        discoveryWarnings: [],
      },
    });

    const result = await autoConfigureRemoteMcpAuth({
      client: client as unknown as Gram,
      authedFetch: vi.fn(),
      remoteMcpServer: remoteMcpServer(),
      mcpServer: mcpServer(),
    });

    expect(result).toEqual({
      status: "skipped",
      message:
        "OAuth metadata was found, but automatic authentication setup requires dynamic client registration.",
      warn: true,
    });
    expect(proxyRegisterMock).not.toHaveBeenCalled();
    expect(client.userSessionIssuers.create).not.toHaveBeenCalled();
    expect(client.mcpServers.update).not.toHaveBeenCalled();
  });

  it("silently skips when protected-resource metadata is unavailable", async () => {
    const client = mockClient({
      protectedResource: {
        available: false,
        discoveryWarnings: [],
      },
    });

    const result = await autoConfigureRemoteMcpAuth({
      client: client as unknown as Gram,
      authedFetch: vi.fn(),
      remoteMcpServer: remoteMcpServer(),
      mcpServer: mcpServer(),
    });

    expect(result).toEqual({
      status: "skipped",
      message: "No OAuth protected-resource metadata was discovered.",
      warn: false,
    });
    expect(client.remoteSessionIssuers.discover).not.toHaveBeenCalled();
  });
});

function mockClient({
  issuers = [],
  protectedResource = {
    available: true,
    discoveryWarnings: [],
    metadata: {
      authorizationServers: ["https://idp.example.com"],
      scopesSupported: ["resource.read", "resource.write"],
    },
  },
  issuerDraft = {
    issuer: "https://idp.example.com",
    authorizationEndpoint: "https://idp.example.com/authorize",
    tokenEndpoint: "https://idp.example.com/token",
    registrationEndpoint: "https://idp.example.com/register",
    scopesSupported: ["profile", "email"],
    tokenEndpointAuthMethodsSupported: ["client_secret_post"],
    oidc: false,
    passthrough: false,
    discoveryWarnings: [],
  },
}: {
  issuers?: RemoteSessionIssuer[];
  protectedResource?: unknown;
  issuerDraft?: unknown;
} = {}) {
  return {
    remoteMcp: {
      discoverProtectedResourceMetadata: vi
        .fn()
        .mockResolvedValue(protectedResource),
    },
    remoteSessionIssuers: {
      discover: vi.fn().mockResolvedValue(issuerDraft),
      list: vi.fn().mockResolvedValue(issuerPageIterator(issuers)),
      create: vi.fn().mockResolvedValue(
        remoteSessionIssuer({
          id: "created-issuer",
          projectId: "project-1",
          issuer: "https://idp.example.com",
        }),
      ),
      delete: vi.fn().mockResolvedValue(undefined),
    },
    userSessionIssuers: {
      create: vi.fn().mockResolvedValue({ id: "user-session-issuer-1" }),
      delete: vi.fn().mockResolvedValue(undefined),
    },
    remoteSessionClients: {
      create: vi.fn().mockResolvedValue({ id: "remote-session-client-1" }),
    },
    mcpServers: {
      update: vi.fn().mockResolvedValue({
        ...mcpServer(),
        visibility: "private",
        userSessionIssuerId: "user-session-issuer-1",
      }),
    },
  };
}

function issuerPageIterator(items: RemoteSessionIssuer[]) {
  return {
    result: { items },
    next: () => null,
    [Symbol.asyncIterator]: async function* () {
      yield { result: { items }, next: () => null };
    },
  };
}

function remoteSessionIssuer(
  overrides: Partial<RemoteSessionIssuer>,
): RemoteSessionIssuer {
  return {
    id: overrides.id ?? "issuer-1",
    projectId: overrides.projectId ?? "project-1",
    organizationId: "org-1",
    slug: "issuer",
    issuer: overrides.issuer ?? "https://idp.example.com",
    authorizationEndpoint:
      overrides.authorizationEndpoint ?? "https://idp.example.com/authorize",
    tokenEndpoint: overrides.tokenEndpoint ?? "https://idp.example.com/token",
    registrationEndpoint:
      overrides.registrationEndpoint ?? "https://idp.example.com/register",
    scopesSupported: [],
    grantTypesSupported: [],
    responseTypesSupported: [],
    tokenEndpointAuthMethodsSupported: [],
    oidc: false,
    passthrough: false,
    createdAt: new Date(0),
    updatedAt: new Date(0),
  };
}

function remoteMcpServer(): RemoteMcpServer {
  return {
    id: "remote-mcp-server-1",
    projectId: "project-1",
    url: "https://remote.example.com/mcp",
    transportType: "streamable-http",
    headers: [],
    createdAt: new Date(0),
    updatedAt: new Date(0),
  };
}

function mcpServer(): McpServer {
  return {
    id: "mcp-server-1",
    projectId: "project-1",
    name: "Remote server",
    slug: "remote-server",
    remoteMcpServerId: "remote-mcp-server-1",
    visibility: "disabled",
    createdAt: new Date(0),
    updatedAt: new Date(0),
  };
}
