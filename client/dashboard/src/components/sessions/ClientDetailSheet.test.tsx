import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TooltipProvider } from "@/components/ui/Tooltip";
import type { UserSessionClient } from "@gram/client/models/components/usersessionclient.js";
import { ClientDetailSheet } from "./ClientDetailSheet";

const {
  useUserSessionClient,
  setUserSessionClientData,
  invalidateUserSessionClient,
  invalidateAllUserSessionClients,
  refreshMutate,
  hasScope,
  refreshHookOptions,
} = vi.hoisted(() => ({
  useUserSessionClient: vi.fn(),
  setUserSessionClientData: vi.fn(),
  invalidateUserSessionClient: vi.fn(),
  invalidateAllUserSessionClients: vi.fn(),
  refreshMutate: vi.fn(),
  hasScope: vi.fn(),
  refreshHookOptions: {} as {
    options?: {
      onSuccess?: (data: unknown) => Promise<void>;
      onError?: (error: unknown) => Promise<void>;
    };
  },
}));

vi.mock("@gram/client/react-query/userSessionClient.js", () => ({
  useUserSessionClient: (...args: unknown[]) => useUserSessionClient(...args),
  setUserSessionClientData: (...args: unknown[]) =>
    setUserSessionClientData(...args),
  invalidateUserSessionClient: (...args: unknown[]) =>
    invalidateUserSessionClient(...args),
}));

vi.mock("@gram/client/react-query/userSessionClients.js", () => ({
  invalidateAllUserSessionClients: (...args: unknown[]) =>
    invalidateAllUserSessionClients(...args),
}));

vi.mock("@gram/client/react-query/refreshUserSessionClientCIMD.js", () => ({
  useRefreshUserSessionClientCIMDMutation: (options?: {
    onSuccess?: (data: unknown) => Promise<void>;
    onError?: (error: unknown) => Promise<void>;
  }) => {
    refreshHookOptions.options = options;
    return {
      mutate: (...args: unknown[]) => refreshMutate(...args),
      isPending: false,
    };
  },
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (...args: unknown[]) => hasScope(...args),
    hasAnyScope: () => true,
  }),
}));

vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project-1", slug: "project-1" }),
}));

// The route-derived fallback the SDK would otherwise apply itself.
vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "project-1",
}));

const DOCUMENT_URL = "https://client.example.com/oauth/client.json";

function cimdClient(overrides: Partial<UserSessionClient> = {}) {
  return {
    id: "client-1",
    userSessionIssuerId: "issuer-1",
    clientId: DOCUMENT_URL,
    clientIdMetadataUri: DOCUMENT_URL,
    clientIdMetadataFetchedAt: new Date("2026-02-01T10:00:00Z"),
    clientIdMetadataCacheExpiresAt: new Date("2026-02-01T11:00:00Z"),
    clientIdMetadataEtag: '"v1"',
    clientName: "CIMD Client",
    redirectUris: ["https://client.example.com/cb"],
    clientIdIssuedAt: new Date("2026-01-01T00:00:00Z"),
    createdAt: new Date("2026-01-01T00:00:00Z"),
    updatedAt: new Date("2026-01-01T00:00:00Z"),
    activeSessionCount: 2,
    credentialKind: "public",
    ...overrides,
  } as UserSessionClient;
}

function dcrClient(overrides: Partial<UserSessionClient> = {}) {
  return cimdClient({
    clientId: "minted-client-id",
    clientIdMetadataUri: undefined,
    clientIdMetadataFetchedAt: undefined,
    clientIdMetadataCacheExpiresAt: undefined,
    clientIdMetadataEtag: undefined,
    clientName: "DCR Client",
    ...overrides,
  });
}

function renderSheet(client: UserSessionClient) {
  return renderSheetForId(client.id, client);
}

