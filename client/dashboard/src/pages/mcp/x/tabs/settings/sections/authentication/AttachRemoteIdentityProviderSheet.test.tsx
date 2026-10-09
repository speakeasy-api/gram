import { TooltipProvider } from "@/components/ui/Tooltip";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AttachRemoteIdentityProviderSheet } from "./AttachRemoteIdentityProviderSheet";
import type { AuthTarget } from "./authTarget";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";

const mocks = vi.hoisted(() => ({
  attachClient: vi.fn(),
  createClient: vi.fn(),
  createCimdClient: vi.fn(),
  createProvider: vi.fn(),
  createUserSessionIssuer: vi.fn(),
  invalidateTarget: vi.fn(),
  linkTarget: vi.fn(),
  onOpenChange: vi.fn(),
  clearDiscoverError: vi.fn(),
  handleResetEndpoints: vi.fn(),
  resetEndpointState: vi.fn(),
  runDiscover: vi.fn(),
  setAuthorizationEndpoint: vi.fn(),
  setIssuerUrl: vi.fn(),
  setJwksUri: vi.fn(),
  setRegistrationEndpoint: vi.fn(),
  setTokenEndpoint: vi.fn(),
  issuersPage: vi.fn(),
  issuersSearch: vi.fn(),
  issuerById: vi.fn(),
  discovery: { snapshot: null as Record<string, unknown> | null },
  impact: vi.fn(),
}));

vi.mock("@gram/client/react-query/remoteSessionIssuers.js", () => ({
  invalidateAllRemoteSessionIssuers: vi.fn(),
  useRemoteSessionIssuers: () => ({
    data: { result: { items: mocks.issuersPage() } },
  }),
  useRemoteSessionIssuersInfinite: (request: { search?: string }) => {
    mocks.issuersSearch(request.search);
    return {
      data: { pages: [{ result: { items: mocks.issuersPage() } }] },
      isFetching: false,
      isError: false,
      hasNextPage: false,
      isFetchingNextPage: false,
      fetchNextPage: vi.fn(),
    };
  },
}));

vi.mock(
  "@gram/client/react-query/newRemoteSessionClientCallbackUrl.js",
  () => ({
    useNewRemoteSessionClientCallbackUrl: () => ({
      data: {
        callbackUrl: "https://new.example.com/mcp/remote_login_callback",
      },
    }),
  }),
);

vi.mock("@gram/client/react-query/serverIdentityImpact.js", () => ({
  invalidateAllServerIdentityImpact: () => Promise.resolve(),
  useServerIdentityImpact: (
    request: unknown,
    _security: unknown,
    options: { enabled?: boolean },
  ) => ({
    data: options.enabled ? mocks.impact(request) : undefined,
    isError: false,
    isLoading: false,
  }),
}));

vi.mock("@gram/client/react-query/remoteSessionIssuer.js", () => ({
  useRemoteSessionIssuer: (
    request: { id: string },
    _security: unknown,
    options: { enabled: boolean },
  ) => ({
    data: options.enabled ? mocks.issuerById(request.id) : undefined,
  }),
}));

vi.mock("@/components/asset-image-upload-field", () => ({
  AssetImageUploadField: () => null,
}));

vi.mock("@/contexts/Fetcher", () => ({
  useFetcher: () => ({ fetch: vi.fn() }),
}));

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    remoteSessionClients: {
      attachUserSessionIssuer: mocks.attachClient,
      create: mocks.createClient,
      createCimd: mocks.createCimdClient,
    },
    remoteSessionIssuers: { create: mocks.createProvider },
    userSessionIssuers: { create: mocks.createUserSessionIssuer },
  }),
  // main routes requests through an explicit project slug; the sheet does not
  // care which project, only that the value resolves.
  useProjectSlugForRequests: () => "default",
}));

vi.mock("@/lib/remote-identity/queries/useAllRemoteSessionClients", () => ({
  useAllRemoteSessionClients: () => ({ items: [], isLoading: false }),
}));

vi.mock("./useIssuerDuplicatePreflight", () => ({
  useIssuerDuplicatePreflight: () => ({ matches: [] }),
}));

