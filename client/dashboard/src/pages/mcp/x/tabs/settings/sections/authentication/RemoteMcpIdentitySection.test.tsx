import { GramError } from "@gram/client/models/errors/gramerror.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { RemoteMcpIdentitySectionBody } from "./RemoteMcpIdentitySection";
import type { AuthTarget } from "./authTarget";
import type { CommitServerIdentityConfigurationForm } from "@gram/client/models/components/commitserveridentityconfigurationform.js";

const mocks = vi.hoisted(() => ({
  headers: vi.fn(),
  sessions: vi.fn(),
  clients: vi.fn(),
  siblings: vi.fn(),
  issuers: vi.fn(),
  issuersByIds: vi.fn(),
  hostIssuers: vi.fn(),
  tierSearch: vi.fn(),
  tierHasMore: vi.fn(),
  tierLoadMore: vi.fn(),
  source: vi.fn(),
  rbac: vi.fn(),
  hasScope: vi.fn(),
  create: vi.fn(),
  update: vi.fn(),
  remove: vi.fn(),
  invalidateHeaders: vi.fn(),
  refetchHeaders: vi.fn(),
  authenticationProbe: vi.fn(),
  protectedResourceMetadata: vi.fn(),
  fetchMetadata: vi.fn(),
  discoverProtectedResource: vi.fn(),
  commit: vi.fn(),
  detach: vi.fn(),
  userSessionIssuer: vi.fn(),
  toastSuccess: vi.fn(),
  scopes: vi.fn(),
  setPin: vi.fn(),
  invalidateScopes: vi.fn(),
  setScopesData: vi.fn(),
  toastError: vi.fn(),
}));

vi.mock("sonner", () => ({
  toast: {
    success: mocks.toastSuccess,
    error: mocks.toastError,
    warning: vi.fn(),
  },
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: {
      x: {
        overview: { href: (slug: string) => `/mcp/x/${slug}` },
        inspect: { href: (slug: string) => `/mcp/x/${slug}/inspect` },
        teamAccess: { href: (slug: string) => `/mcp/x/${slug}/team-access` },
        sessions: { href: (slug: string) => `/mcp/x/${slug}/sessions` },
        settings: { href: (slug: string) => `/mcp/x/${slug}/settings` },
      },
    },
    remoteIdentityProviders: {
      href: () => "/remote-identity-providers",
      issuerDetail: { href: (id: string) => `/providers/${id}` },
      clientDetail: {
        href: (issuerId: string, clientId: string) =>
          `/providers/${issuerId}/clients/${clientId}`,
        mcpServers: {
          href: (issuerId: string, clientId: string) =>
            `/providers/${issuerId}/clients/${clientId}/mcp-servers`,
        },
      },
    },
  }),
}));

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    remoteSessions: { commitServerIdentityConfiguration: mocks.commit },
    remoteSessionIssuers: { fetchMetadata: mocks.fetchMetadata },
    remoteMcp: {
      discoverProtectedResourceMetadata: mocks.discoverProtectedResource,
    },
  }),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => mocks.rbac(),
}));

// The MCP catalog only seeds suggested header rows. Nothing here is about
// suggestions, and the real hook drags the project-slug context in with it.
vi.mock("@/pages/catalog/hooks", () => ({
  useListMCPCatalog: () => ({ data: undefined }),
}));

vi.mock("@gram/client/react-query/remoteMcpServerHeaders.js", () => ({
  useRemoteMcpServerHeaders: () => mocks.headers(),
  invalidateAllRemoteMcpServerHeaders: (...args: unknown[]) =>
    mocks.invalidateHeaders(...args),
}));

vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  useMcpServers: () => mocks.siblings(),
}));

vi.mock("@gram/client/react-query/getRemoteMcpServer.js", () => ({
  useGetRemoteMcpServer: () => mocks.source(),
}));

vi.mock("@gram/client/react-query/remoteSessionIssuers.js", () => ({
  useRemoteSessionIssuers: (request?: { upstreamHost?: string }) => {
    const listed = mocks.issuers();
    const host = request?.upstreamHost;
    if (!host) return listed;
    const hostItems = mocks.hostIssuers(host);
    if (hostItems === "error") return { data: undefined, isError: true };
    if (hostItems) return { data: { result: { items: hostItems } } };
    // Stands in for the server's upstream_host filter: the issuer's host is
    // the given host or one of its parent domains.
    const items = (
      (listed.data?.result.items ?? []) as Array<{ issuer: string }>
    ).filter((issuer) => {
      const issuerHost = new URL(issuer.issuer).host;
      return issuerHost === host || host.endsWith(`.${issuerHost}`);
    });
    return { data: { result: { items } }, isLoading: false };
  },
  // Stands in for the server's tier and search filters over the listing.
  useRemoteSessionIssuersInfinite: (request: {
    tier: "project" | "organization" | "platform";
    search?: string;
  }) => {
    mocks.tierSearch(request.tier, request.search);
    const items = (
      (mocks.issuers().data?.result.items ?? []) as Array<{
        projectId?: string;
        organizationId?: string;
        name?: string;
        slug: string;
        issuer: string;
      }>
    ).filter((issuer) => {
      const tier = issuer.projectId
        ? "project"
        : issuer.organizationId
          ? "organization"
          : "platform";
      const q = request.search?.toLowerCase();
      return (
        tier === request.tier &&
        (!q ||
          [issuer.name ?? "", issuer.slug, issuer.issuer].some((field) =>
            field.toLowerCase().includes(q),
          ))
      );
    });
    return {
      data: { pages: [{ result: { items } }] },
      isLoading: false,
      isError: false,
      hasNextPage: !!mocks.tierHasMore(request.tier),
      isFetchingNextPage: false,
      fetchNextPage: () => mocks.tierLoadMore(request.tier),
    };
  },
  invalidateAllRemoteSessionIssuers: vi.fn(),
}));

vi.mock("@gram/client/react-query/userSessionIssuer.js", () => ({
  useUserSessionIssuer: () => mocks.userSessionIssuer(),
}));

vi.mock("@gram/client/react-query/remoteSessionClients.js", () => ({
  invalidateAllRemoteSessionClients: vi.fn(),
}));

vi.mock("@gram/client/react-query/detachUserSessionIssuer.js", () => ({
  useDetachUserSessionIssuerMutation: () => ({
    mutateAsync: mocks.detach,
    isPending: false,
    error: null,
  }),
}));

vi.mock("@gram/client/react-query/remoteSessionsCount.js", () => ({
  useRemoteSessionsCount: () => mocks.sessions(),
  invalidateAllRemoteSessionsCount: vi.fn(),
}));

vi.mock("@/lib/remote-identity/queries/useAllRemoteSessionClients", () => ({
  useAllRemoteSessionClients: () => mocks.clients(),
}));

vi.mock("@/lib/remote-identity/queries/useRemoteSessionIssuersByIds", () => ({
  useRemoteSessionIssuersByIds: (ids: string[]) => mocks.issuersByIds(ids),
}));

vi.mock("@/lib/remote-identity/queries/useUpstreamProbe", () => ({
  useUpstreamProbe: (...args: unknown[]) => mocks.authenticationProbe(...args),
}));

vi.mock("@/lib/remote-identity/queries/useProtectedResourceMetadata", () => ({
  useProtectedResourceMetadata: (...args: unknown[]) =>
    mocks.protectedResourceMetadata(...args),
}));

vi.mock("@gram/client/react-query/getRemoteMcpServerScopes.js", () => ({
  useGetRemoteMcpServerScopes: (...args: unknown[]) => mocks.scopes(...args),
  setGetRemoteMcpServerScopesData: (...args: unknown[]) =>
    mocks.setScopesData(...args),
  invalidateAllGetRemoteMcpServerScopes: (...args: unknown[]) =>
    mocks.invalidateScopes(...args),
}));

vi.mock("@gram/client/react-query/setRemoteMcpServerScopePin.js", () => ({
  useSetRemoteMcpServerScopePinMutation: () => ({
    mutateAsync: mocks.setPin,
    isPending: false,
  }),
}));

vi.mock("@gram/client/react-query/createRemoteMcpServerHeader.js", () => ({
  useCreateRemoteMcpServerHeaderMutation: () => ({
    mutateAsync: mocks.create,
    reset: vi.fn(),
    isPending: false,
    error: null,
  }),
}));

vi.mock("@gram/client/react-query/updateRemoteMcpServerHeader.js", () => ({
  useUpdateRemoteMcpServerHeaderMutation: () => ({
    mutateAsync: mocks.update,
    reset: vi.fn(),
    isPending: false,
    error: null,
  }),
}));

vi.mock("@gram/client/react-query/deleteRemoteMcpServerHeader.js", () => ({
  useDeleteRemoteMcpServerHeaderMutation: () => ({
    mutateAsync: mocks.remove,
    reset: vi.fn(),
    isPending: false,
    error: null,
  }),
}));

function renderIdentity(): ReturnType<typeof render> & {
  rerenderIdentity: () => void;
} {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  // A fresh element each time, or React would skip the re-render.
  const tree = () => (
    <MemoryRouter>
      <QueryClientProvider client={queryClient}>
        <TooltipProvider>
          <RemoteMcpIdentitySectionBody target={target} />
        </TooltipProvider>
      </QueryClientProvider>
    </MemoryRouter>
  );
  const result = render(tree());
  return { ...result, rerenderIdentity: () => result.rerender(tree()) };
}

