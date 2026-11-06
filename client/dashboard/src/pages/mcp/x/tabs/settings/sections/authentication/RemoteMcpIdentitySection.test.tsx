import { ServiceError } from "@gram/client/models/errors/serviceerror.js";
import { useUserIdentityDraft } from "@/lib/remote-identity/drafts/useIdentityDraft";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  act,
  fireEvent,
  render,
  renderHook,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { RemoteMcpIdentitySectionBody } from "./RemoteMcpIdentitySection";
import type { AuthTarget } from "./authTarget";
import type { CommitServerIdentityConfigurationForm } from "@gram/client/models/components/commitserveridentityconfigurationform.js";

const mocks = vi.hoisted(() => ({
  githubClients: vi.fn(),
  detachGitHub: vi.fn(),
  headers: vi.fn(),
  sessions: vi.fn(),
  clients: vi.fn(),
  siblings: vi.fn(),
  impact: vi.fn(),
  associations: vi.fn(),
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
  detachAssociation: vi.fn(),
  userSessionIssuer: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
}));

vi.mock("@gram/client/react-query/authClients.js", () => ({
  useAuthClientsInfinite: (
    _request: unknown,
    _key: unknown,
    options: { enabled: boolean },
  ) => ({
    data: options.enabled
      ? { pages: [{ result: { items: mocks.githubClients() } }] }
      : undefined,
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  }),
}));
vi.mock("@/lib/remote-identity/queries/useCustomerManagedKeys", () => ({
  useCustomerManagedKeys: () => ({ entitled: false, isLoading: false }),
}));
vi.mock("sonner", () => ({
  toast: {
    success: mocks.toastSuccess,
    error: mocks.toastError,
    warning: vi.fn(),
  },
}));

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    authClients: { detachFromMcpServer: mocks.detachGitHub },
    remoteSessions: { commitServerIdentityConfiguration: mocks.commit },
    remoteSessionIssuers: { fetchMetadata: mocks.fetchMetadata },
    remoteMcp: {
      discoverProtectedResourceMetadata: mocks.discoverProtectedResource,
    },
  }),
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-1" }),
}));