vi.mock("./useIssuerDiscovery", () => ({
  useIssuerDiscovery: () => ({
    issuerUrl: "https://id.example.test",
    setIssuerUrl: mocks.setIssuerUrl,
    authorizationEndpoint: "https://id.example.test/authorize",
    setAuthorizationEndpoint: mocks.setAuthorizationEndpoint,
    tokenEndpoint: "https://id.example.test/token",
    setTokenEndpoint: mocks.setTokenEndpoint,
    registrationEndpoint: "",
    setRegistrationEndpoint: mocks.setRegistrationEndpoint,
    jwksUri: "https://id.example.test/jwks",
    setJwksUri: mocks.setJwksUri,
    discoveredSnapshot: mocks.discovery.snapshot,
    discoverPending: false,
    discoverError: null,
    clearDiscoverError: mocks.clearDiscoverError,
    runDiscover: mocks.runDiscover,
    handleResetEndpoints: mocks.handleResetEndpoints,
    resetEndpointState: mocks.resetEndpointState,
    showDiscoverControls: false,
    showResetControls: false,
    endpointWarnings: [],
  }),
}));

function target(overrides: Partial<AuthTarget> = {}): AuthTarget {
  return {
    kind: "standard",
    slug: "example-server",
    projectId: "project-1",
    permissionResourceId: "mcp-server-1",
    supportsOrganizationIssuers: true,
    userSessionIssuerId: null,
    invalidate: mocks.invalidateTarget,
    linkUserSessionIssuer: mocks.linkTarget,
    ...overrides,
  };
}