function addCustomHeader(name: string, value: string): void {
  fireEvent.click(screen.getByText("Custom Headers"));
  fireEvent.click(screen.getByRole("button", { name: "Add header" }));
  const names = screen.getAllByLabelText("Header name");
  fireEvent.change(names.at(-1)!, { target: { value: name } });
  const values = screen.getAllByLabelText("Header value");
  fireEvent.change(values.at(-1)!, { target: { value } });
}

function configuredHeader(overrides: Record<string, unknown> = {}) {
  return {
    id: "header-1",
    name: "Authorization",
    value: "***",
    isRequired: true,
    isSecret: true,
    createdAt: new Date(0),
    updatedAt: new Date(0),
    ...overrides,
  };
}

function serverScopes(overrides: Record<string, unknown> = {}) {
  return {
    resourceUrl: "https://mcp.linear.app/mcp",
    pinnedScopes: ["read"],
    advertisedScopesKnown: true,
    advertisedScopes: ["read", "write"],
    challengeScopes: [],
    discoveryEnabled: true,
    sharedServerCount: 0,
    clients: [
      {
        clientId: "client-1",
        scopeSource: "resource_pin",
        requestedScopes: ["read"],
        unadvertisedPinnedScopes: [],
        pinWouldDecide: true,
      },
    ],
    ...overrides,
  };
}

function sharedSource(): void {
  mocks.siblings.mockReturnValue({
    data: {
      mcpServers: [
        { id: "mcp-server-1", remoteMcpServerId: "remote-source-1" },
        { id: "mcp-server-2", remoteMcpServerId: "remote-source-1" },
      ],
    },
    isLoading: false,
    isError: false,
  });
}

function connectClient(): void {
  mocks.clients.mockReturnValue({
    items: [
      {
        id: "client-1",
        clientId: "dashboard-client",
        remoteSessionIssuerId: "provider-1",
        userSessionIssuerIds: ["user-session-issuer-1"],
        scope: [],
      },
    ],
    isLoading: false,
    isError: false,
    error: null,
  });
  mocks.issuers.mockReturnValue({
    data: {
      result: {
        items: [
          {
            id: "provider-1",
            name: "Example provider",
            issuer: "https://id.example",
            slug: "example",
            projectId: "project-1",
            authorizationEndpoint: "https://id.example/authorize",
            tokenEndpoint: "https://id.example/token",
            scopesSupported: ["profile"],
          },
        ],
      },
    },
  });
}

function autoConfigurableProvider(): void {
  mocks.issuers.mockReturnValue({
    data: {
      result: {
        items: [
          {
            id: "provider-1",
            name: "Linear",
            issuer: "https://mcp.linear.app",
            slug: "linear",
            projectId: "project-1",
            clientIdMetadataDocumentSupported: true,
            authorizationEndpoint: "https://mcp.linear.app/authorize",
            tokenEndpoint: "https://mcp.linear.app/token",
          },
        ],
      },
    },
  });
}

// Save, then confirm if the change is destructive enough to ask.
function confirmSave(): void {
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  const confirm = screen.queryByRole("button", { name: "Save changes" });
  if (confirm) fireEvent.click(confirm);
}

const target: AuthTarget = {
  kind: "remote-mcp",
  slug: "remote-server",
  projectId: "project-1",
  permissionResourceId: "mcp-server-1",
  supportsOrganizationIssuers: true,
  userSessionIssuerId: "user-session-issuer-1",
  remoteMcpServerId: "remote-source-1",
  invalidate: vi.fn(),
};

