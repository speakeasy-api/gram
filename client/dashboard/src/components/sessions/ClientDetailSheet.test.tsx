import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TooltipProvider } from "@/components/ui/Tooltip";
import type { UserSessionClient } from "@gram/client/models/components/usersessionclient.js";
import { ClientDetailSheet } from "./ClientDetailSheet";

const {
  useUserSessionClient,
  setUserSessionClientData,
  invalidateAllUserSessionClients,
  refreshMutate,
  hasScope,
} = vi.hoisted(() => ({
  useUserSessionClient: vi.fn(),
  setUserSessionClientData: vi.fn(),
  invalidateAllUserSessionClients: vi.fn(),
  refreshMutate: vi.fn(),
  hasScope: vi.fn(),
}));

vi.mock("@gram/client/react-query/userSessionClient.js", () => ({
  useUserSessionClient: (...args: unknown[]) => useUserSessionClient(...args),
  setUserSessionClientData: (...args: unknown[]) =>
    setUserSessionClientData(...args),
}));

vi.mock("@gram/client/react-query/userSessionClients.js", () => ({
  invalidateAllUserSessionClients: (...args: unknown[]) =>
    invalidateAllUserSessionClients(...args),
}));

vi.mock("@gram/client/react-query/refreshUserSessionClientCIMD.js", () => ({
  useRefreshUserSessionClientCIMDMutation: () => ({
    mutate: (...args: unknown[]) => refreshMutate(...args),
    isPending: false,
  }),
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
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <ClientDetailSheet client={client} open onOpenChange={() => {}} />
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
      request: { id: "client-1" },
    });
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

  it("prefers the freshly fetched detail over the listing row", () => {
    useUserSessionClient.mockReturnValue({
      data: cimdClient({ clientName: "Republished Name" }),
    });

    renderSheet(cimdClient({ clientName: "Stale Row Name" }));

    expect(screen.getByText("Republished Name")).toBeDefined();
    expect(screen.queryByText("Stale Row Name")).toBeNull();
  });
});
