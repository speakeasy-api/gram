import type { Gram } from "@gram/client";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { RemoteMcpServer } from "@gram/client/models/components/remotemcpserver.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { proxyRegisterUpstreamClient } from "@/lib/proxyRegisterUpstreamClient";

import { autoConfigureRemoteMcpAuth as autoConfigureRemoteMcpAuthImplementation } from "./autoConfigureAuth";

vi.mock("@/lib/proxyRegisterUpstreamClient", () => ({
  proxyRegisterUpstreamClient: vi.fn(),
}));

const proxyRegisterMock = vi.mocked(proxyRegisterUpstreamClient);

type AutoConfigureInput = Parameters<
  typeof autoConfigureRemoteMcpAuthImplementation
>[0];

function autoConfigureRemoteMcpAuth(
  input: Omit<AutoConfigureInput, "isPlatformAdmin" | "projectSlug"> &
    Partial<Pick<AutoConfigureInput, "isPlatformAdmin" | "projectSlug">>,
) {
  return autoConfigureRemoteMcpAuthImplementation({
    isPlatformAdmin: false,
    projectSlug: "test-project",
    ...input,
  });
}

// Which issuer an upstream URL maps to — the project's own, one inherited from
// the organization, or one from the platform catalog — is decided server-side by
// remoteSessionIssuers.get({issuer}), and a 404 there means "nothing describes
// this URL yet". These tests cover what this module still owns: the order of the
// guards, the get-then-create branch, and what happens to a created issuer when
// a later step fails. Tier precedence and URL normalization are covered
// server-side.
describe("autoConfigureRemoteMcpAuth", () => {
  beforeEach(() => {
    vi.resetAllMocks();
    proxyRegisterMock.mockResolvedValue({
      clientId: "client-from-dcr",
      clientSecret: "secret-from-dcr",
      tokenEndpointAuthMethod: "client_secret_post",
    });
  });

  it("creates an issuer from the discovered draft when none exists and attaches a client under the server's own USI", async () => {
    const client = mockClient();

    const result = await autoConfigureRemoteMcpAuth({
      client: client as unknown as Gram,
      authedFetch: vi.fn(),
      remoteMcpServer: remoteMcpServer(),
      mcpServer: mcpServer(),
    });

    expect(result).toMatchObject({
      status: "configured",
      remoteSessionIssuerId: "created-issuer",
      userSessionIssuerId: "server-usi",
    });
    // The server already owns its USI from setup — auto-config never makes one.
    expect(client.userSessionIssuers.create).not.toHaveBeenCalled();
    // The lookup is by URL, never by scanning the issuer list.
    expect(client.remoteSessionIssuers.get).toHaveBeenCalledWith(
      { issuer: "https://idp.example.com" },
      undefined,
      undefined,
    );
    expect(client.remoteSessionIssuers.create).toHaveBeenCalledWith(
      {
        createRemoteSessionIssuerForm: expect.objectContaining({
          issuer: "https://idp.example.com",
          name: "idp.example.com",
          authorizationEndpoint: "https://idp.example.com/authorize",
          tokenEndpoint: "https://idp.example.com/token",
          registrationEndpoint: "https://idp.example.com/register",
        }),
      },
      undefined,
      undefined,
    );
    expect(client.remoteSessionClients.create).toHaveBeenCalledWith(
      {
        createRemoteSessionClientForm: expect.objectContaining({
          remoteSessionIssuerId: "created-issuer",
          userSessionIssuerIds: ["server-usi"],
          clientId: "client-from-dcr",
          clientSecret: "secret-from-dcr",
          tokenEndpointAuthMethod: "client_secret_post",
        }),
      },
      undefined,
      undefined,
    );
    expect(client.mcpServers.update).toHaveBeenCalledWith(
      {
        updateMcpServerForm: expect.objectContaining({
          id: "mcp-server-1",
          visibility: "private",
        }),
      },
      undefined,
      undefined,
    );
    // The update payload must not carry the issuer.
    expect(
      client.mcpServers.update.mock.calls[0]?.[0]?.updateMcpServerForm,
    ).not.toHaveProperty("userSessionIssuerId");
  });

  it("reuses the issuer the lookup returns instead of creating one", async () => {
    const client = mockClient({
      existingIssuer: remoteSessionIssuer({
        id: "existing-issuer",
        projectId: "project-1",
        issuer: "https://idp.example.com",
      }),
    });

    const result = await autoConfigureRemoteMcpAuth({
      client: client as unknown as Gram,
      authedFetch: vi.fn(),
      remoteMcpServer: remoteMcpServer(),
      mcpServer: mcpServer(),
    });

    expect(result).toMatchObject({
      status: "configured",
      remoteSessionIssuerId: "existing-issuer",
    });
    expect(client.remoteSessionIssuers.create).not.toHaveBeenCalled();
    expect(client.remoteSessionClients.create).toHaveBeenCalledWith(
      {
        createRemoteSessionClientForm: expect.objectContaining({
          remoteSessionIssuerId: "existing-issuer",
        }),
      },
      undefined,
      undefined,
    );
  });

  it("discovers a missing DCR endpoint for an otherwise configured issuer", async () => {
    const client = mockClient({
      existingIssuer: remoteSessionIssuer({ registrationEndpoint: "" }),
    });

    const result = await autoConfigureRemoteMcpAuth({
      client: client as unknown as Gram,
      authedFetch: vi.fn(),
      remoteMcpServer: remoteMcpServer(),
      mcpServer: mcpServer(),
    });

    expect(result.status).toBe("configured");
    expect(client.remoteSessionIssuers.fetchMetadata).toHaveBeenCalledOnce();
    expect(proxyRegisterMock).toHaveBeenCalledWith(
      expect.any(Function),
      expect.objectContaining({
        registrationEndpoint: "https://idp.example.com/register",
      }),
    );
  });

  it("attaches the client to an issuer the project does not own, such as a platform-catalog one", async () => {
    // A platform-catalog issuer comes back with no project and no organization.
    // The tenant still hangs its own client off it.
    const client = mockClient({
      existingIssuer: remoteSessionIssuer({
        id: "platform-issuer",
        projectId: "",
        organizationId: "",
        issuer: "https://idp.example.com",
      }),
    });

    const result = await autoConfigureRemoteMcpAuth({
      client: client as unknown as Gram,
      authedFetch: vi.fn(),
      remoteMcpServer: remoteMcpServer(),
      mcpServer: mcpServer(),
    });

    expect(result).toMatchObject({
      status: "configured",
      remoteSessionIssuerId: "platform-issuer",
    });
    expect(client.remoteSessionIssuers.create).not.toHaveBeenCalled();
  });

  it("prefers the protected resource's scopes over the authorization server's", async () => {
    const client = mockClient();

    const result = await autoConfigureRemoteMcpAuth({
      client: client as unknown as Gram,
      authedFetch: vi.fn(),
      remoteMcpServer: remoteMcpServer(),
      mcpServer: mcpServer(),
    });

    expect(result.status).toBe("configured");
    expect(proxyRegisterMock).toHaveBeenCalledWith(expect.any(Function), {
      registrationEndpoint: "https://idp.example.com/register",
      scope: "resource.read resource.write",
      tokenEndpointAuthMethod: "client_secret_post",
      tunneledMcpServerId: undefined,
      projectSlug: "test-project",
    });
    expect(client.remoteSessionClients.create).toHaveBeenCalledWith(
      {
        createRemoteSessionClientForm: expect.objectContaining({
          scope: ["resource.read", "resource.write"],
        }),
      },
      undefined,
      undefined,
    );
  });

  it("skips before discovery when the server has no user session issuer", async () => {
    const client = mockClient();

    const result = await autoConfigureRemoteMcpAuth({
      client: client as unknown as Gram,
      authedFetch: vi.fn(),
      remoteMcpServer: remoteMcpServer(),
      mcpServer: mcpServer({ userSessionIssuerId: undefined }),
    });

    expect(result).toEqual({
      status: "skipped",
      message: "No user session issuer is linked to this server.",
      warn: false,
    });
    expect(
      client.remoteMcp.discoverProtectedResourceMetadata,
    ).not.toHaveBeenCalled();
  });

  it("skips tunneled DCR with a permission-oriented result for non-platform admins", async () => {
    const client = mockClient({
      existingIssuer: remoteSessionIssuer({
        tunneledMcpServerId: "tunnel-1",
      }),
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
        "Automatic authentication setup cannot use tunneled dynamic client registration without platform admin access. Configure Manual credentials or CIMD from the Authentication tab.",
      warn: true,
    });
    expect(proxyRegisterMock).not.toHaveBeenCalled();
    expect(client.remoteSessionClients.create).not.toHaveBeenCalled();
  });

  it("uses a saved registration endpoint through the target project's tunnel for platform admins", async () => {
    const client = mockClient({
      existingIssuer: remoteSessionIssuer({
        registrationEndpoint: "http://idp.internal/register",
        tunneledMcpServerId: "tunnel-1",
        tokenEndpointAuthMethodsSupported: ["client_secret_post"],
      }),
    });
    client.remoteSessionIssuers.fetchMetadata.mockRejectedValue(
      new Error("private metadata is unreachable from direct egress"),
    );

    const result = await autoConfigureRemoteMcpAuth({
      client: client as unknown as Gram,
      authedFetch: vi.fn(),
      remoteMcpServer: remoteMcpServer(),
      mcpServer: mcpServer(),
      isPlatformAdmin: true,
      projectSlug: "fallback-project",
      options: { headers: { "gram-project": "other-project" } },
    });

    expect(result.status).toBe("configured");
    expect(client.remoteSessionIssuers.fetchMetadata).not.toHaveBeenCalled();
    expect(proxyRegisterMock).toHaveBeenCalledWith(expect.any(Function), {
      registrationEndpoint: "http://idp.internal/register",
      scope: "resource.read resource.write",
      tokenEndpointAuthMethod: "client_secret_post",
      tunneledMcpServerId: "tunnel-1",
      projectSlug: "other-project",
    });
  });

  // Issuer lookup must happen before the DCR guard because an operator may have
  // saved a registration endpoint that discovery does not advertise. A miss
  // still skips before creating anything.
  it("skips without creating an issuer when neither discovery nor a saved issuer provides DCR", async () => {
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
    expect(client.remoteSessionIssuers.get).toHaveBeenCalledOnce();
    expect(client.remoteSessionIssuers.create).not.toHaveBeenCalled();
    expect(proxyRegisterMock).not.toHaveBeenCalled();
    expect(client.remoteSessionClients.create).not.toHaveBeenCalled();
    expect(client.mcpServers.update).not.toHaveBeenCalled();
  });

  it("skips when a reused issuer is missing OAuth endpoints", async () => {
    const client = mockClient({
      existingIssuer: {
        ...remoteSessionIssuer({ id: "endpointless-issuer" }),
        authorizationEndpoint: undefined,
        tokenEndpoint: undefined,
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
        "A matching identity provider already exists, but it is missing OAuth endpoints.",
      warn: true,
    });
    // Bailing out before DCR means no stray client registration upstream.
    expect(proxyRegisterMock).not.toHaveBeenCalled();
    expect(client.remoteSessionClients.create).not.toHaveBeenCalled();
  });

  it("skips when nothing matches and the draft itself lacks OAuth endpoints", async () => {
    const client = mockClient({
      issuerDraft: {
        issuer: "https://idp.example.com",
        registrationEndpoint: "https://idp.example.com/register",
        scopesSupported: [],
        tokenEndpointAuthMethodsSupported: [],
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
        "OAuth metadata was found, but it is missing required OAuth endpoints.",
      warn: true,
    });
    expect(client.remoteSessionIssuers.create).not.toHaveBeenCalled();
    expect(proxyRegisterMock).not.toHaveBeenCalled();
  });

  // Only a 404 means "nothing describes this URL". Any other failure is a real
  // error and must not be mistaken for a miss that then creates a duplicate.
  it("skips without creating when the issuer lookup fails for a reason other than not-found", async () => {
    const client = mockClient();
    client.remoteSessionIssuers.get.mockRejectedValue(
      Object.assign(new Error("boom"), { statusCode: 500 }),
    );

    const result = await autoConfigureRemoteMcpAuth({
      client: client as unknown as Gram,
      authedFetch: vi.fn(),
      remoteMcpServer: remoteMcpServer(),
      mcpServer: mcpServer(),
    });

    expect(result).toEqual({
      status: "skipped",
      message:
        "OAuth metadata was found, but existing identity providers could not be checked.",
      warn: true,
    });
    expect(client.remoteSessionIssuers.create).not.toHaveBeenCalled();
    expect(proxyRegisterMock).not.toHaveBeenCalled();
  });

  it("skips when the issuer cannot be created", async () => {
    const client = mockClient();
    client.remoteSessionIssuers.create.mockRejectedValueOnce(
      new Error("create failed"),
    );

    const result = await autoConfigureRemoteMcpAuth({
      client: client as unknown as Gram,
      authedFetch: vi.fn(),
      remoteMcpServer: remoteMcpServer(),
      mcpServer: mcpServer(),
    });

    expect(result).toEqual({
      status: "skipped",
      message:
        "OAuth metadata was found, but an identity provider for it could not be created.",
      warn: true,
    });
    expect(proxyRegisterMock).not.toHaveBeenCalled();
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
    expect(client.remoteSessionIssuers.fetchMetadata).not.toHaveBeenCalled();
  });

  // A created issuer is deliberately left behind. The lookup is keyed on the
  // upstream URL, so the next attempt finds this same issuer and reuses it;
  // deleting it would force a re-create on every retry.
  it("keeps a newly created issuer and the USI when client registration fails", async () => {
    const client = mockClient();
    client.remoteSessionClients.create.mockRejectedValueOnce(
      new Error("client create failed"),
    );

    const result = await autoConfigureRemoteMcpAuth({
      client: client as unknown as Gram,
      authedFetch: vi.fn(),
      remoteMcpServer: remoteMcpServer(),
      mcpServer: mcpServer(),
    });

    expect(result).toEqual({
      status: "skipped",
      message:
        "Automatic authentication setup failed. You can configure it from the Authentication tab.",
      warn: true,
    });
    expect(client.remoteSessionIssuers.delete).not.toHaveBeenCalled();
    expect(client.userSessionIssuers.delete).not.toHaveBeenCalled();
    expect(client.mcpServers.update).not.toHaveBeenCalled();
  });
});

function mockClient({
  existingIssuer = null,
  createdIssuer = remoteSessionIssuer({
    id: "created-issuer",
    projectId: "project-1",
    issuer: "https://idp.example.com",
  }),
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
  existingIssuer?: RemoteSessionIssuer | null;
  createdIssuer?: RemoteSessionIssuer;
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
      fetchMetadata: vi.fn().mockResolvedValue(issuerDraft),
      get: existingIssuer
        ? vi.fn().mockResolvedValue(existingIssuer)
        : vi.fn().mockRejectedValue(notFound()),
      create: vi.fn().mockResolvedValue(createdIssuer),
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
        userSessionIssuerId: "server-usi",
      }),
    },
  };
}