beforeEach(() => {
  mocks.userSessionIssuer.mockReturnValue({
    data: { id: "user-session-issuer-1", projectId: "project-1" },
  });
  mocks.headers.mockReturnValue({
    data: { headers: [] },
    isLoading: false,
    isError: false,
    error: null,
    refetch: mocks.refetchHeaders,
  });
  mocks.refetchHeaders.mockResolvedValue({
    data: { headers: [] },
    isError: false,
  });
  mocks.clients.mockReturnValue({
    items: [],
    isLoading: false,
    isError: false,
    error: null,
  });
  mocks.siblings.mockReturnValue({
    data: {
      mcpServers: [
        {
          id: "mcp-server-1",
          name: "Linear",
          remoteMcpServerId: "remote-source-1",
        },
      ],
    },
    isLoading: false,
    isError: false,
  });
  mocks.source.mockReturnValue({
    data: { slug: "linear", url: "https://mcp.linear.app/mcp" },
    isLoading: false,
    isError: false,
  });
  mocks.issuers.mockReturnValue({ data: { result: { items: [] } } });
  // No host override: host lookups filter the listing above.
  mocks.hostIssuers.mockImplementation(() => undefined);
  mocks.tierHasMore.mockImplementation(() => false);
  // By default a lookup by id finds whatever the listing holds.
  mocks.issuersByIds.mockImplementation((ids: string[]) => ({
    items: (
      (mocks.issuers().data?.result.items ?? []) as Array<{ id: string }>
    ).filter((issuer) => ids.includes(issuer.id)),
    isLoading: false,
    isError: false,
  }));
  mocks.sessions.mockReturnValue({ data: { subjects: 1 } });
  mocks.protectedResourceMetadata.mockReturnValue({
    status: "idle",
    metadata: null,
  });
  mocks.rbac.mockReturnValue({
    isLoading: false,
    hasScope: mocks.hasScope,
    hasAllScopes: () => true,
    hasAnyScope: (_scopes: string[], resourceId?: string) =>
      resourceId === undefined || resourceId === "mcp-server-1",
  });
  mocks.hasScope.mockImplementation(
    (_scope: string, resourceId?: string) => resourceId === "mcp-server-1",
  );
  mocks.create.mockResolvedValue({});
  mocks.update.mockResolvedValue({});
  mocks.remove.mockResolvedValue({});
  mocks.commit.mockResolvedValue({
    status: "registered",
    manualSetupRequired: false,
  });
  mocks.discoverProtectedResource.mockResolvedValue({ available: false });
  mocks.invalidateHeaders.mockResolvedValue(undefined);
  mocks.detach.mockResolvedValue({});
  mocks.authenticationProbe.mockReturnValue("available");
  mocks.scopes.mockReturnValue({ data: undefined, isError: false });
  mocks.setPin.mockImplementation(
    async ({
      request,
    }: {
      request: { setServerScopePinRequestBody: { scopes: string[] } };
    }) =>
      serverScopes({
        pinnedScopes: request.setServerScopePinRequestBody.scopes,
      }),
  );
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("RemoteMcpIdentitySectionBody", () => {
  it("names the three identity modes after the upstream service", () => {
    renderIdentity();

    expect(screen.getByRole("radio", { name: /User Identity/ })).toBeDefined();
    expect(
      screen.getByText(
        "Each user signs in to Linear as themselves and keeps their own permissions.",
      ),
    ).toBeDefined();
    expect(
      screen.getByRole("radio", { name: /Service Account/ }),
    ).toBeDefined();
    expect(
      screen.getByRole("radio", {
        name: "Manual",
        description: /static headers/,
      }),
    ).toBeDefined();
  });

  it("configures User Identity inline without OAuth vocabulary", async () => {
    mocks.issuers.mockReturnValue({
      data: {
        result: {
          items: [
            {
              id: "provider-1",
              name: "Linear",
              issuer: "https://mcp.linear.app",
              slug: "linear",
              projectId: "project-1",
              clientIdMetadataDocumentSupported: true,
              authorizationEndpoint: "https://mcp.linear.app/authorize",
              tokenEndpoint: "https://mcp.linear.app/token",
            },
          ],
        },
      },
    });

    renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));

    // One provider and a choice of how to register — no issuer, DCR or CIMD
    // wording while the provider offers only one automatic path.
    expect(screen.getByLabelText("Identity provider")).toBeDefined();
    expect(
      screen
        .getByRole("radio", { name: /Auto-Configure/ })
        .getAttribute("aria-checked"),
    ).toBe("true");
    expect(screen.queryByText(/CIMD/i)).toBeNull();
    expect(screen.queryByText(/DCR/i)).toBeNull();
    expect(screen.queryByText(/issuer/i)).toBeNull();
    expect(screen.queryByText(/audience/i)).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(mocks.commit).toHaveBeenCalledOnce());
    expect(mocks.commit).toHaveBeenCalledWith(
      expect.objectContaining({
        commitServerIdentityConfigurationForm: expect.objectContaining({
          mcpServerId: "mcp-server-1",
          providerId: "provider-1",
          clientMode: "auto",
          registrationMethod: undefined,
        }),
      }),
    );
  });

  it("stores no scope on an automatic client so each sign-in discovers it live", async () => {
    // Nothing discovered here is copied onto the client: the server resolves
    // the scopes to request at each sign-in from the protected resource, so
    // a copied list cannot go stale.
    mocks.issuers.mockReturnValue({
      data: {
        result: {
          items: [
            {
              id: "provider-1",
              name: "Linear",
              issuer: "https://mcp.linear.app",
              slug: "linear",
              projectId: "project-1",
              clientIdMetadataDocumentSupported: true,
              authorizationEndpoint: "https://mcp.linear.app/authorize",
              tokenEndpoint: "https://mcp.linear.app/token",
              // client_secret_post rules CIMD out, so this registers by DCR.
              registrationEndpoint: "https://mcp.linear.app/register",
              scopesSupported: ["read", "write", "admin", "refresh_token"],
              tokenEndpointAuthMethodsSupported: ["client_secret_post"],
            },
          ],
        },
      },
    });
    mocks.discoverProtectedResource.mockResolvedValue({
      available: true,
      metadata: { scopesSupported: ["read", " "] },
    });

    renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(mocks.commit).toHaveBeenCalledOnce());
    const form = mocks.commit.mock.calls[0]![0]
      .commitServerIdentityConfigurationForm as CommitServerIdentityConfigurationForm;
    expect(form.clientMode).toBe("auto");
    expect(form.clientConfiguration?.scope).toBeUndefined();
    expect(form.clientConfiguration?.tokenEndpointAuthMethod).toBe(
      "client_secret_post",
    );
  });

  it("will not auto-configure a provider missing its OAuth endpoints", () => {
    // Advertising CIMD is a claim about registration, not about being usable:
    // without both endpoints the registration would persist an identity
    // nobody can complete a login through.
    mocks.issuers.mockReturnValue({
      data: {
        result: {
          items: [
            {
              id: "provider-1",
              name: "Linear",
              issuer: "https://mcp.linear.app",
              slug: "linear",
              projectId: "project-1",
              clientIdMetadataDocumentSupported: true,
              authorizationEndpoint: "https://mcp.linear.app/authorize",
              tokenEndpoint: undefined,
            },
          ],
        },
      },
    });

    renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));

    expect(
      (
        screen.getByRole("radio", {
          name: /Auto-Configure/,
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    expect(
      screen
        .getByRole("radio", {
          name: "Manual",
          description: /client ID and secret/,
        })
        .getAttribute("aria-checked"),
    ).toBe("true");
  });

  it("opens the provider menu on click", () => {
    mocks.issuers.mockReturnValue({
      data: {
        result: {
          items: [
            {
              id: "provider-1",
              name: "Linear",
              issuer: "https://mcp.linear.app",
              slug: "linear",
              projectId: "project-1",
              clientIdMetadataDocumentSupported: true,
              authorizationEndpoint: "https://mcp.linear.app/authorize",
              tokenEndpoint: "https://mcp.linear.app/token",
            },
          ],
        },
      },
    });

    renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));

    // The trigger renders under <PopoverTrigger asChild>, so the props Radix
    // clones onto it have to survive. aria-expanded flipping is the proof that
    // the click handler and state actually reached the button.
    const trigger = screen.getByLabelText("Identity provider");
    expect(trigger.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(trigger);
    expect(trigger.getAttribute("aria-expanded")).toBe("true");
    fireEvent.click(trigger);
    expect(trigger.getAttribute("aria-expanded")).toBe("false");
  });

  it("searches each provider tier on the server from the menu", async () => {
    const catalog = Array.from({ length: 3 }, (_, index) => ({
      id: `platform-${index}`,
      name: `Catalog ${index}`,
      issuer: `https://catalog-${index}.example.test`,
      slug: `catalog-${index}`,
    }));
    mocks.issuers.mockReturnValue({
      data: {
        result: {
          items: [
            ...catalog,
            {
              id: "provider-own",
              name: "Own provider",
              issuer: "https://own.example.test",
              slug: "own",
              projectId: "project-1",
            },
          ],
        },
      },
    });
    mocks.tierHasMore.mockImplementation((tier: string) => tier === "platform");

    renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));
    fireEvent.click(screen.getByLabelText("Identity provider"));

    // Every tier is its own query, so the catalog cannot crowd out the
    // project's own provider, and a tier with more pages offers them.
    expect(screen.getByText("Own provider")).toBeDefined();
    expect(screen.getByText("Catalog 0")).toBeDefined();
    fireEvent.click(screen.getByText("More platform providers"));
    expect(mocks.tierLoadMore).toHaveBeenCalledWith("platform");

    fireEvent.change(
      screen.getByPlaceholderText("Search identity providers…"),
      { target: { value: "own" } },
    );
    await waitFor(() =>
      expect(mocks.tierSearch).toHaveBeenCalledWith("platform", "own"),
    );
    expect(mocks.tierSearch).toHaveBeenCalledWith("project", "own");
    await waitFor(() => expect(screen.queryByText("Catalog 0")).toBeNull());
    expect(screen.getByText("Own provider")).toBeDefined();
  });

  it("holds the provider control while discovery is still running", () => {
    mocks.protectedResourceMetadata.mockReturnValue({
      status: "loading",
      metadata: null,
    });

    renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));

    // Not "Choose an identity provider": the control is about to answer its
    // own question, and a pick made now would look overwritten when the
    // discovered default lands.
    const trigger = screen.getByLabelText(
      "Identity provider",
    ) as HTMLButtonElement;
    expect(trigger.textContent).toContain("Checking");
    expect(trigger.disabled).toBe(true);
  });

  it("suggests a same-site provider the issuer listing does not contain", async () => {
    // The listing page is empty, standing in for a catalog large enough to
    // push the matching provider off it; only the host lookup finds it.
    mocks.hostIssuers.mockImplementation((host: string) =>
      host === "mcp.linear.app"
        ? [
            {
              id: "provider-linear",
              name: "Linear",
              issuer: "https://linear.app",
              slug: "linear",
              projectId: "project-1",
              clientIdMetadataDocumentSupported: true,
              authorizationEndpoint: "https://linear.app/authorize",
              tokenEndpoint: "https://linear.app/token",
            },
          ]
        : undefined,
    );

    renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));

    expect(screen.queryByText("Will be created")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(mocks.commit).toHaveBeenCalledOnce());
    expect(mocks.commit).toHaveBeenCalledWith(
      expect.objectContaining({
        commitServerIdentityConfigurationForm: expect.objectContaining({
          providerId: "provider-linear",
        }),
      }),
    );
  });

  it("does not offer to create an advertised provider the project already has", () => {
    mocks.protectedResourceMetadata.mockReturnValue({
      status: "available",
      metadata: { authorizationServers: ["https://auth.example.test"] },
    });
    mocks.hostIssuers.mockImplementation((host: string) =>
      host === "auth.example.test"
        ? [
            {
              id: "provider-known",
              name: "Known provider",
              issuer: "https://auth.example.test/oauth",
              slug: "known",
            },
          ]
        : undefined,
    );

    renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));

    expect(screen.queryByText("Will be created")).toBeNull();
    fireEvent.click(screen.getByLabelText("Identity provider"));
    expect(screen.getByText("Known provider")).toBeDefined();
  });

  it("does not offer to create a provider when the known-provider lookup fails", () => {
    mocks.protectedResourceMetadata.mockReturnValue({
      status: "available",
      metadata: { authorizationServers: ["https://auth.example.test"] },
    });
    mocks.hostIssuers.mockImplementation((host: string) =>
      host === "auth.example.test" ? "error" : undefined,
    );

    renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));

    expect(screen.queryByText("Will be created")).toBeNull();
    expect(
      screen.getByText(/Couldn.t load this server.s identity providers/),
    ).toBeDefined();
  });

  it("does not call the upstream unreachable when our own lookup failed", () => {
    mocks.protectedResourceMetadata.mockReturnValue({
      status: "unavailable",
      metadata: null,
    });
    mocks.hostIssuers.mockImplementation(() => "error");

    renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));

    expect(
      screen.getByText(/Couldn.t load this server.s identity providers/),
    ).toBeDefined();
    expect(screen.queryByText(/Couldn.t reach the upstream/)).toBeNull();
  });

  it("offers the discovered provider as one that will be created", () => {
    mocks.protectedResourceMetadata.mockReturnValue({
      status: "available",
      metadata: { authorizationServers: ["https://auth.linear.app"] },
    });

    renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));

    expect(screen.getByText("Will be created")).toBeDefined();
    // The derived name and the host read the same for a bare issuer URL.
    expect(screen.getAllByText("auth.linear.app").length).toBeGreaterThan(0);
  });

  it("does not offer Auto-Configure for a provider that cannot register", async () => {
    mocks.protectedResourceMetadata.mockReturnValue({
      status: "available",
      metadata: { authorizationServers: ["https://github.com/login/oauth"] },
    });
    // GitHub publishes no registration endpoint and no CIMD document — it can
    // only be set up by hand, and says so through service_documentation.
    mocks.fetchMetadata.mockResolvedValue({
      clientIdMetadataDocumentSupported: false,
      registrationEndpoint: undefined,
      serviceDocumentation: "https://docs.github.com/apps",
    });

    renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));

    await waitFor(() =>
      expect(
        screen
          .getByRole("radio", {
            name: "Manual",
            description: /client ID and secret/,
          })
          .getAttribute("aria-checked"),
      ).toBe("true"),
    );
    expect(
      (
        screen.getByRole("radio", {
          name: /Auto-Configure/,
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    expect(screen.getByLabelText("Client ID")).toBeDefined();
    // The guide comes from the provider's metadata, which lands after the
    // cards do.
    const guide = await screen.findByRole("link", {
      name: /Open registration guide/i,
    });
    expect(guide.getAttribute("href")).toBe("https://docs.github.com/apps");
  });

  it("marks the No Identity card when the probe says auth is required", () => {
    mocks.authenticationProbe.mockReturnValue("authentication-required");

    renderIdentity();

    // The warning lives on the choice it is about, not in a banner below it,
    // and the detail waits for a hover rather than taking a row.
    const card = screen
      .getByLabelText("This server requires authentication")
      .closest("[data-slot=radio-card]");
    expect(card).not.toBeNull();
    expect(card?.className).toContain("border-warning-default");
    expect(screen.queryByText(/keep failing/i)).toBeNull();
    expect(mocks.authenticationProbe).toHaveBeenCalledWith(
      "remote-source-1",
      true,
    );
  });

  it("allows Agent to User, which the derivation already expects", () => {
    mocks.headers.mockReturnValue({
      data: { headers: [configuredHeader()] },
      isLoading: false,
      isError: false,
      error: null,
      refetch: mocks.refetchHeaders,
    });

    renderIdentity();

    // A linked client outranks a static credential, so a server holding both
    // reads as User — AIM-230 calls that out and leaves the stale credential
    // visible for cleanup rather than forbidding the move.
    expect(
      screen
        .getByRole("radio", { name: /Service Account/ })
        .getAttribute("aria-checked"),
    ).toBe("true");
    expect(
      (
        screen.getByRole("radio", {
          name: /User Identity/,
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(false);
  });

  it("will not remove the credential when Agent to User cannot commit a client", async () => {
    mocks.headers.mockReturnValue({
      data: { headers: [configuredHeader()] },
      isLoading: false,
      isError: false,
      error: null,
      refetch: mocks.refetchHeaders,
    });

    renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));
    addCustomHeader("X-Team", "eng");

    // No provider is picked, so Save would delete the credential and then
    // skip the client: the server would be left with no identity.
    const save = screen.getByRole("button", { name: "Save" });
    expect((save as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(save);
    await Promise.resolve();
    expect(mocks.remove).not.toHaveBeenCalled();
    expect(mocks.create).not.toHaveBeenCalled();
    expect(mocks.commit).not.toHaveBeenCalled();
  });

  it("commits the client before removing the credential on Agent to User", async () => {
    mocks.headers.mockReturnValue({
      data: { headers: [configuredHeader()] },
      isLoading: false,
      isError: false,
      error: null,
      refetch: mocks.refetchHeaders,
    });
    autoConfigurableProvider();

    renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));
    confirmSave();

    await waitFor(() => expect(mocks.remove).toHaveBeenCalledOnce());
    expect(mocks.commit).toHaveBeenCalledOnce();
    expect(mocks.remove).toHaveBeenCalledWith({ request: { id: "header-1" } });
    expect(mocks.commit.mock.invocationCallOrder[0]).toBeLessThan(
      mocks.remove.mock.invocationCallOrder[0]!,
    );
  });

  it("keeps the credential and skips headers when Agent to User fails to commit", async () => {
    mocks.headers.mockReturnValue({
      data: { headers: [configuredHeader()] },
      isLoading: false,
      isError: false,
      error: null,
      refetch: mocks.refetchHeaders,
    });
    autoConfigurableProvider();
    mocks.commit.mockRejectedValue(new Error("commit refused"));

    renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));
    addCustomHeader("X-Team", "eng");
    confirmSave();

    await waitFor(() => expect(mocks.commit).toHaveBeenCalledOnce());
    await waitFor(() => expect(mocks.toastError).toHaveBeenCalled());
    await new Promise((resolve) => {
      setTimeout(resolve, 0);
    });
    expect(mocks.remove).not.toHaveBeenCalled();
    expect(mocks.create).not.toHaveBeenCalled();
    expect(mocks.update).not.toHaveBeenCalled();
    expect(mocks.setPin).not.toHaveBeenCalled();
    expect(mocks.toastSuccess).not.toHaveBeenCalled();
  });

  it("stays Linked when the provider already in force is re-picked", async () => {
    mocks.clients.mockReturnValue({
      items: [
        {
          id: "client-1",
          clientId: "dashboard-client",
          remoteSessionIssuerId: "provider-1",
          userSessionIssuerIds: ["user-session-issuer-1"],
        },
      ],
      isLoading: false,
      isError: false,
      error: null,
    });
    mocks.issuers.mockReturnValue({
      data: {
        result: {
          items: [
            {
              id: "provider-1",
              name: "Example provider",
              issuer: "https://id.example",
              slug: "example",
              clientIdMetadataDocumentSupported: true,
              authorizationEndpoint: "https://mcp.linear.app/authorize",
              tokenEndpoint: "https://mcp.linear.app/token",
            },
          ],
        },
      },
    });

    renderIdentity();

    // Configured and unchanged: nothing to commit.
    const save = () =>
      screen.getByRole("button", { name: /^Save/ }) as HTMLButtonElement;
    expect(save().disabled).toBe(true);

    // Opening the menu and choosing what is already chosen is not a change.
    fireEvent.click(screen.getByLabelText("Identity provider"));
    // The trigger shows the name too, so the menu entry is the later match.
    await waitFor(() =>
      expect(screen.getAllByText("Example provider").length).toBeGreaterThan(1),
    );
    const entries = screen.getAllByText("Example provider");
    fireEvent.click(entries[entries.length - 1] as HTMLElement);

    expect(save().disabled).toBe(true);
    expect(mocks.commit).not.toHaveBeenCalled();
  });

  it("shows the linked provider when the issuer listing does not contain it", () => {
    mocks.clients.mockReturnValue({
      items: [
        {
          id: "client-1",
          clientId: "dashboard-client",
          remoteSessionIssuerId: "provider-far",
          userSessionIssuerIds: ["user-session-issuer-1"],
        },
      ],
      isLoading: false,
      isError: false,
      error: null,
    });
    // The listing is empty, standing in for a linked issuer pushed past the
    // first page by the platform catalog; only the lookup by id finds it.
    mocks.issuersByIds.mockReturnValue({
      items: [
        {
          id: "provider-far",
          name: "Far provider",
          issuer: "https://id.example",
          slug: "far",
        },
      ],
      isLoading: false,
      isError: false,
    });

    renderIdentity();

    expect(mocks.issuersByIds).toHaveBeenCalledWith(["provider-far"]);
    expect(screen.getByText("Far provider")).toBeDefined();
  });

  it("holds a mode change as a draft until Save", async () => {
    mocks.clients.mockReturnValue({
      items: [
        {
          id: "client-1",
          clientId: "dashboard-client",
          remoteSessionIssuerId: "provider-1",
          userSessionIssuerIds: ["user-session-issuer-1"],
        },
      ],
      isLoading: false,
      isError: false,
      error: null,
    });
    mocks.issuers.mockReturnValue({
      data: {
        result: {
          items: [
            {
              id: "provider-1",
              name: "Example provider",
              issuer: "https://id.example",
              slug: "example",
            },
          ],
        },
      },
    });

    renderIdentity();

    // The choice is not a one-way door, and picking a card writes nothing.
    const none = screen.getByRole("radio", {
      name: "Manual",
      description: /static headers/,
    }) as HTMLButtonElement;
    expect(none.disabled).toBe(false);
    fireEvent.click(none);
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(mocks.detach).not.toHaveBeenCalled();

    // Save is what commits it, and the dialog is where the consequence is
    // stated — the footer stays quiet.
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(screen.getByRole("dialog")).toBeDefined();
    expect(mocks.detach).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() =>
      expect(mocks.detach).toHaveBeenCalledWith({
        request: {
          attachUserSessionIssuerForm: {
            id: "client-1",
            userSessionIssuerId: "user-session-issuer-1",
          },
        },
      }),
    );
  });

  it("shows a connected client with its sign-ins, scopes and settings", () => {
    mocks.clients.mockReturnValue({
      items: [
        {
          id: "client-1",
          clientId: "dashboard-client",
          remoteSessionIssuerId: "provider-1",
          userSessionIssuerIds: ["user-session-issuer-1"],
          scope: ["read", "write"],
        },
      ],
      isLoading: false,
      isError: false,
      error: null,
    });
    mocks.issuers.mockReturnValue({
      data: {
        result: {
          items: [
            {
              id: "provider-1",
              name: "Example provider",
              issuer: "https://id.example",
              slug: "example",
              projectId: "project-1",
              clientIdMetadataDocumentSupported: true,
              authorizationEndpoint: "https://id.example/authorize",
              tokenEndpoint: "https://id.example/token",
            },
          ],
        },
      },
    });
    mocks.sessions.mockReturnValue({ data: { subjects: 3 } });

    renderIdentity();

    expect(screen.getByText("Connected")).toBeDefined();
    expect(screen.getByText("3 people")).toBeDefined();
    // The count opens the list of what the client asks for.
    fireEvent.click(screen.getByRole("button", { name: "2 scopes" }));
    expect(screen.getByText("Requested scopes")).toBeDefined();
    expect(screen.getByText("read")).toBeDefined();
    expect(screen.getByText("write")).toBeDefined();
    // The issuer URL is context, not a way in; the client's own page is.
    expect(screen.queryByRole("link", { name: "id.example" })).toBeNull();
    expect(
      screen.getByRole("link", { name: /Advanced/ }).getAttribute("href"),
    ).toBe("/providers/provider-1/clients/client-1");
    // A raw client ID tells an operator nothing.
    expect(screen.queryByText("dashboard-client")).toBeNull();
  });

  it("replaces a cleared client only after confirming the sign-out", async () => {
    mocks.clients.mockReturnValue({
      items: [
        {
          id: "client-1",
          clientId: "dashboard-client",
          remoteSessionIssuerId: "provider-1",
          userSessionIssuerIds: ["user-session-issuer-1"],
          scope: ["read", "write"],
        },
      ],
      isLoading: false,
      isError: false,
      error: null,
    });
    mocks.issuers.mockReturnValue({
      data: {
        result: {
          items: [
            {
              id: "provider-1",
              name: "Example provider",
              issuer: "https://id.example",
              slug: "example",
              projectId: "project-1",
              clientIdMetadataDocumentSupported: true,
              authorizationEndpoint: "https://id.example/authorize",
              tokenEndpoint: "https://id.example/token",
            },
          ],
        },
      },
    });

    renderIdentity();
    fireEvent.click(screen.getByRole("button", { name: "Clear connection" }));

    // Clearing is a draft: the choices open, and nothing is written yet.
    expect(screen.getByText("Unconfigured client")).toBeDefined();
    expect(mocks.commit).not.toHaveBeenCalled();

    // The only existing client is the one in force, so reusing it changes
    // nothing and Save stays shut.
    expect(
      screen
        .getByRole("radio", { name: /Existing client/ })
        .getAttribute("aria-checked"),
    ).toBe("true");
    expect(
      (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);

    fireEvent.click(screen.getByRole("radio", { name: /Auto-Configure/ }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(screen.getByRole("dialog")).toBeDefined();
    expect(screen.getByText("Replace the connected client?")).toBeDefined();
    expect(mocks.commit).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() => expect(mocks.commit).toHaveBeenCalledOnce());
    expect(mocks.commit).toHaveBeenCalledWith(
      expect.objectContaining({
        commitServerIdentityConfigurationForm: expect.objectContaining({
          clientMode: "auto",
        }),
      }),
    );
  });

  it("holds Save while a replacement client is incomplete, even with header edits", async () => {
    connectClient();

    renderIdentity();
    fireEvent.click(screen.getByRole("button", { name: "Clear connection" }));
    fireEvent.click(
      screen.getByRole("radio", {
        name: "Manual",
        description: /client ID and secret/,
      }),
    );
    addCustomHeader("X-Team", "eng");

    const save = screen.getByRole("button", { name: "Save" });
    expect((save as HTMLButtonElement).disabled).toBe(true);
    expect(
      screen.getByText(
        "Finish the User Identity change, or cancel it, to save.",
      ),
    ).toBeDefined();
    fireEvent.click(save);
    await Promise.resolve();
    expect(mocks.create).not.toHaveBeenCalled();
    expect(mocks.commit).not.toHaveBeenCalled();
  });

  it("saves header edits when a cleared client is put back unchanged", async () => {
    connectClient();

    renderIdentity();
    fireEvent.click(screen.getByRole("button", { name: "Clear connection" }));
    // The only existing client is the connected one, so nothing changed.
    expect(
      screen
        .getByRole("radio", { name: /Existing client/ })
        .getAttribute("aria-checked"),
    ).toBe("true");
    addCustomHeader("X-Team", "eng");

    expect(
      screen.queryByText(
        "Finish the User Identity change, or cancel it, to save.",
      ),
    ).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(mocks.create).toHaveBeenCalledOnce());
    expect(mocks.commit).not.toHaveBeenCalled();
  });

  it("restores the connected client on cancel", () => {
    mocks.clients.mockReturnValue({
      items: [
        {
          id: "client-1",
          clientId: "dashboard-client",
          remoteSessionIssuerId: "provider-1",
          userSessionIssuerIds: ["user-session-issuer-1"],
          scope: ["read", "write"],
        },
      ],
      isLoading: false,
      isError: false,
      error: null,
    });
    mocks.issuers.mockReturnValue({
      data: {
        result: {
          items: [
            {
              id: "provider-1",
              name: "Example provider",
              issuer: "https://id.example",
              slug: "example",
              projectId: "project-1",
              clientIdMetadataDocumentSupported: true,
              authorizationEndpoint: "https://id.example/authorize",
              tokenEndpoint: "https://id.example/token",
            },
          ],
        },
      },
    });

    renderIdentity();
    fireEvent.click(screen.getByRole("button", { name: "Clear connection" }));
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

    expect(screen.getByText("Connected")).toBeDefined();
    expect(screen.queryByRole("radio", { name: /Existing client/ })).toBeNull();
  });

  it("does not offer Auto-Configure against a plain-http token endpoint", () => {
    mocks.issuers.mockReturnValue({
      data: {
        result: {
          items: [
            {
              id: "provider-1",
              name: "Linear",
              issuer: "https://mcp.linear.app",
              slug: "linear",
              projectId: "project-1",
              clientIdMetadataDocumentSupported: true,
              registrationEndpoint: "https://mcp.linear.app/register",
              authorizationEndpoint: "https://mcp.linear.app/authorize",
              tokenEndpoint: "http://mcp.linear.app/token",
            },
          ],
        },
      },
    });

    renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));

    expect(
      (
        screen.getByRole("radio", {
          name: /Auto-Configure/,
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  });

  it("offers DCR under Advanced when the provider supports both", async () => {
    mocks.issuers.mockReturnValue({
      data: {
        result: {
          items: [
            {
              id: "provider-1",
              name: "Linear",
              issuer: "https://mcp.linear.app",
              slug: "linear",
              projectId: "project-1",
              clientIdMetadataDocumentSupported: true,
              registrationEndpoint: "https://mcp.linear.app/register",
              authorizationEndpoint: "https://mcp.linear.app/authorize",
              tokenEndpoint: "https://mcp.linear.app/token",
            },
          ],
        },
      },
    });

    renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));
    fireEvent.click(screen.getByRole("button", { name: /Advanced/ }));
    fireEvent.click(screen.getByRole("button", { name: "DCR" }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(mocks.commit).toHaveBeenCalledOnce());
    expect(mocks.commit).toHaveBeenCalledWith(
      expect.objectContaining({
        commitServerIdentityConfigurationForm: expect.objectContaining({
          clientMode: "auto",
          registrationMethod: "dcr",
        }),
      }),
    );
  });

  it("sends the scopes chosen and typed for a manual client", async () => {
    mocks.issuers.mockReturnValue({
      data: {
        result: {
          items: [
            {
              id: "provider-1",
              name: "Linear",
              issuer: "https://mcp.linear.app",
              slug: "linear",
              projectId: "project-1",
              clientIdMetadataDocumentSupported: true,
              authorizationEndpoint: "https://mcp.linear.app/authorize",
              tokenEndpoint: "https://mcp.linear.app/token",
              scopesSupported: ["read", "admin"],
            },
          ],
        },
      },
    });
    // The provider matches by host, so the provider probe stays off and only
    // the scope probe, enabled in manual mode, reads the resource's scopes.
    mocks.protectedResourceMetadata.mockImplementation(
      (_id: unknown, enabled: unknown) =>
        enabled
          ? { status: "available", metadata: { scopesSupported: ["issues"] } }
          : { status: "idle", metadata: null },
    );

    renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));
    fireEvent.click(
      screen.getByRole("radio", {
        name: "Manual",
        description: /client ID and secret/,
      }),
    );
    fireEvent.change(screen.getByLabelText("Client ID"), {
      target: { value: "manual-client" },
    });
    fireEvent.click(screen.getByRole("button", { name: /Advanced/ }));
    fireEvent.click(screen.getByRole("combobox", { name: "Scope" }));

    // The resource's scopes lead, followed by the provider's.
    expect(
      screen
        .getAllByRole("option", { name: /not selected/ })
        .map((option) => option.textContent),
    ).toEqual(["issues", "read", "admin"]);

    fireEvent.click(screen.getByRole("option", { name: /^read,/ }));
    const search = screen.getByPlaceholderText("Search options...");
    fireEvent.change(search, { target: { value: "write  read" } });
    fireEvent.click(screen.getByRole("option", { name: /Create new option/ }));
    // Scopes are case-sensitive: Read is not the advertised read.
    fireEvent.change(search, { target: { value: "Read" } });
    fireEvent.click(screen.getByRole("option", { name: /Create new option/ }));

    // A typed scope joins the menu, so it can be found and removed there.
    expect(
      screen.getByRole("option", { name: /^write, selected/ }),
    ).toBeDefined();

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(mocks.commit).toHaveBeenCalledOnce());
    expect(mocks.commit).toHaveBeenCalledWith(
      expect.objectContaining({
        commitServerIdentityConfigurationForm: expect.objectContaining({
          clientMode: "manual",
          clientConfiguration: expect.objectContaining({
            clientId: "manual-client",
            scope: ["read", "write", "Read"],
          }),
        }),
      }),
    );
  });

  it("sends no scope when a manual client chooses none", async () => {
    mocks.issuers.mockReturnValue({
      data: {
        result: {
          items: [
            {
              id: "provider-1",
              name: "Linear",
              issuer: "https://mcp.linear.app",
              slug: "linear",
              projectId: "project-1",
              authorizationEndpoint: "https://mcp.linear.app/authorize",
              tokenEndpoint: "https://mcp.linear.app/token",
              scopesSupported: ["read", "admin"],
            },
          ],
        },
      },
    });
    // The resource's scopes are on offer in the picker, but only scopes the
    // operator picks are stored; none picked means none sent.
    mocks.protectedResourceMetadata.mockImplementation(
      (_id: unknown, enabled: unknown) =>
        enabled
          ? { status: "available", metadata: { scopesSupported: ["issues"] } }
          : { status: "idle", metadata: null },
    );

    renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));
    fireEvent.click(
      screen.getByRole("radio", {
        name: "Manual",
        description: /client ID and secret/,
      }),
    );
    fireEvent.change(screen.getByLabelText("Client ID"), {
      target: { value: "manual-client" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(mocks.commit).toHaveBeenCalledOnce());
    const form = mocks.commit.mock.calls[0]![0]
      .commitServerIdentityConfigurationForm as CommitServerIdentityConfigurationForm;
    expect(form.clientMode).toBe("manual");
    expect(form.clientConfiguration?.clientId).toBe("manual-client");
    expect(form.clientConfiguration?.scope).toBeUndefined();
    // The new client changes what the pin view resolves.
    await waitFor(() => expect(mocks.invalidateScopes).toHaveBeenCalled());
  });

  it("drops scopes chosen for a manual client when Auto-Configure is saved", async () => {
    mocks.issuers.mockReturnValue({
      data: {
        result: {
          items: [
            {
              id: "provider-1",
              name: "Linear",
              issuer: "https://mcp.linear.app",
              slug: "linear",
              projectId: "project-1",
              authorizationEndpoint: "https://mcp.linear.app/authorize",
              tokenEndpoint: "https://mcp.linear.app/token",
              registrationEndpoint: "https://mcp.linear.app/register",
              scopesSupported: ["read", "write", "admin"],
            },
          ],
        },
      },
    });

    renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));
    fireEvent.click(
      screen.getByRole("radio", {
        name: "Manual",
        description: /client ID and secret/,
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: /Advanced/ }));
    fireEvent.click(screen.getByRole("combobox", { name: "Scope" }));
    fireEvent.click(screen.getByRole("option", { name: /^admin,/ }));
    fireEvent.click(screen.getByRole("radio", { name: /Auto-Configure/ }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(mocks.commit).toHaveBeenCalledOnce());
    const form = mocks.commit.mock.calls[0]![0]
      .commitServerIdentityConfigurationForm as CommitServerIdentityConfigurationForm;
    expect(form.clientMode).toBe("auto");
    expect(form.clientConfiguration?.scope).toBeUndefined();
  });

  it("previews the Authorization header without revealing the credential", async () => {
    const { container } = renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: /Service Account/ }));

    const input = screen.getByLabelText("Token");
    fireEvent.change(input, { target: { value: "bearer-secret-value" } });

    expect((input as HTMLInputElement).type).toBe("password");
    const preview = screen.getByRole("status", {
      name: "Authorization preview",
    });
    expect(preview.textContent).toContain("Authorization:");
    expect(preview.textContent).toContain("Bearer");
    expect(container.textContent).not.toContain("bearer-secret-value");

    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(mocks.create).toHaveBeenCalledOnce());
    expect(mocks.create).toHaveBeenCalledWith(
      expect.objectContaining({
        request: expect.objectContaining({
          createServerHeaderForm: expect.objectContaining({
            name: "Authorization",
            value: "Bearer bearer-secret-value",
          }),
        }),
      }),
    );
    await waitFor(() => expect((input as HTMLInputElement).value).toBe(""));
  });

  it("labels Basic and Manual credential inputs without exposing their values", () => {
    const { container } = renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: /Service Account/ }));
    fireEvent.click(screen.getByRole("button", { name: "Basic" }));

    fireEvent.change(screen.getByLabelText("Username"), {
      target: { value: "operator" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "basic-secret" },
    });
    expect((screen.getByLabelText("Password") as HTMLInputElement).type).toBe(
      "password",
    );
    expect(container.textContent).not.toContain("basic-secret");
    expect(container.textContent).not.toContain("b3BlcmF0b3I6YmFzaWMtc2VjcmV0");

    fireEvent.click(screen.getByRole("button", { name: "Manual" }));
    fireEvent.change(screen.getByLabelText("Header value"), {
      target: { value: "Custom manual-secret" },
    });
    expect(
      (screen.getByLabelText("Header value") as HTMLInputElement).type,
    ).toBe("password");
    expect(container.textContent).not.toContain("manual-secret");
  });

  it("separates a changed credential from a usable one", () => {
    // A readable saved credential: complete, so the form is valid on load,
    // which is exactly where "valid" and "changed" part ways.
    mocks.headers.mockReturnValue({
      data: {
        headers: [
          configuredHeader({ value: "Bearer seeded", isSecret: false }),
        ],
      },
      isLoading: false,
      isError: false,
      error: null,
      refetch: mocks.refetchHeaders,
    });

    renderIdentity();

    expect((screen.getByLabelText("Token") as HTMLInputElement).value).toBe(
      "seeded",
    );
    // Unchanged, so there is nothing to save. The old flag conflated the two
    // and offered Save the moment the page loaded.
    const save = () =>
      screen.getByRole("button", { name: /^Save/ }) as HTMLButtonElement;
    expect(save().disabled).toBe(true);

    // Typing makes it dirty; typing the original back makes it clean again.
    const token = screen.getByLabelText("Token");
    fireEvent.change(token, { target: { value: "rotated" } });
    expect(save().disabled).toBe(false);
    fireEvent.change(token, { target: { value: "seeded" } });
    expect(save().disabled).toBe(true);
  });

  it("keeps password managers out of the credential fields", () => {
    renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: /Service Account/ }));
    fireEvent.click(screen.getByRole("button", { name: "Basic" }));

    // A username next to a password is exactly the shape a manager treats as a
    // login form, but these are credentials for the upstream service.
    for (const label of ["Username", "Password"]) {
      const field = screen.getByLabelText(label);
      expect(field.getAttribute("data-1p-ignore")).toBe("true");
      expect(field.getAttribute("data-lpignore")).toBe("true");
      expect(field.getAttribute("data-bwignore")).toBe("true");
      expect(field.getAttribute("data-form-type")).toBe("other");
    }
    // Browsers ignore autocomplete="off" on password inputs.
    expect(screen.getByLabelText("Password").getAttribute("autocomplete")).toBe(
      "new-password",
    );
    expect(screen.getByLabelText("Username").getAttribute("autocomplete")).toBe(
      "off",
    );
  });

  it("keeps Client Credentials visible but unselectable", () => {
    renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: /Service Account/ }));

    expect(screen.getByText("Coming soon")).toBeDefined();
    fireEvent.click(screen.getByRole("button", { name: /Client credentials/ }));
    // Still on Bearer: the segment advertises the roadmap, it does not switch.
    expect(screen.getByLabelText("Token")).toBeDefined();
  });

  it("blocks Service Account and describes legacy pass-through Authorization honestly", () => {
    mocks.headers.mockReturnValue({
      data: {
        headers: [
          configuredHeader({
            value: undefined,
            valueFromRequestHeader: "X-Legacy-Authorization",
            isSecret: false,
          }),
        ],
      },
      isLoading: false,
      isError: false,
      error: null,
      refetch: mocks.refetchHeaders,
    });

    renderIdentity();

    // One alert above the choice, not a second copy under it.
    expect(
      screen.getAllByText(/legacy pass-through Authorization header/i),
    ).toHaveLength(1);
    expect(
      (
        screen.getByRole("radio", {
          name: /Service Account/,
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  });

  it("locks identity only when an organization-wide issuer holds an organization client", () => {
    mocks.userSessionIssuer.mockReturnValue({
      data: { id: "user-session-issuer-1", projectId: "" },
    });
    mocks.clients.mockReturnValue({
      items: [
        {
          id: "client-1",
          clientId: "org-client",
          projectId: "",
          remoteSessionIssuerId: "provider-1",
          userSessionIssuerIds: ["user-session-issuer-1"],
        },
      ],
      isLoading: false,
      isError: false,
      error: null,
    });

    renderIdentity();

    // The server refuses to change a binding every project shares, so the
    // panel says who can and links to where they would do it.
    expect(screen.getByText(/shared by every project/i)).toBeDefined();
    expect(
      screen
        .getByRole("link", { name: /client's MCP servers/ })
        .getAttribute("href"),
    ).toBe("/providers/provider-1/clients/client-1/mcp-servers");
    expect(
      (
        screen.getByRole("radio", {
          name: /Service Account/,
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    cleanup();

    // A project-owned client on the same issuer is this project's to change.
    mocks.clients.mockReturnValue({
      items: [
        {
          id: "client-2",
          clientId: "project-client",
          projectId: "project-1",
          remoteSessionIssuerId: "provider-1",
          userSessionIssuerIds: ["user-session-issuer-1"],
        },
      ],
      isLoading: false,
      isError: false,
      error: null,
    });
    renderIdentity();
    expect(screen.queryByText(/shared by every project/i)).toBeNull();
    expect(
      (
        screen.getByRole("radio", {
          name: /Service Account/,
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(false);
  });

  it("renders query failures as indeterminate instead of No Identity", () => {
    mocks.clients.mockReturnValue({
      items: [],
      isLoading: false,
      isError: true,
      error: new Error("client lookup failed"),
    });

    renderIdentity();

    expect(
      screen.getByText(/Could not determine the current identity/i),
    ).toBeDefined();
    expect(screen.getByText("Identity is unavailable.")).toBeDefined();
    expect(
      screen.queryByRole("radio", {
        name: "Manual",
        description: /static headers/,
      }),
    ).toBeNull();
  });

  it("says why identity is locked when the issuer fails to load", () => {
    mocks.userSessionIssuer.mockReturnValue({
      data: undefined,
      isLoading: false,
      isError: true,
    });

    renderIdentity();

    expect(
      screen.getByText(/Could not load this server's user session issuer/i),
    ).toBeDefined();
  });

  it("fails closed without the target-specific mcp:write grant", () => {
    mocks.rbac.mockReturnValue({
      isLoading: false,
      hasScope: () => false,
      hasAllScopes: () => false,
      hasAnyScope: () => false,
    });

    renderIdentity();

    expect(
      (
        screen.getByRole("radio", {
          name: /Service Account/,
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    fireEvent.click(screen.getByRole("radio", { name: /Service Account/ }));
    expect(screen.queryByLabelText("Token")).toBeNull();
  });

  it("checks mode changes against the MCP target resource", () => {
    renderIdentity();

    expect(mocks.hasScope).toHaveBeenCalledWith("mcp:write", "mcp-server-1");
  });

  it("fails closed while RBAC grants are loading", () => {
    mocks.rbac.mockReturnValue({
      isLoading: true,
      hasScope: () => true,
      hasAllScopes: () => true,
      hasAnyScope: () => true,
    });

    renderIdentity();

    expect(
      (
        screen.getByRole("radio", {
          name: /Service Account/,
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  });

  it("removes the Service Account credential only once Save is confirmed", async () => {
    mocks.headers.mockReturnValue({
      data: { headers: [configuredHeader()] },
      isLoading: false,
      isError: false,
      error: null,
      refetch: mocks.refetchHeaders,
    });

    renderIdentity();
    fireEvent.click(
      screen.getByRole("radio", {
        name: "Manual",
        description: /static headers/,
      }),
    );
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(mocks.remove).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(screen.getByRole("dialog")).toBeDefined();

    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() =>
      expect(mocks.remove).toHaveBeenCalledWith({
        request: { id: "header-1" },
      }),
    );
  });

  it("discloses both removals when No Identity unlinks a provider and a credential", () => {
    mocks.headers.mockReturnValue({
      data: { headers: [configuredHeader()] },
      isLoading: false,
      isError: false,
      error: null,
      refetch: mocks.refetchHeaders,
    });
    mocks.clients.mockReturnValue({
      items: [
        {
          id: "client-1",
          clientId: "dashboard-client",
          remoteSessionIssuerId: "provider-1",
          userSessionIssuerIds: ["user-session-issuer-1"],
        },
      ],
      isLoading: false,
      isError: false,
      error: null,
    });

    renderIdentity();
    fireEvent.click(
      screen.getByRole("radio", {
        name: "Manual",
        description: /static headers/,
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    const dialog = screen.getByRole("dialog");
    expect(dialog.textContent).toContain("unlinks the identity provider");
    expect(dialog.textContent).toContain(
      "removes the static Authorization credential",
    );
  });

  it("lets Custom Headers own a credential row it already removed", async () => {
    mocks.headers.mockReturnValue({
      data: { headers: [configuredHeader()] },
      isLoading: false,
      isError: false,
      error: null,
      refetch: mocks.refetchHeaders,
    });

    renderIdentity();
    fireEvent.click(
      screen.getByRole("radio", {
        name: "Manual",
        description: /static headers/,
      }),
    );
    fireEvent.click(screen.getByText("Custom Headers"));
    fireEvent.click(
      screen.getByRole("button", { name: "Remove header Authorization" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    // One delete, from the header save: a second one from the identity side
    // would fail on a row that is already gone.
    await waitFor(() =>
      expect(mocks.remove).toHaveBeenCalledWith({
        request: { id: "header-1" },
      }),
    );
    expect(mocks.remove).toHaveBeenCalledTimes(1);
  });

  it("does not report the identity removed when the deferred delete fails", async () => {
    mocks.headers.mockReturnValue({
      data: { headers: [configuredHeader()] },
      isLoading: false,
      isError: false,
      error: null,
      refetch: mocks.refetchHeaders,
    });
    mocks.remove.mockRejectedValue(new Error("delete refused"));

    renderIdentity();
    fireEvent.click(
      screen.getByRole("radio", {
        name: "Manual",
        description: /static headers/,
      }),
    );
    fireEvent.click(screen.getByText("Custom Headers"));
    fireEvent.click(
      screen.getByRole("button", { name: "Remove header Authorization" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    // The header save owns this delete; until it lands the credential is
    // still live upstream, so success must not be claimed.
    await waitFor(() => expect(mocks.remove).toHaveBeenCalledOnce());
    expect(mocks.toastSuccess).not.toHaveBeenCalledWith("Identity removed");
  });

  it("links shared-source guidance to identity provider management", () => {
    mocks.siblings.mockReturnValue({
      data: {
        mcpServers: [
          { id: "mcp-server-1", remoteMcpServerId: "remote-source-1" },
          { id: "mcp-server-2", remoteMcpServerId: "remote-source-1" },
        ],
      },
      isLoading: false,
      isError: false,
    });

    renderIdentity();

    expect(
      screen
        .getByRole("link", { name: "Remote Identity Providers" })
        .getAttribute("href"),
    ).toBe("/remote-identity-providers");
  });

  describe("pinned scopes", () => {
    it("shows the pin under the connected provider for a writer", () => {
      connectClient();
      mocks.scopes.mockReturnValue({ data: serverScopes(), isError: false });

      renderIdentity();

      expect(mocks.scopes).toHaveBeenCalledWith(
        { mcpServerId: "mcp-server-1" },
        undefined,
        expect.objectContaining({ enabled: true }),
      );
      expect(
        screen.getByRole("combobox", { name: "Pinned scopes" }),
      ).toBeDefined();
      expect(screen.getByText("read")).toBeDefined();
      expect(screen.getByText("Sign-ins request these scopes.")).toBeDefined();
    });

    it("neither fetches nor shows the pin without mcp:write", () => {
      connectClient();
      mocks.scopes.mockReturnValue({ data: serverScopes(), isError: false });
      mocks.rbac.mockReturnValue({
        isLoading: false,
        hasScope: () => false,
        hasAllScopes: () => false,
        hasAnyScope: () => false,
      });

      renderIdentity();

      for (const call of mocks.scopes.mock.calls) {
        expect(call[2]).toMatchObject({ enabled: false });
      }
      expect(
        screen.queryByRole("combobox", { name: "Pinned scopes" }),
      ).toBeNull();
    });

    it("saves an added scope with the section's Save", async () => {
      connectClient();
      mocks.scopes.mockReturnValue({ data: serverScopes(), isError: false });

      renderIdentity();
      const save = screen.getByRole("button", { name: "Save" });
      expect((save as HTMLButtonElement).disabled).toBe(true);

      fireEvent.click(screen.getByRole("combobox", { name: "Pinned scopes" }));
      // Advertised scopes and the provider's are both offered.
      expect(
        screen
          .getAllByRole("option", { name: /not selected/ })
          .map((option) => option.textContent),
      ).toEqual(["write", "profile"]);
      fireEvent.click(screen.getByRole("option", { name: /^write,/ }));
      fireEvent.click(screen.getByRole("button", { name: "Save" }));

      await waitFor(() => expect(mocks.setPin).toHaveBeenCalledOnce());
      expect(mocks.setPin).toHaveBeenCalledWith({
        request: {
          setServerScopePinRequestBody: {
            mcpServerId: "mcp-server-1",
            scopes: ["read", "write"],
          },
        },
      });
      expect(mocks.commit).not.toHaveBeenCalled();
      await waitFor(() =>
        expect(mocks.toastSuccess).toHaveBeenCalledWith(
          "Pinned scopes updated",
        ),
      );
    });

    it("writes the saved pin into the view without a refetch", async () => {
      connectClient();
      mocks.scopes.mockReturnValue({ data: serverScopes(), isError: false });

      renderIdentity();
      fireEvent.click(screen.getByRole("combobox", { name: "Pinned scopes" }));
      fireEvent.click(screen.getByRole("option", { name: /^write,/ }));
      fireEvent.click(screen.getByRole("button", { name: "Save" }));

      await waitFor(() =>
        expect(mocks.toastSuccess).toHaveBeenCalledWith(
          "Pinned scopes updated",
        ),
      );
      expect(mocks.setScopesData).toHaveBeenCalledOnce();
      expect(mocks.setScopesData).toHaveBeenCalledWith(
        expect.anything(),
        [{ mcpServerId: "mcp-server-1" }],
        serverScopes({ pinnedScopes: ["read", "write"] }),
      );
      expect(mocks.invalidateScopes).not.toHaveBeenCalled();
    });

    it("leaves the pin view alone when headers are saved", async () => {
      connectClient();
      mocks.scopes.mockReturnValue({ data: serverScopes(), isError: false });

      renderIdentity();
      addCustomHeader("X-Team", "eng");
      fireEvent.click(screen.getByRole("button", { name: "Save" }));

      await waitFor(() =>
        expect(mocks.toastSuccess).toHaveBeenCalledWith(
          "Upstream headers updated",
        ),
      );
      expect(mocks.create).toHaveBeenCalledOnce();
      expect(mocks.invalidateScopes).not.toHaveBeenCalled();
    });

    it("keeps the saved pin when headers save alongside it", async () => {
      connectClient();
      mocks.scopes.mockReturnValue({ data: serverScopes(), isError: false });

      renderIdentity();
      fireEvent.click(screen.getByRole("combobox", { name: "Pinned scopes" }));
      fireEvent.click(screen.getByRole("option", { name: /^write,/ }));
      addCustomHeader("X-Team", "eng");
      fireEvent.click(screen.getByRole("button", { name: "Save" }));

      await waitFor(() =>
        expect(mocks.toastSuccess).toHaveBeenCalledWith(
          "Upstream headers updated",
        ),
      );
      expect(mocks.toastSuccess).toHaveBeenCalledWith("Pinned scopes updated");
      expect(mocks.create).toHaveBeenCalledOnce();
      expect(mocks.setScopesData).toHaveBeenCalledOnce();
      expect(mocks.invalidateScopes).not.toHaveBeenCalled();
    });

    it("lets a pin-only edit through a locked identity", async () => {
      sharedSource();
      connectClient();
      mocks.scopes.mockReturnValue({ data: serverScopes(), isError: false });

      renderIdentity();
      expect(
        (
          screen.getByRole("radio", {
            name: /Service Account/,
          }) as HTMLButtonElement
        ).disabled,
      ).toBe(true);
      const field = screen.getByRole("combobox", { name: "Pinned scopes" });
      expect((field as HTMLButtonElement).disabled).toBe(false);
      const save = screen.getByRole("button", { name: "Save" });
      expect((save as HTMLButtonElement).disabled).toBe(true);

      fireEvent.click(field);
      fireEvent.click(screen.getByRole("option", { name: /^write,/ }));
      expect((save as HTMLButtonElement).disabled).toBe(false);
      fireEvent.click(save);

      await waitFor(() => expect(mocks.setPin).toHaveBeenCalledOnce());
      expect(mocks.setPin).toHaveBeenCalledWith({
        request: {
          setServerScopePinRequestBody: {
            mcpServerId: "mcp-server-1",
            scopes: ["read", "write"],
          },
        },
      });
      expect(mocks.commit).not.toHaveBeenCalled();
      expect(mocks.create).not.toHaveBeenCalled();
    });

    it("keeps Save shut under a locked identity once headers are also edited", () => {
      sharedSource();
      connectClient();
      mocks.scopes.mockReturnValue({ data: serverScopes(), isError: false });

      renderIdentity();
      fireEvent.click(screen.getByRole("combobox", { name: "Pinned scopes" }));
      fireEvent.click(screen.getByRole("option", { name: /^write,/ }));
      addCustomHeader("X-Team", "eng");

      const save = screen.getByRole("button", { name: "Save" });
      expect((save as HTMLButtonElement).disabled).toBe(true);
    });

    it("saves a cleared pin with the flag off", async () => {
      connectClient();
      mocks.scopes.mockReturnValue({
        data: serverScopes({ discoveryEnabled: false }),
        isError: false,
      });

      renderIdentity();
      const field = screen.getByRole("combobox", { name: "Pinned scopes" });
      expect((field as HTMLButtonElement).disabled).toBe(true);
      const save = screen.getByRole("button", { name: "Save" });
      expect((save as HTMLButtonElement).disabled).toBe(true);

      fireEvent.click(
        screen.getByRole("button", { name: "Clear pinned scopes" }),
      );
      expect((save as HTMLButtonElement).disabled).toBe(false);
      fireEvent.click(save);

      await waitFor(() => expect(mocks.setPin).toHaveBeenCalledOnce());
      expect(mocks.setPin).toHaveBeenCalledWith({
        request: {
          setServerScopePinRequestBody: {
            mcpServerId: "mcp-server-1",
            scopes: [],
          },
        },
      });
    });

    it("drops a draft edited back to the saved pin", () => {
      connectClient();
      mocks.scopes.mockReturnValue({ data: serverScopes(), isError: false });

      const { rerenderIdentity } = renderIdentity();
      fireEvent.click(screen.getByRole("combobox", { name: "Pinned scopes" }));
      fireEvent.click(screen.getByRole("option", { name: /^write,/ }));
      fireEvent.click(screen.getByRole("option", { name: /^write, selected/ }));
      const save = screen.getByRole("button", { name: "Save" });
      expect((save as HTMLButtonElement).disabled).toBe(true);

      // A refetch now shows through instead of a stale draft.
      mocks.scopes.mockReturnValue({
        data: serverScopes({ pinnedScopes: ["read", "admin"] }),
        isError: false,
      });
      rerenderIdentity();
      expect(
        screen.getByRole("option", { name: /^admin, selected/ }),
      ).toBeDefined();
    });

    it("saves headers even when the pin save fails", async () => {
      connectClient();
      mocks.scopes.mockReturnValue({ data: serverScopes(), isError: false });
      mocks.setPin.mockRejectedValue(new Error("pin refused"));

      renderIdentity();
      fireEvent.click(screen.getByRole("combobox", { name: "Pinned scopes" }));
      fireEvent.click(screen.getByRole("option", { name: /^write,/ }));
      addCustomHeader("X-Team", "eng");
      fireEvent.click(screen.getByRole("button", { name: "Save" }));

      await waitFor(() =>
        expect(mocks.toastSuccess).toHaveBeenCalledWith(
          "Upstream headers updated",
        ),
      );
      expect(mocks.create).toHaveBeenCalledOnce();
      expect(mocks.toastError).toHaveBeenCalledWith("pin refused");
      expect(mocks.toastSuccess).not.toHaveBeenCalledWith(
        "Pinned scopes updated",
      );
    });

    it("saves the pin even when the header save fails", async () => {
      connectClient();
      mocks.scopes.mockReturnValue({ data: serverScopes(), isError: false });
      mocks.create.mockRejectedValue(new Error("header refused"));

      renderIdentity();
      fireEvent.click(screen.getByRole("combobox", { name: "Pinned scopes" }));
      fireEvent.click(screen.getByRole("option", { name: /^write,/ }));
      addCustomHeader("X-Team", "eng");
      fireEvent.click(screen.getByRole("button", { name: "Save" }));

      await waitFor(() =>
        expect(mocks.toastSuccess).toHaveBeenCalledWith(
          "Pinned scopes updated",
        ),
      );
      await waitFor(() =>
        expect(mocks.toastError).toHaveBeenCalledWith("header refused"),
      );
      expect(mocks.toastSuccess).not.toHaveBeenCalledWith(
        "Upstream headers updated",
      );
    });

    it("says so when the pin cannot be loaded", () => {
      connectClient();
      mocks.scopes.mockReturnValue({ data: undefined, isError: true });

      renderIdentity();

      expect(screen.getByText("Couldn't load pinned scopes.")).toBeDefined();
      expect(
        screen.queryByRole("combobox", { name: "Pinned scopes" }),
      ).toBeNull();
    });

    it("explains a refusal: the pin needs edit access to every server on the URL", () => {
      connectClient();
      mocks.scopes.mockReturnValue({
        data: undefined,
        isError: true,
        error: new GramError("permission denied", {
          response: new Response(null, { status: 403 }),
          request: new Request("https://app.getgram.ai/rpc/example"),
          body: "",
        }),
      });

      renderIdentity();

      expect(
        screen.getByText(
          "Pinned scopes are shared by every MCP server that uses this URL. You need edit access to all of them to view or change the pin.",
        ),
      ).toBeDefined();
      expect(screen.queryByText("Couldn't load pinned scopes.")).toBeNull();
    });

    it("says the pin is loading until it arrives", () => {
      connectClient();
      mocks.scopes.mockReturnValue({ data: undefined, isError: false });

      renderIdentity();

      expect(screen.getByText("Loading pinned scopes…")).toBeDefined();
      expect(
        screen.queryByRole("combobox", { name: "Pinned scopes" }),
      ).toBeNull();
      expect(screen.queryByText("Couldn't load pinned scopes.")).toBeNull();
    });

    it("shows no loading line outside User Identity", () => {
      mocks.scopes.mockReturnValue({ data: undefined, isError: false });

      renderIdentity();

      expect(screen.queryByText("Loading pinned scopes…")).toBeNull();
    });

    it("clears the pin with an empty list", async () => {
      connectClient();
      mocks.scopes.mockReturnValue({ data: serverScopes(), isError: false });

      renderIdentity();
      fireEvent.click(screen.getByRole("combobox", { name: "Pinned scopes" }));
      fireEvent.click(screen.getByRole("option", { name: /^read, selected/ }));
      fireEvent.click(screen.getByRole("button", { name: "Save" }));

      await waitFor(() => expect(mocks.setPin).toHaveBeenCalledOnce());
      expect(mocks.setPin).toHaveBeenCalledWith({
        request: {
          setServerScopePinRequestBody: {
            mcpServerId: "mcp-server-1",
            scopes: [],
          },
        },
      });
    });
  });
});