vi.mock("@/hooks/useScopeCatalogue", () => ({
  useScopeCatalogue: () => ({
    catalogue: new Map(),
    tasks: [],
    isLoading: false,
    isError: false,
    refetch: () => {},
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

vi.mock("@gram/client/react-query/serverIdentityImpact.js", () => ({
  invalidateAllServerIdentityImpact: () => Promise.resolve(),
  useServerIdentityImpact: (
    request: unknown,
    _security: unknown,
    options: { enabled?: boolean },
  ) => mocks.impact(request, options),
}));

vi.mock("@gram/client/react-query/detachServerClientAssociation.js", () => ({
  useDetachServerClientAssociationMutation: () => ({
    mutateAsync: mocks.detachAssociation,
    isPending: false,
    error: null,
  }),
}));

vi.mock("@gram/client/react-query/serverClientAssociations.js", () => ({
  invalidateAllServerClientAssociations: () => Promise.resolve(),
  useServerClientAssociations: () => mocks.associations(),
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
  // Stands in for the server's tier and search filters over the listing; no
  // tier lists every one.
  useRemoteSessionIssuersInfinite: (request: {
    tier?: "project" | "organization" | "platform";
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
        (!request.tier || tier === request.tier) &&
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
  queryClient: QueryClient;
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
  return {
    ...result,
    queryClient,
    rerenderIdentity: () => result.rerender(tree()),
  };
}

const discoveryIssuer = "https://auth.example.test/";
const discoveryKey = ["remote-session-issuer-metadata", discoveryIssuer];
const discoveryMetadata = {
  issuer: discoveryIssuer,
  authorizationEndpoint: "https://auth.example.test/authorize",
  tokenEndpoint: "https://auth.example.test/token",
  registrationEndpoint: "https://auth.example.test/register",
  scopesSupported: ["read"],
};

function discoverUnsavedProvider(): void {
  mocks.protectedResourceMetadata.mockReturnValue({
    status: "available",
    metadata: { authorizationServers: [discoveryIssuer] },
  });
  mocks.fetchMetadata.mockResolvedValue(discoveryMetadata);
}

/** The correction form opens on a failed discovery and survives its recheck. */
async function recoverCorrectionForm(): Promise<void> {
  mocks.fetchMetadata.mockRejectedValueOnce(new Error("Discovery failed"));
  renderIdentity();
  fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));
  await screen.findByRole("alert");
  fireEvent.click(screen.getByRole("button", { name: "Discover" }));
  await waitFor(() =>
    expect(
      (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
        .disabled,
    ).toBe(false),
  );
  mocks.fetchMetadata.mockClear();
}

function renderIdentityDraft() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const result = renderHook(
    () =>
      useUserIdentityDraft({
        mcpServerId: "mcp-server-1",
        remoteMcpServerId: "remote-source-1",
        upstreamUrl: "https://resource.example.test/mcp",
        linkedClients: [],
        configured: false,
        enabled: true,
      }),
    {
      wrapper: ({ children }) => (
        <QueryClientProvider client={queryClient}>
          {children}
        </QueryClientProvider>
      ),
    },
  );
  return { ...result, queryClient };
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

// A service account client (credential_owner self) bound to this server.
function boundSelfClient(): void {
  mocks.clients.mockReturnValue({
    items: [
      {
        id: "self-client-1",
        clientId: "service-client",
        credentialOwner: "self",
        tokenEndpointAuthMethod: "client_secret_basic",
        remoteSessionIssuerId: "provider-1",
        userSessionIssuerIds: ["user-session-issuer-1"],
        scope: [],
      },
    ],
    isLoading: false,
    isError: false,
    error: null,
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

function swapClientWithImpact(
  servers: Array<Record<string, unknown>> | null,
  hiddenServerCount = 0,
): () => void {
  if (servers) {
    mocks.impact.mockReturnValue({
      data: { servers, hiddenServerCount },
      isLoading: false,
      isError: false,
    });
  }
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

  const { rerenderIdentity } = renderIdentity();
  fireEvent.click(screen.getByRole("button", { name: "Clear connection" }));
  fireEvent.click(screen.getByRole("radio", { name: /Auto-Configure/ }));
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  return rerenderIdentity;
}

const target: AuthTarget = {
  kind: "remote-mcp",
  slug: "remote-server",
  projectId: "project-1",
  permissionResourceId: "mcp-server-1",
  attachImpactTarget: { mcpServerId: "mcp-server-1" },
  supportsOrganizationIssuers: true,
  userSessionIssuerId: "user-session-issuer-1",
  remoteMcpServerId: "remote-source-1",
  invalidate: vi.fn(),
};

beforeEach(() => {
  // Unless a test says otherwise, every client on the issuer is this server's.
  mocks.associations.mockImplementation(() => {
    const items: Array<{ id: string }> = mocks.clients().items ?? [];
    return {
      data: {
        associations: items.map((client) => ({
          remoteSessionClientId: client.id,
          serverCount: 1,
        })),
        requiredClientIds: items.map((client) => client.id),
      },
    };
  });
  mocks.githubClients.mockReturnValue([]);
  mocks.detachGitHub.mockResolvedValue({});
  mocks.impact.mockReturnValue({
    data: { servers: [], hiddenServerCount: 0 },
    isLoading: false,
    isError: false,
  });
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
    (scope: string, resourceId?: string) =>
      scope === "org:read" ||
      scope === "org:admin" ||
      resourceId === "mcp-server-1",
  );
  mocks.create.mockResolvedValue({});
  mocks.update.mockResolvedValue({});
  mocks.remove.mockResolvedValue({});
  mocks.commit.mockResolvedValue({
    status: "registered",
    manualSetupRequired: false,
  });
  mocks.discoverProtectedResource.mockResolvedValue({ available: false });
  mocks.fetchMetadata.mockReset();
  mocks.invalidateHeaders.mockResolvedValue(undefined);
  mocks.detach.mockResolvedValue({});
  mocks.detachAssociation.mockResolvedValue({});
  mocks.authenticationProbe.mockReturnValue("available");
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

// A static credential format, picked from the Service Account panel's Static
// group so it can't be confused with the Manual identity mode.
function staticFormat(name: string): HTMLElement {
  return within(
    screen.getByRole("radiogroup", { name: "Static credential" }),
  ).getByRole("radio", { name: new RegExp(`^${name}`) });
}

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
      screen.getByRole("radio", { name: /Shared credential/ }),
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
    expect(screen.getByLabelText("Authorization server")).toBeDefined();
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
    const trigger = screen.getByLabelText("Authorization server");
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
    fireEvent.click(screen.getByLabelText("Authorization server"));

    // Every tier is its own query, so the catalog cannot crowd out the
    // project's own provider, and a tier with more pages offers them.
    expect(screen.getByText("Own provider")).toBeDefined();
    expect(screen.getByText("Catalog 0")).toBeDefined();
    fireEvent.click(screen.getByText("More platform authorization servers"));
    expect(mocks.tierLoadMore).toHaveBeenCalledWith("platform");

    fireEvent.change(
      screen.getByPlaceholderText("Search authorization servers…"),
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
      "Authorization server",
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
    fireEvent.click(screen.getByLabelText("Authorization server"));
    expect(
      screen.getByRole("option", { name: /Known provider/ }),
    ).toBeDefined();
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
      screen.getByText(/Couldn.t load this server.s authorization servers/),
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
      screen.getByText(/Couldn.t load this server.s authorization servers/),
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
      issuer: "https://github.com/login/oauth",
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

  describe("a client started from an existing one", () => {
    const stored = (overrides: Record<string, unknown> = {}) => ({
      id: "stored-client",
      clientId: "STORED-APP",
      credentialOwner: "subject",
      tokenEndpointAuthMethod: "client_secret_post",
      scope: ["read"],
      audience: "https://api.example.test",
      remoteSessionIssuerId: "provider-1",
      createdAt: new Date(0),
      ...overrides,
    });
    const withStored = (...clients: Array<Record<string, unknown>>) => {
      mocks.clients.mockReturnValue({
        items: clients,
        isLoading: false,
        isError: false,
        error: null,
      });
    };
    const startFrom = (id: string) => {
      const hook = renderIdentityDraft();
      act(() => hook.result.current.selectProvider("provider-1"));
      act(() => hook.result.current.selectChoice("existing"));
      act(() => hook.result.current.selectExisting(id));
      return hook;
    };
    const committed = () =>
      mocks.commit.mock.calls[0]![0].commitServerIdentityConfigurationForm;

    it("fills the form for review and has the server copy the secret into a new client", async () => {
      connectClient();
      withStored(stored());
      const { result } = startFrom("stored-client");

      // Nothing secret is read into the form: the field is blank.
      expect(result.current).toMatchObject({
        existingClientId: "stored-client",
        clientId: "STORED-APP",
        clientSecret: "",
        scopes: ["read"],
        copiesSecret: true,
        canSave: true,
      });
      await act(async () => expect(await result.current.save()).toBe(true));

      expect(committed()).toMatchObject({
        mcpServerId: "mcp-server-1",
        providerId: "provider-1",
        clientMode: "manual",
        clientConfiguration: {
          clientId: "STORED-APP",
          templateClientId: "stored-client",
          scope: ["read"],
          audience: "https://api.example.test",
          tokenEndpointAuthMethod: "client_secret_post",
        },
      });
      expect(committed().clientConfiguration.clientSecret).toBeUndefined();
      // The stored client is named as a starting point only, never linked.
      expect(committed()).not.toHaveProperty("existingClientId");
    });

    it("sends reviewed edits, and copies no secret for a different client ID", async () => {
      connectClient();
      withStored(stored());
      const { result } = startFrom("stored-client");
      act(() => {
        result.current.setClientId("ANOTHER-APP");
        result.current.setClientSecret("TYPED-SECRET");
        result.current.setScopes(["read", "write"]);
      });
      expect(result.current.copiesSecret).toBe(false);
      await act(async () => expect(await result.current.save()).toBe(true));

      expect(committed().clientConfiguration).toMatchObject({
        clientId: "ANOTHER-APP",
        clientSecret: "TYPED-SECRET",
        scope: ["read", "write"],
      });
      expect(committed().clientConfiguration.templateClientId).toBeUndefined();
    });

    it("lets the operator review and change the copied audience and auth method", async () => {
      connectClient();
      withStored(stored());
      const { result } = startFrom("stored-client");
      expect(result.current).toMatchObject({
        startingAudience: "https://api.example.test",
        startingAuthMethod: "client_secret_post",
      });
      act(() => {
        result.current.setStartingAudience("");
        result.current.setStartingAuthMethod("client_secret_basic");
      });
      await act(async () => expect(await result.current.save()).toBe(true));

      expect(committed().clientConfiguration).toMatchObject({
        templateClientId: "stored-client",
        tokenEndpointAuthMethod: "client_secret_basic",
      });
      expect(committed().clientConfiguration.audience).toBeUndefined();
    });

    it("holds Save when a method that sends a secret has none typed or copied", () => {
      connectClient();
      withStored(stored());
      const { result } = startFrom("stored-client");
      expect(result.current.canSave).toBe(true);

      act(() => result.current.setClientId("ANOTHER-APP"));
      expect(result.current.canSave).toBe(false);

      act(() => result.current.setClientSecret("TYPED-SECRET"));
      expect(result.current.canSave).toBe(true);
    });

    it("registers a hosted document client again instead of copying it", async () => {
      autoConfigurableProvider();
      withStored(
        stored({
          clientId: "https://app.example.test/oauth/client-metadata/stored",
          clientIdMetadataUri:
            "https://app.example.test/oauth/client-metadata/stored",
          tokenEndpointAuthMethod: "none",
        }),
      );
      const { result } = startFrom("stored-client");

      expect(result.current.clientId).toBe("");
      expect(result.current.copiesSecret).toBe(false);
      await act(async () => expect(await result.current.save()).toBe(true));

      expect(committed()).toMatchObject({
        clientMode: "auto",
        clientConfiguration: { scope: ["read"] },
      });
      expect(committed().clientConfiguration.clientId).toBeUndefined();
      expect(committed().clientConfiguration.templateClientId).toBeUndefined();
      expect(committed()).not.toHaveProperty("existingClientId");
    });

    it.each([
      ["a shared credential", { credentialOwner: "self" }],
      ["one the provider rejected", { upstreamRejectedAt: new Date(0) }],
      [
        "one an identity provider connection manages",
        { identityProviderConnectionId: "connection-1" },
      ],
      [
        "one that signs with a key set",
        { tokenEndpointAuthMethod: "private_key_jwt" },
      ],
      [
        "a hosted document the provider no longer supports",
        {
          clientIdMetadataUri:
            "https://app.example.test/oauth/client-metadata/stored",
        },
      ],
    ])("does not offer %s as a starting point", (_name, overrides) => {
      connectClient();
      withStored(stored(overrides));
      const { result } = renderIdentityDraft();
      act(() => result.current.selectProvider("provider-1"));

      expect(result.current.existingAvailable).toBe(false);
      expect(result.current.existingOptions).toEqual([]);
      act(() => result.current.selectChoice("existing"));
      expect(result.current.choice).toBe("manual");
    });

    it("never falls back to another client when the starting one goes away", async () => {
      connectClient();
      withStored(stored(), stored({ id: "other-client", clientId: "OTHER" }));
      const { result, rerender } = startFrom("stored-client");
      expect(result.current.canSave).toBe(true);

      withStored(stored({ id: "other-client", clientId: "OTHER" }));
      rerender();

      expect(result.current.existingClientId).toBeNull();
      expect(result.current.canSave).toBe(false);
      await act(async () => expect(await result.current.save()).toBe(false));
      expect(mocks.commit).not.toHaveBeenCalled();
    });

    it("drops the starting client's settings when another choice is made", () => {
      connectClient();
      withStored(stored());
      const { result } = startFrom("stored-client");

      act(() => result.current.selectChoice("manual"));

      expect(result.current).toMatchObject({
        choice: "manual",
        existingClientId: null,
        clientId: "",
        scopes: [],
        copiesSecret: false,
      });
    });

    it("says the new client is separate and never shows a stored secret", () => {
      connectClient();
      mocks.clients.mockReturnValue({
        items: [
          stored({
            id: "client-1",
            userSessionIssuerIds: ["user-session-issuer-1"],
          }),
        ],
        isLoading: false,
        isError: false,
        error: null,
      });
      renderIdentity();
      fireEvent.click(screen.getByRole("button", { name: "Clear connection" }));
      fireEvent.click(screen.getByRole("radio", { name: /New from existing/ }));

      expect(
        screen.getByText(/Saving creates a separate client for this server/),
      ).toBeDefined();
      fireEvent.click(screen.getByRole("combobox", { name: "Start from" }));
      fireEvent.click(screen.getByRole("option"));

      expect(
        (screen.getByLabelText("Client ID") as HTMLInputElement).value,
      ).toBe("STORED-APP");
      const secret = screen.getByLabelText("Client secret") as HTMLInputElement;
      expect(secret.value).toBe("");
      expect(secret.placeholder).toBe("Copied when you save");
    });
  });

  describe("unsaved provider discovery recovery", () => {
    it("offers no correction while discovery is healthy", async () => {
      discoverUnsavedProvider();
      renderIdentity();
      fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));
      await waitFor(() =>
        expect(
          (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
            .disabled,
        ).toBe(false),
      );
      expect(
        screen.queryByRole("group", { name: "Issuer correction" }),
      ).toBeNull();
      expect(screen.queryByLabelText("Issuer URL")).toBeNull();
    });

    it("keeps typed credentials on screen through a background refresh and holds Save", async () => {
      discoverUnsavedProvider();
      const { rerenderIdentity } = renderIdentity();
      fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));
      await waitFor(() =>
        expect(
          (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
            .disabled,
        ).toBe(false),
      );
      fireEvent.click(
        screen.getByRole("radio", {
          name: "Manual",
          description: /client ID and secret/,
        }),
      );
      fireEvent.change(screen.getByLabelText("Client ID"), {
        target: { value: "EXAMPLE-CLIENT" },
      });
      const settled = mocks.clients();
      mocks.clients.mockReturnValue({
        ...settled,
        isLoading: true,
        isRefreshing: true,
      });
      rerenderIdentity();
      expect(screen.queryByText("Loading identity…")).toBeNull();
      expect(
        (screen.getByLabelText("Client ID") as HTMLInputElement).value,
      ).toBe("EXAMPLE-CLIENT");
      expect(
        (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
          .disabled,
      ).toBe(true);
      mocks.clients.mockReturnValue(settled);
      rerenderIdentity();
      expect(
        (screen.getByLabelText("Client ID") as HTMLInputElement).value,
      ).toBe("EXAMPLE-CLIENT");
    });

    it("clears editor success on edits until explicit discovery validates the applied URL again", async () => {
      discoverUnsavedProvider();
      await recoverCorrectionForm();
      const panel = screen.getByRole("group", { name: "Issuer correction" });
      const success = "OAuth metadata discovered. Save to finish setup.";
      expect(within(panel).getByRole("status").textContent).toBe(success);
      const input = within(panel).getByLabelText(
        "Issuer URL",
      ) as HTMLInputElement;
      expect(input.value).toBe(discoveryIssuer);
      expect(
        within(panel).getByRole("button", { name: "Check again" }),
      ).toBeDefined();
      const corrected = "https://auth.example.test";
      fireEvent.change(input, { target: { value: corrected } });
      expect(within(panel).queryByText(success)).toBeNull();
      expect(
        within(panel).queryByRole("button", { name: "Check again" }),
      ).toBeNull();
      expect(mocks.fetchMetadata).not.toHaveBeenCalled();
      // Even reverting the text requires an explicit check before editor success returns.
      fireEvent.change(input, { target: { value: discoveryIssuer } });
      expect(within(panel).queryByText(success)).toBeNull();
      fireEvent.change(input, { target: { value: corrected } });
      mocks.fetchMetadata.mockResolvedValue({
        ...discoveryMetadata,
        issuer: corrected,
      });
      fireEvent.click(within(panel).getByRole("button", { name: "Discover" }));
      await waitFor(() =>
        expect(within(panel).getByRole("status").textContent).toBe(success),
      );
      expect(input.value).toBe(corrected);
      expect(mocks.fetchMetadata).toHaveBeenLastCalledWith({
        fetchIssuerMetadataRequestBody: { issuer: corrected },
      });
      expect(
        within(panel).getByRole("button", { name: "Check again" }),
      ).toBeDefined();
      expect(mocks.commit).not.toHaveBeenCalled();
    });

    it("uses Check again for same-issuer loading, failure, and recovery without closing the form", async () => {
      discoverUnsavedProvider();
      await recoverCorrectionForm();
      const panel = screen.getByRole("group", { name: "Issuer correction" });
      let reject!: (error: Error) => void;
      mocks.fetchMetadata.mockImplementationOnce(
        () =>
          new Promise((_, fail) => {
            reject = fail;
          }),
      );
      fireEvent.click(
        within(panel).getByRole("button", { name: "Check again" }),
      );
      await waitFor(() =>
        expect(within(panel).getByRole("status").textContent).toBe(
          "Checking OAuth metadata",
        ),
      );
      const checking = within(panel).getByRole("button", {
        name: "Checking…",
      }) as HTMLButtonElement;
      expect(checking.disabled).toBe(true);
      expect(
        (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
          .disabled,
      ).toBe(true);
      expect(within(panel).queryByText(/OAuth metadata discovered/)).toBeNull();
      fireEvent.click(checking);
      expect(mocks.fetchMetadata).toHaveBeenCalledTimes(1);
      await act(async () => reject(new Error("synthetic failure")));
      expect((await screen.findByRole("alert")).textContent).toContain(
        "Could not discover OAuth metadata",
      );
      expect(within(panel).queryByRole("status")).toBeNull();
      expect(
        (within(panel).getByLabelText("Issuer URL") as HTMLInputElement).value,
      ).toBe(discoveryIssuer);
      expect(
        (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
          .disabled,
      ).toBe(true);
      fireEvent.click(within(panel).getByRole("button", { name: "Discover" }));
      await waitFor(() =>
        expect(within(panel).getByRole("status").textContent).toBe(
          "OAuth metadata discovered. Save to finish setup.",
        ),
      );
      expect(screen.queryByRole("alert")).toBeNull();
      expect(
        within(panel).getByRole("button", { name: "Check again" }),
      ).toBeDefined();
      expect(mocks.fetchMetadata).toHaveBeenCalledTimes(2);
      expect(mocks.fetchMetadata).toHaveBeenLastCalledWith({
        fetchIssuerMetadataRequestBody: { issuer: discoveryIssuer },
      });
      expect(mocks.commit).not.toHaveBeenCalled();
    });

    it("keeps the automatically shown correction form open after Reset succeeds", async () => {
      discoverUnsavedProvider();
      mocks.fetchMetadata.mockRejectedValueOnce(new Error("Discovery failed"));
      renderIdentity();
      fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));
      await screen.findByRole("alert");
      fireEvent.change(screen.getByLabelText("Issuer URL"), {
        target: { value: "https://other.example.test/" },
      });
      expect(mocks.fetchMetadata).toHaveBeenCalledOnce();
      fireEvent.click(
        screen.getByRole("button", { name: "Reset to advertised URL" }),
      );
      await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
      expect(
        (screen.getByLabelText("Issuer URL") as HTMLInputElement).value,
      ).toBe(discoveryIssuer);
      expect(
        screen.getByRole("button", { name: "Reset to advertised URL" }),
      ).toBeDefined();
      expect(mocks.fetchMetadata).toHaveBeenCalledTimes(2);
      expect(mocks.fetchMetadata).toHaveBeenLastCalledWith({
        fetchIssuerMetadataRequestBody: { issuer: discoveryIssuer },
      });
    });

    it("hides inline correction when the corrected host selects a saved provider", async () => {
      discoverUnsavedProvider();
      mocks.hostIssuers.mockImplementation((host: string) =>
        host === "saved.example.test"
          ? [
              {
                id: "saved-example",
                name: "Saved example",
                slug: "saved-example",
                issuer: "https://saved.example.test/stored",
              },
            ]
          : undefined,
      );
      await recoverCorrectionForm();
      fireEvent.change(screen.getByLabelText("Issuer URL"), {
        target: { value: "https://saved.example.test/corrected" },
      });
      fireEvent.click(screen.getByRole("button", { name: "Discover" }));
      await waitFor(() =>
        expect(
          screen.getByLabelText("Authorization server").textContent,
        ).toContain("Saved example"),
      );
      expect(screen.queryByLabelText("Issuer URL")).toBeNull();
      expect(screen.queryByText("Will be created")).toBeNull();
      expect(mocks.fetchMetadata).not.toHaveBeenCalledWith({
        fetchIssuerMetadataRequestBody: {
          issuer: "https://saved.example.test/corrected",
        },
      });
    });

    it("disables provider and correction controls while committing", async () => {
      discoverUnsavedProvider();
      let finishCommit!: (value: { status: string }) => void;
      mocks.commit.mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            finishCommit = resolve;
          }),
      );
      await recoverCorrectionForm();
      fireEvent.click(screen.getByRole("button", { name: "Save" }));
      await waitFor(() => expect(mocks.commit).toHaveBeenCalledOnce());
      for (const label of ["Authorization server", "Issuer URL"]) {
        expect(
          (screen.getByLabelText(label) as HTMLInputElement).disabled,
        ).toBe(true);
      }
      for (const name of ["Check again", "Reset to advertised URL"]) {
        expect(
          (screen.getByRole("button", { name }) as HTMLButtonElement).disabled,
        ).toBe(true);
      }
      await act(async () => finishCommit({ status: "registered" }));
    });

    it("recovers a mismatch through explicit correction and submits only corrected DCR metadata", async () => {
      discoverUnsavedProvider();
      const issuer = "https://auth.example.test";
      const correctedMetadata = {
        issuer,
        authorizationEndpoint: `${issuer}/oauth/authorize`,
        tokenEndpoint: `${issuer}/oauth/token`,
        registrationEndpoint: `${issuer}/oauth/register`,
        scopesSupported: ["corrected.read"],
        tokenEndpointAuthMethodsSupported: ["client_secret_post"],
      };
      mocks.fetchMetadata.mockResolvedValueOnce(correctedMetadata);
      mocks.fetchMetadata.mockResolvedValue(correctedMetadata);
      const { queryClient } = renderIdentity();
      fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));
      await screen.findByRole("alert");
      expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
      expect(
        screen.queryByText(
          "Use Discover in the issuer correction form to try again, or correct the URL to match the provider's documented issuer.",
        ),
      ).toBeNull();
      expect(
        (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
          .disabled,
      ).toBe(true);
      const input = screen.getByRole("textbox", { name: "Issuer URL" });
      expect(document.activeElement).toBe(input);
      expect(
        within(
          screen.getByRole("group", { name: "Issuer correction" }),
        ).getByRole("textbox", { name: "Issuer URL" }),
      ).toBe(input);
      expect(input.getAttribute("aria-describedby")).toBeTruthy();
      fireEvent.change(input, { target: { value: `  ${issuer}  ` } });
      expect(mocks.fetchMetadata).toHaveBeenCalledOnce();
      fireEvent.click(screen.getByRole("button", { name: "Discover" }));
      await waitFor(() =>
        expect(
          (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
            .disabled,
        ).toBe(false),
      );
      expect(screen.queryByRole("alert")).toBeNull();
      expect(
        screen.getByRole("button", { name: "Reset to advertised URL" }),
      ).toBeDefined();
      expect(
        queryClient.getQueryData(["remote-session-issuer-metadata", issuer]),
      ).toEqual(correctedMetadata);
      fireEvent.click(screen.getByRole("button", { name: "Save" }));
      await waitFor(() => expect(mocks.commit).toHaveBeenCalledOnce());
      expect(mocks.commit).toHaveBeenCalledWith({
        commitServerIdentityConfigurationForm: expect.objectContaining({
          providerId: undefined,
          clientMode: "auto",
          createProvider: expect.objectContaining(correctedMetadata),
          clientConfiguration: expect.objectContaining({
            scope: undefined,
            tokenEndpointAuthMethod: "client_secret_post",
          }),
        }),
      });
      expect(mocks.fetchMetadata).toHaveBeenCalledTimes(2);
    });

    it("labels correction errors and keeps Reset available after successful correction", async () => {
      discoverUnsavedProvider();
      await recoverCorrectionForm();
      fireEvent.change(screen.getByLabelText("Issuer URL"), {
        target: { value: "http://auth.example.test" },
      });
      fireEvent.click(screen.getByRole("button", { name: "Discover" }));
      expect(screen.getByRole("alert").textContent).toContain(
        "absolute HTTPS issuer URL",
      );
      expect(
        screen.getByLabelText("Issuer URL").getAttribute("aria-invalid"),
      ).toBe("true");
      expect(mocks.fetchMetadata).not.toHaveBeenCalled();
      const issuer = "https://other.example.test/tenant/";
      mocks.fetchMetadata.mockResolvedValue({ ...discoveryMetadata, issuer });
      fireEvent.change(screen.getByLabelText("Issuer URL"), {
        target: { value: issuer },
      });
      fireEvent.click(screen.getByRole("button", { name: "Discover" }));
      await waitFor(() =>
        expect(
          (
            screen.getByRole("button", {
              name: "Check again",
            }) as HTMLButtonElement
          ).disabled,
        ).toBe(false),
      );
      expect(screen.queryByRole("alert")).toBeNull();
      mocks.fetchMetadata.mockResolvedValue(discoveryMetadata);
      fireEvent.click(
        screen.getByRole("button", { name: "Reset to advertised URL" }),
      );
      expect(
        (screen.getByLabelText("Issuer URL") as HTMLInputElement).value,
      ).toBe(discoveryIssuer);
      expect(
        screen.getByRole("button", { name: "Reset to advertised URL" }),
      ).toBeDefined();
    });

    it("replaces an explicitly picked unsaved sentinel with a saved host match and its clients", async () => {
      discoverUnsavedProvider();
      const saved = {
        id: "saved-example-provider",
        name: "Saved example provider",
        slug: "saved-example",
        issuer: "https://saved.example.test/stored-path/",
        projectId: "example-project",
      };
      mocks.hostIssuers.mockImplementation((host: string) =>
        host === "saved.example.test" ? [saved] : undefined,
      );
      mocks.clients.mockImplementation(() => ({
        items: [],
        isLoading: false,
      }));
      const { result } = renderIdentityDraft();
      await waitFor(() => expect(result.current.discoveryStatus).toBe("ready"));
      act(() => result.current.selectProvider("__discovered__"));
      act(() =>
        result.current.setIssuerEditorText(
          "https://saved.example.test/corrected-path",
        ),
      );
      mocks.clients.mockReturnValue({
        items: [
          {
            id: "saved-example-client",
            clientId: "EXAMPLE-CLIENT",
            credentialOwner: "subject",
            remoteSessionIssuerId: saved.id,
            createdAt: new Date(0),
          },
        ],
        isLoading: false,
      });
      act(() => result.current.discoverIssuer());
      await waitFor(() => expect(result.current.selected?.id).toBe(saved.id));
      expect(result.current.selected?.isNew).toBe(false);
      expect(result.current.choice).toBe("existing");
      // A stored client is a starting point the operator picks, never a
      // default that Save would use unseen.
      expect(result.current.existingClientId).toBeNull();
      expect(result.current.canSave).toBe(false);
      act(() => result.current.selectExisting("saved-example-client"));
      await act(async () => expect(await result.current.save()).toBe(true));
      const committed =
        mocks.commit.mock.calls[0]![0].commitServerIdentityConfigurationForm;
      expect(committed).toMatchObject({
        providerId: saved.id,
        clientMode: "manual",
        clientConfiguration: {
          clientId: "EXAMPLE-CLIENT",
          templateClientId: "saved-example-client",
        },
      });
      expect(committed.createProvider).toBeUndefined();
      expect(committed).not.toHaveProperty("existingClientId");
      expect(saved.issuer).toBe("https://saved.example.test/stored-path/");
      expect(mocks.fetchMetadata).toHaveBeenCalledOnce();
    });

    it.each(["manual", "existing"] as const)(
      "preserves saved-provider %s submission without discovery",
      async (mode) => {
        autoConfigurableProvider();
        if (mode === "existing") {
          mocks.clients.mockReturnValue({
            items: [
              {
                id: "example-existing-client",
                clientId: "EXAMPLE-CLIENT",
                credentialOwner: "subject",
                remoteSessionIssuerId: "provider-1",
                createdAt: new Date(0),
              },
            ],
            isLoading: false,
          });
        }
        const { result } = renderIdentityDraft();
        act(() => result.current.selectProvider("provider-1"));
        act(() => {
          result.current.selectChoice(mode);
          if (mode === "manual")
            result.current.setClientId("EXAMPLE-MANUAL-CLIENT");
        });
        if (mode === "existing") {
          act(() => result.current.selectExisting("example-existing-client"));
        }
        expect(result.current.canSave).toBe(true);
        await act(async () => expect(await result.current.save()).toBe(true));
        const committed =
          mocks.commit.mock.calls[0]![0].commitServerIdentityConfigurationForm;
        // Starting from a stored client still creates one: it is never linked.
        expect(committed).toMatchObject({
          providerId: "provider-1",
          clientMode: "manual",
        });
        expect(committed.createProvider).toBeUndefined();
        expect(committed).not.toHaveProperty("existingClientId");
        expect(committed.clientConfiguration.templateClientId).toBe(
          mode === "existing" ? "example-existing-client" : undefined,
        );
        expect(mocks.fetchMetadata).not.toHaveBeenCalled();
      },
    );

    it("requests and trusts the whitespace-trimmed issuer without URL serialization", async () => {
      discoverUnsavedProvider();
      mocks.protectedResourceMetadata.mockReturnValue({
        status: "available",
        metadata: { authorizationServers: [`  ${discoveryIssuer}  `] },
      });
      const { result, queryClient } = renderIdentityDraft();
      await waitFor(() => expect(result.current.discoveryStatus).toBe("ready"));
      expect(mocks.fetchMetadata).toHaveBeenCalledWith({
        fetchIssuerMetadataRequestBody: { issuer: discoveryIssuer },
      });
      expect(queryClient.getQueryData(discoveryKey)).toEqual(discoveryMetadata);
    });

    it("waits for metadata without defaulting to Manual or claiming no support", async () => {
      discoverUnsavedProvider();
      let resolveMetadata!: (value: typeof discoveryMetadata) => void;
      mocks.fetchMetadata.mockImplementation(
        () =>
          new Promise((resolve) => {
            resolveMetadata = resolve;
          }),
      );
      renderIdentity();
      fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));

      expect(screen.getByRole("status").textContent).toContain(
        "Checking OAuth metadata",
      );
      expect(
        screen.getByLabelText("Checking automatic registration."),
      ).toBeDefined();
      expect(screen.queryByLabelText("Client ID")).toBeNull();
      expect(
        screen
          .getByRole("radio", {
            name: "Manual",
            description: /client ID and secret/,
          })
          .getAttribute("aria-checked"),
      ).toBe("false");
      expect(
        (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
          .disabled,
      ).toBe(true);
      expect(screen.queryByText(/supports neither/)).toBeNull();
      fireEvent.click(screen.getByRole("button", { name: "Save" }));
      expect(mocks.commit).not.toHaveBeenCalled();
      await act(async () => resolveMetadata(discoveryMetadata));
      await waitFor(() =>
        expect(
          screen
            .getByRole("radio", { name: /Auto-Configure/ })
            .getAttribute("aria-checked"),
        ).toBe("true"),
      );
    });

    it("shows the public SDK error as code and retries unchanged issuer discovery through the correction form", async () => {
      discoverUnsavedProvider();
      const message =
        "Metadata issuer does not exactly match the requested issuer.";
      mocks.fetchMetadata.mockRejectedValueOnce(
        new ServiceError(
          {
            message,
            name: "invalid",
            id: "example-error",
            fault: false,
            temporary: false,
            timeout: false,
          },
          {
            response: new Response(null, { status: 422 }),
            request: new Request("https://app.example.test/rpc/discovery"),
            body: "PRIVATE RESPONSE BODY",
          },
        ),
      );
      renderIdentity();
      fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));

      const alert = await screen.findByRole("alert");
      expect(alert.querySelector("code")?.textContent).toBe(message);
      // The severity eyebrow every alert now carries, and then the message
      // itself — the point of the assertion is that nothing else is added,
      // not that the alert is unlabelled.
      expect(alert.textContent).toBe(`Error${message}`);
      expect(within(alert).queryByRole("heading")).toBeNull();
      expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
      expect(screen.queryByText(/PRIVATE RESPONSE BODY/)).toBeNull();
      expect(
        screen.getByLabelText(
          "Could not check automatic registration. Retry OAuth metadata discovery.",
        ),
      ).toBeDefined();
      expect(screen.queryByLabelText("Client ID")).toBeNull();
      expect(
        (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
          .disabled,
      ).toBe(true);
      expect(
        (screen.getByLabelText("Issuer URL") as HTMLInputElement).value,
      ).toBe(discoveryIssuer);
      fireEvent.click(
        within(
          screen.getByRole("group", { name: "Issuer correction" }),
        ).getByRole("button", { name: "Discover" }),
      );
      await waitFor(() =>
        expect(
          screen
            .getByRole("radio", { name: /Auto-Configure/ })
            .getAttribute("aria-checked"),
        ).toBe("true"),
      );
      expect(mocks.fetchMetadata).toHaveBeenCalledTimes(2);
      fireEvent.click(screen.getByRole("button", { name: "Save" }));
      await waitFor(() => expect(mocks.commit).toHaveBeenCalledOnce());
      expect(mocks.commit).toHaveBeenCalledWith({
        commitServerIdentityConfigurationForm: expect.objectContaining({
          clientMode: "auto",
          createProvider: expect.objectContaining(discoveryMetadata),
        }),
      });
      expect(mocks.fetchMetadata).toHaveBeenCalledTimes(2);
    });

    it.each([
      new Error("dial tcp PRIVATE TRANSPORT DIAGNOSTIC"),
      { message: "PRIVATE RESPONSE BODY" },
    ])(
      "uses a generic error for transport or unknown failures",
      async (error) => {
        discoverUnsavedProvider();
        mocks.fetchMetadata.mockRejectedValue(error);
        renderIdentity();
        fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));
        const alert = await screen.findByRole("alert");
        expect(alert.textContent).toContain(
          "Could not discover OAuth metadata. Try again.",
        );
        expect(alert.textContent).not.toContain("PRIVATE");
        expect(screen.queryByLabelText("Client ID")).toBeNull();
        expect(mocks.fetchMetadata).toHaveBeenCalledOnce();
      },
    );

    it.each([
      null,
      {},
      { ...discoveryMetadata, issuer: "https://auth.example.test" },
    ])(
      "rejects malformed or mismatched returned metadata without a hidden Save fetch",
      async (metadata) => {
        discoverUnsavedProvider();
        mocks.fetchMetadata.mockResolvedValue(metadata);
        const { result } = renderIdentityDraft();
        await waitFor(() =>
          expect(result.current.discoveryStatus).toBe("error"),
        );
        expect(result.current.choice).toBeNull();
        act(() => {
          result.current.selectChoice("manual");
          result.current.setClientId("EXAMPLE-MANUAL-CLIENT");
        });
        expect(result.current.canSave).toBe(false);
        await act(async () => expect(await result.current.save()).toBe(false));
        expect(mocks.commit).not.toHaveBeenCalled();
        expect(mocks.fetchMetadata).toHaveBeenCalledOnce();
      },
    );

    it("defaults to Manual only after validated metadata establishes no usable method", async () => {
      discoverUnsavedProvider();
      mocks.fetchMetadata.mockResolvedValue({
        ...discoveryMetadata,
        registrationEndpoint: "http://auth.example.test/register",
        clientIdMetadataDocumentSupported: true,
        tokenEndpointAuthMethodsSupported: ["client_secret_basic"],
      });
      renderIdentity();
      fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));
      await screen.findByLabelText("Client ID");
      expect(
        screen
          .getByRole("radio", {
            name: "Manual",
            description: /client ID and secret/,
          })
          .getAttribute("aria-checked"),
      ).toBe("true");
      expect(
        screen.getByLabelText(/has no usable automatic registration method/),
      ).toBeDefined();
      expect(screen.queryByRole("alert")).toBeNull();
      fireEvent.change(screen.getByLabelText("Client ID"), {
        target: { value: "EXAMPLE-CLIENT" },
      });
      expect(
        (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
          .disabled,
      ).toBe(false);
    });

    it("blocks cached metadata during refetch and failure, retaining explicit Manual input on Discover", async () => {
      discoverUnsavedProvider();
      const { queryClient } = renderIdentity();
      fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));
      await waitFor(() =>
        expect(
          screen
            .getByRole("radio", { name: /Auto-Configure/ })
            .getAttribute("aria-checked"),
        ).toBe("true"),
      );
      fireEvent.click(
        screen.getByRole("radio", {
          name: "Manual",
          description: /client ID and secret/,
        }),
      );
      fireEvent.change(screen.getByLabelText("Client ID"), {
        target: { value: "EXAMPLE-CLIENT" },
      });
      let rejectMetadata!: (error: Error) => void;
      mocks.fetchMetadata.mockImplementationOnce(
        () =>
          new Promise((_resolve, reject) => {
            rejectMetadata = reject;
          }),
      );
      act(() => {
        void queryClient.refetchQueries({
          queryKey: discoveryKey,
          exact: true,
        });
      });
      await screen.findByText("Checking OAuth metadata");
      expect(
        (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
          .disabled,
      ).toBe(true);
      await act(async () => rejectMetadata(new Error("transport failure")));
      await screen.findByRole("alert");
      expect(queryClient.getQueryData(discoveryKey)).toEqual(discoveryMetadata);
      expect(
        (
          screen.getByRole("radio", {
            name: /Auto-Configure/,
          }) as HTMLButtonElement
        ).disabled,
      ).toBe(true);
      expect(
        (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
          .disabled,
      ).toBe(true);
      fireEvent.click(screen.getByRole("button", { name: "Discover" }));
      await waitFor(() =>
        expect(
          (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
            .disabled,
        ).toBe(false),
      );
      expect(
        (screen.getByLabelText("Client ID") as HTMLInputElement).value,
      ).toBe("EXAMPLE-CLIENT");
      expect(
        screen
          .getByRole("radio", {
            name: "Manual",
            description: /client ID and secret/,
          })
          .getAttribute("aria-checked"),
      ).toBe("true");
      expect(mocks.fetchMetadata).toHaveBeenCalledTimes(3);
      fireEvent.click(screen.getByRole("button", { name: "Save" }));
      await waitFor(() => expect(mocks.commit).toHaveBeenCalledOnce());
      expect(
        mocks.commit.mock.calls[0]?.[0].commitServerIdentityConfigurationForm
          .clientMode,
      ).toBe("manual");
    });

    it("refuses direct Save while initial metadata is pending", async () => {
      discoverUnsavedProvider();
      mocks.fetchMetadata.mockImplementation(() => new Promise(() => {}));
      const { result } = renderIdentityDraft();
      expect(result.current.discoveryStatus).toBe("loading");
      await act(async () => expect(await result.current.save()).toBe(false));
      expect(mocks.commit).not.toHaveBeenCalled();
      expect(mocks.fetchMetadata).toHaveBeenCalledOnce();
    });

    it.each(["selection", "refetch", "failure"])(
      "rechecks %s before a queued commit",
      async (change) => {
        discoverUnsavedProvider();
        const { result, queryClient } = renderIdentityDraft();
        await waitFor(() => expect(result.current.canSave).toBe(true));
        let saving!: Promise<boolean>;
        act(() => {
          saving = result.current.save();
          if (change === "selection") {
            result.current.selectProvider(null);
          } else {
            mocks.fetchMetadata.mockImplementationOnce(() =>
              change === "failure"
                ? Promise.reject(new Error("refetch failed"))
                : new Promise(() => {}),
            );
            void queryClient.refetchQueries({
              queryKey: discoveryKey,
              exact: true,
            });
          }
        });
        await act(async () => expect(await saving).toBe(false));
        expect(mocks.commit).not.toHaveBeenCalled();
      },
    );
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
        .getByRole("radio", { name: /Shared credential/ })
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
    fireEvent.click(screen.getByLabelText("Authorization server"));
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
    mocks.impact.mockReturnValue({
      data: {
        servers: [],
        hiddenServerCount: 0,
        detachClientIds: ["client-1"],
      },
      isFetching: false,
      isError: false,
      error: null,
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
    expect(mocks.impact).toHaveBeenLastCalledWith(
      expect.objectContaining({
        userSessionIssuerId: "user-session-issuer-1",
        change: "detach",
      }),
      expect.objectContaining({ enabled: true }),
    );

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
    expect(mocks.detachAssociation).not.toHaveBeenCalled();
  });

  it("only stops this server using a client other servers share when leaving User Identity", async () => {
    mocks.clients.mockReturnValue({
      items: [
        {
          id: "client-1",
          clientId: "dashboard-client",
          scope: [],
          userSessionIssuerIds: ["user-session-issuer-1"],
          remoteSessionIssuerId: "provider-1",
          projectId: "project-1",
          issuer: {
            id: "provider-1",
            issuer: "https://idp.example.com",
            slug: "idp",
          },
        },
        {
          id: "client-of-another-server",
          clientId: "other-client",
          scope: [],
          userSessionIssuerIds: ["user-session-issuer-1"],
          remoteSessionIssuerId: "provider-2",
          projectId: "project-1",
          issuer: {
            id: "provider-2",
            issuer: "https://other-idp.example.com",
            slug: "other-idp",
          },
        },
      ],
      isLoading: false,
      isError: false,
      error: null,
    });
    mocks.associations.mockReturnValue({
      data: {
        associations: [{ remoteSessionClientId: "client-1", serverCount: 2 }],
        requiredClientIds: ["client-1"],
      },
    });
    mocks.impact.mockReturnValue({
      data: {
        servers: [],
        hiddenServerCount: 0,
        releaseClientIds: ["client-1"],
      },
      isFetching: false,
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
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    await waitFor(() =>
      expect(mocks.detachAssociation).toHaveBeenCalledWith({
        request: {
          detachServerClientAssociationRequestBody: {
            mcpServerId: "mcp-server-1",
            remoteSessionClientId: "client-1",
          },
        },
      }),
    );
    expect(mocks.detach).not.toHaveBeenCalled();
  });

  it("shows and removes a client the server needs without an association", async () => {
    mocks.clients.mockReturnValue({
      items: [
        {
          id: "unclaimed-client",
          clientId: "unclaimed",
          scope: [],
          userSessionIssuerIds: ["user-session-issuer-1"],
          remoteSessionIssuerId: "provider-1",
          projectId: "project-1",
        },
      ],
      isLoading: false,
      isError: false,
      error: null,
    });
    mocks.associations.mockReturnValue({
      data: { associations: [], requiredClientIds: ["unclaimed-client"] },
    });
    mocks.impact.mockReturnValue({
      data: {
        servers: [],
        hiddenServerCount: 0,
        releaseClientIds: ["unclaimed-client"],
      },
      isFetching: false,
      isError: false,
      error: null,
    });

    renderIdentity();
    const manual = screen.getByRole("radio", {
      name: "Manual",
      description: /static headers/,
    });
    expect(manual.getAttribute("aria-checked")).toBe("false");

    fireEvent.click(manual);
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    await waitFor(() =>
      expect(mocks.detachAssociation).toHaveBeenCalledWith({
        request: {
          detachServerClientAssociationRequestBody: {
            mcpServerId: "mcp-server-1",
            remoteSessionClientId: "unclaimed-client",
          },
        },
      }),
    );
    expect(mocks.detach).not.toHaveBeenCalled();
  });

  it("does not report success when turning off User Identity would remove nothing", async () => {
    mocks.clients.mockReturnValue({
      items: [
        {
          id: "client-1",
          clientId: "dashboard-client",
          scope: [],
          userSessionIssuerIds: ["user-session-issuer-1"],
          remoteSessionIssuerId: "provider-1",
          projectId: "project-1",
        },
      ],
      isLoading: false,
      isError: false,
      error: null,
    });
    mocks.impact.mockReturnValue({
      data: { servers: [], hiddenServerCount: 0 },
      isFetching: false,
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
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    await waitFor(() =>
      expect(mocks.toastError).toHaveBeenCalledWith(
        expect.stringContaining("nothing was removed"),
      ),
    );
    expect(mocks.detach).not.toHaveBeenCalled();
    expect(mocks.detachAssociation).not.toHaveBeenCalled();
  });

  it("does not trust the previous preview once a refresh of it fails", async () => {
    mocks.clients.mockReturnValue({
      items: [
        {
          id: "client-1",
          clientId: "dashboard-client",
          scope: [],
          userSessionIssuerIds: ["user-session-issuer-1"],
          remoteSessionIssuerId: "provider-1",
          projectId: "project-1",
        },
      ],
      isLoading: false,
      isError: false,
      error: null,
    });
    mocks.impact.mockReturnValue({
      data: {
        servers: [],
        hiddenServerCount: 0,
        detachClientIds: ["client-1"],
      },
      isFetching: false,
      isError: true,
      error: new Error("preview failed"),
    });

    renderIdentity();
    fireEvent.click(
      screen.getByRole("radio", {
        name: "Manual",
        description: /static headers/,
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    await waitFor(() =>
      expect(mocks.toastError).toHaveBeenCalledWith(
        expect.stringContaining("Couldn't check which clients"),
      ),
    );
    expect(mocks.detach).not.toHaveBeenCalled();
  });

  it("stays unresolved when it cannot tell which clients are this server's", () => {
    mocks.associations.mockReturnValue({
      data: undefined,
      isError: true,
      error: new Error("associations failed"),
    });

    renderIdentity();

    expect(screen.getByText("Identity is unavailable.")).toBeDefined();
    expect(screen.queryByRole("radio", { name: /User Identity/ })).toBeNull();
  });

  it("does not treat another server's client on the issuer as this server's identity", () => {
    mocks.clients.mockReturnValue({
      items: [
        {
          id: "client-of-another-server",
          clientId: "other-client",
          scope: [],
          userSessionIssuerIds: ["user-session-issuer-1"],
          remoteSessionIssuerId: "provider-2",
          projectId: "project-1",
        },
      ],
      isLoading: false,
      isError: false,
      error: null,
    });
    mocks.associations.mockReturnValue({
      data: { associations: [], requiredClientIds: [] },
    });

    renderIdentity();

    expect(
      screen.queryByRole("button", { name: "Clear connection" }),
    ).toBeNull();
    expect(
      screen
        .getByRole("radio", { name: "Manual", description: /static headers/ })
        .getAttribute("aria-checked"),
    ).toBe("true");
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
    ).toBe(
      "/org/projects/project/remote-identity-providers/provider-1/clients/client-1",
    );
    // A raw client ID tells an operator nothing.
    expect(screen.queryByText("dashboard-client")).toBeNull();
  });

  it("names the other servers a shared client signs in", () => {
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
              clientIdMetadataDocumentSupported: true,
              authorizationEndpoint: "https://id.example/authorize",
              tokenEndpoint: "https://id.example/token",
            },
          ],
        },
      },
    });
    mocks.associations.mockReturnValue({
      data: {
        associations: [{ remoteSessionClientId: "client-1", serverCount: 3 }],
        requiredClientIds: ["client-1"],
      },
    });

    renderIdentity();

    expect(
      screen.getByText(/This client also signs in 2 other MCP servers/),
    ).toBeDefined();
  });

  it("replaces a cleared client only after confirming the sign-out", async () => {
    mocks.clients.mockReturnValue({
      items: [
        {
          id: "client-1",
          clientId: "dashboard-client",
          credentialOwner: "subject",
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

    // Every choice makes a client of the server's own. A stored client is only
    // ever a starting point, and none is picked on the operator's behalf.
    expect(screen.queryByRole("radio", { name: /Existing client/ })).toBeNull();
    expect(
      screen
        .getByRole("radio", { name: /New from existing/ })
        .getAttribute("aria-checked"),
    ).toBe("false");

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
          replaceClient: true,
          replaceClientServerIds: [],
        }),
      }),
    );
  });

  it("names servers sharing the issuer when a client swap keeps the provider", async () => {
    swapClientWithImpact([
      {
        id: "mcp-server-2",
        name: "Other server",
        projectId: "project-1",
        projectName: "Project",
        impact: "resignin",
      },
    ]);

    expect(screen.getByText("Replace the connected client?")).toBeDefined();
    expect(screen.getByText("Other server")).toBeDefined();
    expect(screen.getByText(/have to sign in again:/)).toBeDefined();
    expect(mocks.commit).not.toHaveBeenCalled();
    expect(mocks.impact).toHaveBeenLastCalledWith(
      expect.objectContaining({
        userSessionIssuerId: "user-session-issuer-1",
        change: "replace",
        providerId: "provider-1",
      }),
      expect.objectContaining({ enabled: true }),
    );
  });

  it("names servers in other projects and blocks when some cannot be seen", async () => {
    swapClientWithImpact(
      [
        {
          id: "mcp-server-3",
          name: "Elsewhere",
          projectId: "project-2",
          projectName: "Other project",
          impact: "repoint",
        },
      ],
      2,
    );

    expect(screen.getByText("Elsewhere (Other project)")).toBeDefined();
    expect(
      screen.getByText(
        /2 more servers you don't have access to share this user session issuer and may be affected/,
      ),
    ).toBeDefined();
    expect(screen.queryByText(/other\s+projects may share it/)).toBeNull();
    const confirm = screen.getByRole("button", { name: "Save changes" });
    expect((confirm as HTMLButtonElement).disabled).toBe(true);
  });

  it("blocks the save when the server refuses the change", async () => {
    mocks.impact.mockReturnValue({
      data: undefined,
      isLoading: false,
      isError: true,
      error: Object.assign(
        new Error(
          "change it from the organization's authorization server settings",
        ),
        { statusCode: 409 },
      ),
    });
    swapClientWithImpact(null);

    expect(
      screen.getByText(
        /change it from the organization's authorization server/,
      ),
    ).toBeDefined();
    expect(
      screen.queryByText(/Could not check which other servers share/),
    ).toBeNull();
    const confirm = screen.getByRole("button", { name: "Save changes" });
    expect((confirm as HTMLButtonElement).disabled).toBe(true);
  });

  it("holds the confirm while the clients are refreshed", () => {
    const rerenderIdentity = swapClientWithImpact([
      {
        id: "mcp-server-2",
        name: "Other server",
        projectId: "project-1",
        projectName: "Project",
        impact: "resignin",
      },
    ]);
    const confirm = () =>
      screen.getByRole("button", { name: "Save changes" }) as HTMLButtonElement;
    expect(confirm().disabled).toBe(false);
    mocks.clients.mockReturnValue({
      ...mocks.clients(),
      isLoading: true,
      isRefreshing: true,
    });
    rerenderIdentity();
    expect(confirm().disabled).toBe(true);
  });

  it("holds the confirm while a cached preview is refetched", async () => {
    mocks.impact.mockReturnValue({
      data: { servers: [], hiddenServerCount: 0 },
      isLoading: false,
      isFetching: true,
      isError: false,
    });
    swapClientWithImpact(null);

    expect(mocks.impact).toHaveBeenLastCalledWith(
      expect.anything(),
      expect.objectContaining({ refetchOnMount: "always", staleTime: 0 }),
    );
    expect(
      screen.getByText("Checking other servers on this user session issuer…"),
    ).toBeDefined();
    const save = screen.getByRole("button", { name: "Save" });
    expect((save as HTMLButtonElement).disabled).toBe(true);
    expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull();
  });

  it("warns when the affected servers cannot be checked", async () => {
    mocks.impact.mockReturnValue({
      data: undefined,
      isLoading: false,
      isError: true,
    });
    swapClientWithImpact(null);

    expect(
      screen.getByText(/Could not check which other servers share/),
    ).toBeDefined();
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

  it("saves header edits once a cleared client is put back by cancelling", async () => {
    connectClient();

    renderIdentity();
    fireEvent.click(screen.getByRole("button", { name: "Clear connection" }));
    addCustomHeader("X-Team", "eng");
    // Clearing asks for a new client, and no choice puts the old one back.
    expect(
      screen.getByText(
        "Finish the User Identity change, or cancel it, to save.",
      ),
    ).toBeDefined();

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
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
    expect(
      screen.queryByRole("radio", { name: /New from existing/ }),
    ).toBeNull();
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
    fireEvent.click(screen.getByRole("radio", { name: /Shared credential/ }));

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
    fireEvent.click(screen.getByRole("radio", { name: /Shared credential/ }));
    fireEvent.click(staticFormat("Basic"));

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

    fireEvent.click(staticFormat("Manual"));
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
    fireEvent.click(screen.getByRole("radio", { name: /Shared credential/ }));
    fireEvent.click(staticFormat("Basic"));

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

  it("keeps Signing Client visible but unselectable without KMS", () => {
    renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: /Shared credential/ }));

    expect(screen.getByText("Requires KMS")).toBeDefined();
    const signingKey = within(
      screen.getByRole("radiogroup", { name: "Service account" }),
    ).getByRole("radio", { name: /^Signing Client/ }) as HTMLButtonElement;
    expect(signingKey.disabled).toBe(true);
    // Still on Bearer: the option advertises KMS signing, it does not switch.
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
          name: /Shared credential/,
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  });

  it("leaves an organization client on an organization-wide issuer editable for an organization admin", () => {
    mocks.hasScope.mockImplementation(
      (scope: string, resourceId?: string) =>
        scope === "org:read" ||
        resourceId === "mcp-server-1" ||
        (scope === "org:admin" && resourceId === "org-1"),
    );
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

    expect(screen.queryByText(/shared by every project/i)).toBeNull();
    expect(
      (
        screen.getByRole("radio", {
          name: /Shared credential/,
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(false);
  });

  it("locks identity only when an organization-wide issuer holds an organization client", () => {
    mocks.hasScope.mockImplementation(
      (scope: string, resourceId?: string) =>
        scope === "org:read" || resourceId === "mcp-server-1",
    );
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
    ).toBe(
      "/org/projects/project/remote-identity-providers/provider-1/clients/client-1/mcp-servers",
    );
    expect(
      (
        screen.getByRole("radio", {
          name: /Shared credential/,
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
          name: /Shared credential/,
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

    expect(screen.getByText("Identity is unavailable.")).toBeDefined();
    expect(
      screen.queryByRole("radio", { name: /Shared credential/ }),
    ).toBeNull();
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

    expect(screen.getByText("Identity is unavailable.")).toBeDefined();
    expect(
      screen.queryByRole("radio", { name: /Shared credential/ }),
    ).toBeNull();
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
    expect(dialog.textContent).toContain("unlinks the authorization server");
    expect(dialog.textContent).toContain(
      "removes the static Authorization credential",
    );
  });

  it("asks to remove the service account client when No Identity is saved", async () => {
    boundSelfClient();

    renderIdentity();
    fireEvent.click(
      screen.getByRole("radio", {
        name: "Manual",
        description: /static headers/,
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    const dialog = screen.getByRole("dialog");
    expect(
      within(dialog).getByText("Remove the service account client?"),
    ).toBeDefined();
    expect(dialog.textContent).toContain(
      "requests will be rejected until you save a new identity",
    );
    expect(dialog.textContent).not.toContain("new credential");

    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() =>
      expect(mocks.detach).toHaveBeenCalledWith({
        request: {
          attachUserSessionIssuerForm: {
            id: "self-client-1",
            userSessionIssuerId: "user-session-issuer-1",
          },
        },
      }),
    );
  });

  it("confirms replacing the service account client with User Identity", async () => {
    boundSelfClient();
    autoConfigurableProvider();

    renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    const dialog = screen.getByRole("dialog");
    expect(
      within(dialog).getByText("Replace the service account client?"),
    ).toBeDefined();
    expect(dialog.textContent).toContain(
      "stops this server using its current service account client",
    );
    expect(dialog.textContent).not.toContain("sign in again");

    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() => expect(mocks.commit).toHaveBeenCalledOnce());
    // The commit replaces the bound client, so it is not detached first.
    expect(mocks.detach).not.toHaveBeenCalled();
    expect(mocks.remove).not.toHaveBeenCalled();
  });

  it("removes a leftover credential after binding User Identity over the service account client", async () => {
    mocks.headers.mockReturnValue({
      data: { headers: [configuredHeader()] },
      isLoading: false,
      isError: false,
      error: null,
      refetch: mocks.refetchHeaders,
    });
    boundSelfClient();
    autoConfigurableProvider();

    renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    const dialog = screen.getByRole("dialog");
    expect(
      within(dialog).getByText("Replace the service account client?"),
    ).toBeDefined();
    expect(dialog.textContent).toContain(
      "removes the static Authorization credential",
    );

    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() =>
      expect(mocks.remove).toHaveBeenCalledWith({
        request: { id: "header-1" },
      }),
    );
    expect(mocks.commit.mock.invocationCallOrder[0]).toBeLessThan(
      mocks.remove.mock.invocationCallOrder[0]!,
    );
  });

  it("holds Save when User Identity is left for an unfinished OAuth client", async () => {
    connectClient();

    renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: /Shared credential/ }));
    fireEvent.click(
      within(
        screen.getByRole("radiogroup", { name: "Service account" }),
      ).getByRole("radio", { name: /^Client Secret/ }),
    );
    addCustomHeader("X-Team", "platform");

    const save = screen.getByRole("button", { name: "Save" });
    expect((save as HTMLButtonElement).disabled).toBe(true);
    expect(
      screen.getByText(
        "Finish the service account client, or cancel, to save.",
      ),
    ).toBeDefined();
    fireEvent.click(save);
    await Promise.resolve();
    expect(mocks.create).not.toHaveBeenCalled();
    expect(mocks.commit).not.toHaveBeenCalled();
    expect(mocks.detach).not.toHaveBeenCalled();
  });

  it("holds Save when a static credential replacing the service account client is blank", () => {
    boundSelfClient();

    renderIdentity();
    // The saved client hides the format choice until it is cleared.
    fireEvent.click(screen.getByRole("button", { name: "Clear" }));
    fireEvent.click(staticFormat("Bearer"));
    addCustomHeader("X-Team", "platform");

    expect(
      (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    expect(
      screen.getByText("Enter the credential, or cancel, to save."),
    ).toBeDefined();

    fireEvent.change(screen.getByLabelText("Token"), {
      target: { value: "bearer-secret-value" },
    });
    expect(
      (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
        .disabled,
    ).toBe(false);
  });

  function boundGitHubApp() {
    mocks.githubClients.mockReturnValue([
      {
        id: "github-client",
        githubApp: {
          instanceUrl: "https://github.com",
          appId: 123,
          installationId: 456,
          upstreamOrigin: "https://mcp.example.com",
        },
      },
    ]);
  }

  it("lets an mcp:write collaborator save custom headers without changing the bound GitHub App", async () => {
    boundGitHubApp();
    mocks.hasScope.mockImplementation(
      (scope: string, resourceId?: string) =>
        scope === "org:read" ||
        ((scope === "mcp:write" || scope === "mcp:read") &&
          resourceId === "mcp-server-1"),
    );
    mocks.headers.mockReturnValue({
      data: { headers: [configuredHeader()] },
      isLoading: false,
      isError: false,
      error: null,
      refetch: mocks.refetchHeaders,
    });
    renderIdentity();
    addCustomHeader("X-Team", "platform");
    expect(
      (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
        .disabled,
    ).toBe(false);
    confirmSave();
    await waitFor(() => expect(mocks.create).toHaveBeenCalled());
    expect(mocks.remove).not.toHaveBeenCalled();
    expect(mocks.detachGitHub).not.toHaveBeenCalled();
    expect(mocks.commit).not.toHaveBeenCalled();
  });

  it("still requires admin access to save an actual GitHub App draft", () => {
    boundGitHubApp();
    mocks.hasScope.mockReturnValue(true);
    const { rerenderIdentity } = renderIdentity();
    fireEvent.change(screen.getByLabelText("Private key (PEM)"), {
      target: { value: "-----BEGIN PRIVATE KEY-----\nreplacement-key" },
    });
    expect(
      (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
        .disabled,
    ).toBe(false);
    mocks.hasScope.mockImplementation((scope: string) => scope !== "org:admin");
    rerenderIdentity();
    addCustomHeader("X-Team", "platform");
    expect(
      (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    confirmSave();
    expect(mocks.create).not.toHaveBeenCalled();
    expect(mocks.remove).not.toHaveBeenCalled();
    expect(mocks.detachGitHub).not.toHaveBeenCalled();
  });

  it("blocks an App replacement before writing when admin access is revoked after confirmation", async () => {
    boundGitHubApp();
    const { rerenderIdentity } = renderIdentity();
    fireEvent.click(staticFormat("Bearer"));
    fireEvent.change(screen.getByLabelText("Token"), {
      target: { value: "replacement-token" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    mocks.hasScope.mockImplementation((scope: string) => scope !== "org:admin");
    rerenderIdentity();
    fireEvent.click(
      within(screen.getByRole("dialog")).getByRole("button", {
        name: "Save changes",
      }),
    );
    await waitFor(() => expect(mocks.create).not.toHaveBeenCalled());
    expect(mocks.commit).not.toHaveBeenCalled();
    expect(mocks.remove).not.toHaveBeenCalled();
    expect(mocks.detachGitHub).not.toHaveBeenCalled();
  });

  it("keeps a hidden App fail-closed without org:read while allowing ordinary header saves", async () => {
    boundGitHubApp();
    mocks.hasScope.mockImplementation(
      (scope: string, resourceId?: string) =>
        scope === "mcp:write" && resourceId === "mcp-server-1",
    );
    renderIdentity();
    expect(screen.getByText("Identity is unavailable.")).toBeDefined();
    expect(screen.queryByRole("radio", { name: /Service Account/ })).toBeNull();
    addCustomHeader("X-Team", "platform");
    confirmSave();
    await waitFor(() => expect(mocks.create).toHaveBeenCalled());
    expect(mocks.commit).not.toHaveBeenCalled();
    expect(mocks.remove).not.toHaveBeenCalled();
    expect(mocks.detachGitHub).not.toHaveBeenCalled();
  });

  it.each([
    "https://api.githubcopilot.com/mcp",
    "https://proxy.example.com/mcp",
  ])(
    "does not replace a hidden App on %s through an Authorization header",
    (url) => {
      mocks.source.mockReturnValue({
        data: { slug: "fixture", url },
        isLoading: false,
        isError: false,
      });
      boundGitHubApp();
      mocks.hasScope.mockImplementation(
        (scope: string, resourceId?: string) =>
          scope === "mcp:write" && resourceId === "mcp-server-1",
      );
      renderIdentity();
      addCustomHeader("Authorization", "Bearer replacement-token");
      expect(
        (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
          .disabled,
      ).toBe(true);
      expect(mocks.create).not.toHaveBeenCalled();
      expect(mocks.detachGitHub).not.toHaveBeenCalled();
    },
  );

  it("describes replacing the GitHub App with a Bearer credential", () => {
    boundGitHubApp();
    renderIdentity();
    fireEvent.click(staticFormat("Bearer"));
    fireEvent.change(screen.getByLabelText("Token"), {
      target: { value: "replacement-token" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(
      screen.getByRole("heading", { name: "Replace the GitHub App?" }),
    ).toBeDefined();
    expect(
      screen.getByText(/Requests will use the replacement identity instead/),
    ).toBeDefined();
    expect(mocks.detachGitHub).not.toHaveBeenCalled();
  });

  it("describes removing the GitHub App without a replacement identity", () => {
    boundGitHubApp();
    renderIdentity();
    fireEvent.click(
      screen.getByRole("radio", {
        name: "Manual",
        description: /static headers/,
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(
      screen.getByRole("heading", { name: "Remove the GitHub App?" }),
    ).toBeDefined();
    expect(
      screen.getByText(/no replacement identity will be configured/),
    ).toBeDefined();
  });

  it("keeps a bound GitHub App when an unfinished OAuth choice only saves headers", async () => {
    mocks.githubClients.mockReturnValue([
      {
        id: "github-client",
        githubApp: {
          instanceUrl: "https://github.com",
          appId: 123,
          installationId: 456,
          upstreamOrigin: "https://mcp.example.com",
        },
      },
    ]);
    renderIdentity();
    expect(screen.getByRole("radio", { name: /^GitHub App/ })).toBeDefined();
    fireEvent.click(
      within(
        screen.getByRole("radiogroup", { name: "Service account" }),
      ).getByRole("radio", { name: /^Client Secret/ }),
    );
    addCustomHeader("X-Team", "platform");
    confirmSave();
    await waitFor(() => expect(mocks.create).toHaveBeenCalled());
    expect(mocks.detachGitHub).not.toHaveBeenCalled();
  });

  it("keeps a static credential an unfinished OAuth client would not replace", async () => {
    mocks.headers.mockReturnValue({
      data: { headers: [configuredHeader()] },
      isLoading: false,
      isError: false,
      error: null,
      refetch: mocks.refetchHeaders,
    });

    renderIdentity();
    fireEvent.click(
      within(
        screen.getByRole("radiogroup", { name: "Service account" }),
      ).getByRole("radio", { name: /^Client Secret/ }),
    );
    addCustomHeader("X-Team", "platform");
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(screen.queryByRole("dialog")).toBeNull();
    await waitFor(() => expect(mocks.create).toHaveBeenCalled());
    expect(mocks.remove).not.toHaveBeenCalled();
  });

  it("confirms the service account client replacing a static credential", async () => {
    mocks.headers.mockReturnValue({
      data: { headers: [configuredHeader()] },
      isLoading: false,
      isError: false,
      error: null,
      refetch: mocks.refetchHeaders,
    });
    boundSelfClient();

    renderIdentity();
    addCustomHeader("X-Team", "platform");
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    const dialog = screen.getByRole("dialog");
    expect(
      within(dialog).getByText("Replace the shared credential?"),
    ).toBeDefined();
    expect(dialog.textContent).toContain(
      "replaces the static Authorization credential with the service account client",
    );
    expect(dialog.textContent).not.toContain("no longer authenticate");

    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() =>
      expect(mocks.remove).toHaveBeenCalledWith({
        request: { id: "header-1" },
      }),
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

  it("links shared-source guidance to authorization server management", () => {
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
        .getByRole("link", { name: "Authorization Servers" })
        .getAttribute("href"),
    ).toBe("/org/projects/project/remote-identity-providers");
  });
});

describe("identity chaining", () => {
  it("leaves identity chaining to its own section, even for a sign-in that trusts an identity provider", () => {
    mocks.userSessionIssuer.mockReturnValue({
      data: {
        id: "user-session-issuer-1",
        projectId: "",
        trustedRemoteSessionIssuerId: "okta-issuer",
        trustedRemoteSessionClientId: "okta-client",
      },
    });
    renderIdentity();
    expect(screen.queryByText(/Enterprise Managed Authorization/)).toBeNull();
    expect(screen.queryByText(/Identity chaining/i)).toBeNull();
    expect(screen.queryByRole("button", { name: "Turn on" })).toBeNull();
  });
});