// Shaped like the SDK's 404 rejection, which is what isNotFoundError keys on.
function notFound(): Error {
  return Object.assign(new Error("not found"), { statusCode: 404 });
}

function remoteSessionIssuer(
  overrides: Partial<RemoteSessionIssuer>,
): RemoteSessionIssuer {
  return {
    id: overrides.id ?? "issuer-1",
    projectId: overrides.projectId ?? "project-1",
    organizationId: overrides.organizationId ?? "org-1",
    slug: "issuer",
    issuer: overrides.issuer ?? "https://idp.example.com",
    authorizationEndpoint:
      overrides.authorizationEndpoint ?? "https://idp.example.com/authorize",
    tokenEndpoint: overrides.tokenEndpoint ?? "https://idp.example.com/token",
    registrationEndpoint:
      overrides.registrationEndpoint ?? "https://idp.example.com/register",
    scopesSupported: overrides.scopesSupported ?? [],
    grantTypesSupported: overrides.grantTypesSupported ?? [],
    responseTypesSupported: overrides.responseTypesSupported ?? [],
    tokenEndpointAuthMethodsSupported:
      overrides.tokenEndpointAuthMethodsSupported ?? [],
    clientIdMetadataDocumentSupported:
      overrides.clientIdMetadataDocumentSupported ?? false,
    tunneledMcpServerId: overrides.tunneledMcpServerId,
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
    createdAt: new Date(0),
    updatedAt: new Date(0),
  };
}

function mcpServer(overrides: Partial<McpServer> = {}): McpServer {
  return {
    id: "mcp-server-1",
    projectId: "project-1",
    name: "Remote server",
    slug: "remote-server",
    remoteMcpServerId: "remote-mcp-server-1",
    visibility: "disabled",
    userSessionIssuerId: "server-usi",
    createdAt: new Date(0),
    updatedAt: new Date(0),
    ...overrides,
  };
}