function renderSheetForId(
  clientId: string,
  client?: UserSessionClient,
  projectSlug?: string,
) {
  const project = projectSlug
    ? { slug: projectSlug, id: `${projectSlug}-id` }
    : undefined;
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <ClientDetailSheet
          clientId={clientId}
          client={client}
          project={project}
          open
          onOpenChange={() => {}}
        />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

describe("ClientDetailSheet", () => {
  beforeEach(() => {
    useUserSessionClient.mockReturnValue({ data: undefined });
    hasScope.mockReturnValue(true);
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("renders the CIMD metadata panel for a CIMD-resolved client", () => {
    renderSheet(cimdClient());

    expect(screen.getByText("Metadata document")).toBeDefined();
    expect(screen.getByText("Last fetched")).toBeDefined();
    expect(screen.getByText("Cache expires")).toBeDefined();
    expect(screen.getByText("ETag")).toBeDefined();
    expect(screen.getByText('"v1"')).toBeDefined();

    // The document URL renders as a safe external link.
    const link = screen.getByRole("link", { name: DOCUMENT_URL });
    expect(link.getAttribute("href")).toBe(DOCUMENT_URL);
    expect(link.getAttribute("target")).toBe("_blank");
    expect(link.getAttribute("rel")).toBe("noopener noreferrer");

    expect(
      screen.getByRole("button", { name: "Refresh metadata" }),
    ).toBeDefined();
  });

  it("renders the base detail without the CIMD panel for a DCR client", () => {
    renderSheet(dcrClient());

    expect(screen.getByText("Client ID")).toBeDefined();
    // Once in the header (a DCR row has no document origin, so the minted id
    // is the secondary label) and once in the Client ID field.
    expect(screen.getAllByText("minted-client-id")).toHaveLength(2);
    expect(screen.getByText("Redirect URIs")).toBeDefined();
    expect(screen.queryByText("Metadata document")).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Refresh metadata" }),
    ).toBeNull();
  });

  it("triggers the refresh mutation for this client", () => {
    renderSheet(cimdClient());

    fireEvent.click(screen.getByRole("button", { name: "Refresh metadata" }));

    expect(refreshMutate).toHaveBeenCalledWith({
      request: { id: "client-1", gramProject: "project-1" },
    });
  });

  // The sheet reads its detail from a project-scoped key, so a refresh that
  // seeded an unscoped one would land where nothing is watching and the panel
  // would keep showing the pre-refresh copy.
  it("seeds the refreshed view under the key the sheet reads", async () => {
    renderSheetForId("client-1", cimdClient(), "analytics");

    const fresh = cimdClient({ clientName: "Refreshed Name" });
    await refreshHookOptions.options?.onSuccess?.(fresh);

    expect(setUserSessionClientData).toHaveBeenCalledWith(
      expect.anything(),
      [{ id: "client-1", gramProject: "analytics" }],
      fresh,
    );
  });

  it("invalidates the same key after a failed refresh", async () => {
    renderSheetForId("client-1", cimdClient(), "analytics");

    await refreshHookOptions.options?.onError?.(new Error("boom"));

    expect(invalidateUserSessionClient).toHaveBeenCalledWith(
      expect.anything(),
      [{ id: "client-1", gramProject: "analytics" }],
      expect.objectContaining({ refetchType: "all" }),
    );
  });

  it("hides the refresh button without project write access", () => {
    // The backend gates refresh on project:write for this project; the
    // check must be scoped by project id, not existential.
    hasScope.mockImplementation(
      (scope: string, projectId: string) =>
        !(scope === "project:write" && projectId === "project-1"),
    );

    renderSheet(cimdClient());

    expect(screen.getByText("Metadata document")).toBeDefined();
    expect(
      screen.queryByRole("button", { name: "Refresh metadata" }),
    ).toBeNull();
    expect(hasScope).toHaveBeenCalledWith("project:write", "project-1");
  });

  it("refetches the client after a failed refresh so purged state shows", async () => {
    // The backend commits the purge before fetching, so a failed re-read has
    // still cleared the cache; the sheet must refetch rather than keep
    // rendering the pre-purge copy.
    renderSheet(cimdClient());

    await refreshHookOptions.options?.onError?.(new Error("boom"));

    expect(invalidateUserSessionClient).toHaveBeenCalled();
    expect(invalidateAllUserSessionClients).toHaveBeenCalled();
  });

  it("prefers the freshly fetched detail over the listing row", () => {
    useUserSessionClient.mockReturnValue({
      data: cimdClient({ clientName: "Republished Name" }),
    });

    renderSheet(cimdClient({ clientName: "Stale Row Name" }));

    expect(screen.getByText("Republished Name")).toBeDefined();
    expect(screen.queryByText("Stale Row Name")).toBeNull();
  });

  // The organization page names its project through a filter rather than the
  // route, and the SDK stamps an unscoped request with the literal "default".
  // Left to that fallback, the lookup misses for every org whose selected
  // project is not slugged "default".
  it("scopes the lookup to the project it is given", () => {
    renderSheetForId("client-1", cimdClient(), "analytics");

    expect(useUserSessionClient).toHaveBeenCalledWith(
      { id: "client-1", gramProject: "analytics" },
      undefined,
      expect.objectContaining({ enabled: true }),
    );
  });

  // The refresh is a write against the same registration, so it has to be sent
  // with the project the lookup used. Left to the SDK's fallback it would go to
  // the literal "default" project and fail on a button that looked live.
  it("sends the refresh with the project it was given", () => {
    renderSheetForId("client-1", cimdClient(), "analytics");

    const button = screen.getByRole("button", { name: "Refresh metadata" });
    fireEvent.click(button);

    expect(refreshMutate).toHaveBeenCalledWith({
      request: { id: "client-1", gramProject: "analytics" },
    });
  });

  // Opened from a surface that holds only an id, the sheet has nothing to
  // render until the query lands, so it says so rather than showing an empty
  // panel or a half-built one.
  it("renders a loading state when opened with only a client id", () => {
    useUserSessionClient.mockReturnValue({ data: undefined });

    renderSheetForId("client-1");

    expect(screen.getByText("Loading…")).toBeDefined();
    expect(screen.queryByText("Redirect URIs")).toBeNull();
  });

  it("reports a registration that could not be loaded", () => {
    useUserSessionClient.mockReturnValue({ data: undefined, isError: true });

    renderSheetForId("client-1");

    expect(
      screen.getByText("This registration could not be loaded."),
    ).toBeDefined();
  });

  // The listing badges only key and misconfigured, so the sheet is the one
  // place a public client is distinguishable from a secret-authenticating one.
  it("states the authentication kind for a public client", () => {
    renderSheet(cimdClient({ credentialKind: "public" }));

    expect(screen.getByText("Authentication")).toBeDefined();
    expect(screen.getByText("Public")).toBeDefined();
    expect(screen.getByText("Not declared")).toBeDefined();
  });

  it("writes out the declared method beneath the resolved kind", () => {
    renderSheet(
      cimdClient({
        credentialKind: "key",
        tokenEndpointAuthMethod: "private_key_jwt",
      }),
    );

    // Once in the header badge and once as the field label.
    expect(screen.getAllByText("Signed")).toHaveLength(2);
    expect(screen.getByText("private_key_jwt")).toBeDefined();
  });

  it("names a registration that cannot authenticate at all", () => {
    renderSheet(
      cimdClient({
        credentialKind: "misconfigured",
        tokenEndpointAuthMethod: "private_key_jwt",
      }),
    );

    expect(screen.getAllByText("Cannot authenticate").length).toBeGreaterThan(
      0,
    );
  });
});