function renderSheet(
  excludedIssuerIds?: string[],
  userSessionIssuer: UserSessionIssuer | null = null,
  targetOverrides: Partial<AuthTarget> = {},
): void {
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <AttachRemoteIdentityProviderSheet
          open
          onOpenChange={(open) => {
            mocks.onOpenChange(open);
          }}
          target={target(targetOverrides)}
          userSessionIssuer={userSessionIssuer}
          excludedIssuerIds={excludedIssuerIds}
        />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

async function submitManualClient(): Promise<void> {
  fireEvent.change(screen.getByPlaceholderText("client_abc123"), {
    target: { value: "client-1" },
  });
  fireEvent.click(
    screen.getByRole("button", { name: "Attach Identity Provider" }),
  );
}

beforeEach(() => {
  mocks.issuersPage.mockReturnValue([]);
  mocks.impact.mockReturnValue({ servers: [], hiddenServerCount: 0 });
  mocks.createUserSessionIssuer.mockResolvedValue({
    id: "user-session-issuer-1",
  });
  mocks.createProvider.mockResolvedValue({ id: "provider-1" });
  mocks.createClient.mockResolvedValue({ id: "client-1" });
  mocks.invalidateTarget.mockResolvedValue(undefined);
  mocks.linkTarget.mockResolvedValue(undefined);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  mocks.discovery.snapshot = null;
});

describe("AttachRemoteIdentityProviderSheet", () => {
  it("keeps standard targets on the existing multi-call setup flow", async () => {
    renderSheet();

    await submitManualClient();

    await waitFor(() => {
      expect(mocks.createUserSessionIssuer).toHaveBeenCalled();
      expect(mocks.createProvider).toHaveBeenCalled();
      expect(mocks.createClient).toHaveBeenCalled();
      expect(mocks.linkTarget).toHaveBeenCalledWith("user-session-issuer-1");
    });
  });

  it("forwards discovered grant profiles on a new provider", async () => {
    mocks.discovery.snapshot = {
      url: "https://id.example.test",
      scopesSupported: [],
      grantTypesSupported: [],
      authorizationGrantProfilesSupported: [
        "urn:ietf:params:oauth:grant-profile:id-jag",
      ],
      responseTypesSupported: [],
      tokenEndpointAuthMethodsSupported: [],
    };
    renderSheet();

    await submitManualClient();

    await waitFor(() =>
      expect(mocks.createProvider).toHaveBeenCalledWith({
        createRemoteSessionIssuerForm: expect.objectContaining({
          authorizationGrantProfilesSupported: [
            "urn:ietf:params:oauth:grant-profile:id-jag",
          ],
        }),
      }),
    );
  });

  it("omits grant profiles on a hand-typed provider", async () => {
    renderSheet();

    await submitManualClient();

    await waitFor(() => expect(mocks.createProvider).toHaveBeenCalled());
    const [request] = mocks.createProvider.mock.calls[0]! as [
      { createRemoteSessionIssuerForm: Record<string, unknown> },
    ];
    expect(
      request.createRemoteSessionIssuerForm.authorizationGrantProfilesSupported,
    ).toBeUndefined();
  });

  it("confirms before re-pointing servers that share the user session issuer", async () => {
    mocks.impact.mockReturnValue({
      servers: [
        {
          id: "mcp-server-2",
          name: "Sibling",
          projectId: "project-1",
          projectName: "Project",
          impact: "clear",
        },
      ],
      hiddenServerCount: 0,
    });
    renderSheet(["provider-old"], { id: "usi-1" } as UserSessionIssuer, {
      mcpServerId: "mcp-server-1",
    });

    await submitManualClient();

    expect(
      await screen.findByText("Change the upstream of other servers?"),
    ).toBeTruthy();
    expect(screen.getByText("Sibling")).toBeTruthy();
    expect(mocks.createProvider).not.toHaveBeenCalled();
    expect(mocks.impact).toHaveBeenLastCalledWith({
      userSessionIssuerId: "usi-1",
      mcpServerId: "mcp-server-1",
      change: "attach",
      providerId: undefined,
      clientId: undefined,
    });

    const confirm = screen.getAllByRole("button", {
      name: "Attach Identity Provider",
    });
    fireEvent.click(confirm[confirm.length - 1] as HTMLElement);
    await waitFor(() => expect(mocks.createProvider).toHaveBeenCalled());
  });

  it("attaches without confirming when no other server is affected", async () => {
    renderSheet(["provider-old"], { id: "usi-1" } as UserSessionIssuer, {
      permissionResourceId: "toolset-1",
      mcpServerId: "mcp-server-1",
    });

    await submitManualClient();

    await waitFor(() => expect(mocks.createProvider).toHaveBeenCalled());
    expect(
      screen.queryByText("Change the upstream of other servers?"),
    ).toBeNull();
  });

  it("names affected servers in other projects and blocks on hidden ones", async () => {
    mocks.impact.mockReturnValue({
      servers: [
        {
          id: "mcp-server-9",
          name: "Elsewhere",
          projectId: "project-2",
          projectName: "Other project",
          impact: "repoint",
        },
      ],
      hiddenServerCount: 1,
    });
    renderSheet([], { id: "usi-1", projectId: "" } as UserSessionIssuer);

    await submitManualClient();

    expect(await screen.findByText("Elsewhere (Other project)")).toBeTruthy();
    expect(
      screen.getByText(
        /1 more server you don't have access to shares this user session issuer and may be affected/,
      ),
    ).toBeTruthy();
    const confirm = screen.getAllByRole("button", {
      name: "Attach Identity Provider",
    });
    expect((confirm[confirm.length - 1] as HTMLButtonElement).disabled).toBe(
      true,
    );
    expect(mocks.createProvider).not.toHaveBeenCalled();
  });

  it("searches the listing on the server and resolves the pick by id", async () => {
    const attached = {
      id: "provider-attached",
      slug: "attached",
      name: "Attached",
      issuer: "https://attached.example.test",
    };
    const catalog = {
      id: "provider-catalog",
      slug: "catalog",
      name: "Catalog provider",
      issuer: "https://catalog.example.test",
    };
    mocks.issuersPage.mockReturnValue([attached, catalog]);
    mocks.issuerById.mockImplementation((id: string) =>
      id === catalog.id ? catalog : undefined,
    );
    renderSheet([attached.id]);

    fireEvent.click(screen.getByText("Choose an identity provider…"));
    fireEvent.change(
      screen.getByPlaceholderText("Search identity providers…"),
      { target: { value: "catalog" } },
    );
    await waitFor(() =>
      expect(mocks.issuersSearch).toHaveBeenLastCalledWith("catalog"),
    );

    // The attached provider is left out of the choices.
    expect(screen.queryByText(/^Attached —/)).toBeNull();
    fireEvent.click(
      screen.getByText("Catalog provider — https://catalog.example.test"),
    );

    // The trigger shows the pick, resolved by id rather than from the page:
    // it stays even once no loaded page contains it.
    await waitFor(() =>
      expect(mocks.issuerById).toHaveBeenCalledWith(catalog.id),
    );
    mocks.issuersPage.mockReturnValue([]);
    fireEvent.click(screen.getAllByRole("combobox")[0] as HTMLElement);
    fireEvent.change(
      screen.getByPlaceholderText("Search identity providers…"),
      { target: { value: "nothing" } },
    );
    await waitFor(() =>
      expect(mocks.issuersSearch).toHaveBeenLastCalledWith("nothing"),
    );
    expect(screen.getAllByRole("combobox")[0]?.textContent).toContain(
      "Catalog provider — https://catalog.example.test",
    );
  });
});
