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

const mocks = vi.hoisted(() => ({
  attachClient: vi.fn(),
  commit: vi.fn(),
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
  hasScope: vi.fn(),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    isLoading: false,
    hasScope: mocks.hasScope,
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
    remoteSessions: {
      commitServerUserIdentityConfiguration: mocks.commit,
    },
    remoteSessionClients: {
      attachUserSessionIssuer: mocks.attachClient,
      create: mocks.createClient,
      createCimd: mocks.createCimdClient,
    },
    remoteSessionIssuers: { create: mocks.createProvider },
    userSessionIssuers: { create: mocks.createUserSessionIssuer },
  }),
}));

vi.mock("./useAllRemoteSessionClients", () => ({
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
    discoveredSnapshot: null,
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

function target(kind: AuthTarget["kind"]): AuthTarget {
  return {
    kind,
    slug: "example-server",
    projectId: "project-1",
    resourceId: "mcp-server-1",
    userSessionIssuerId: null,
    invalidate: mocks.invalidateTarget,
    linkUserSessionIssuer: mocks.linkTarget,
  };
}

function renderSheet(kind: AuthTarget["kind"]): void {
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
          target={target(kind)}
          userSessionIssuer={null}
          selectableIssuers={[]}
          initialIssuerUrl={
            kind === "remote-mcp" ? "https://id.example.test" : undefined
          }
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
  mocks.hasScope.mockReturnValue(true);
  mocks.commit.mockResolvedValue({
    manualSetupRequired: false,
    status: "registered",
  });
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
});

describe("AttachRemoteIdentityProviderSheet", () => {
  it("commits Remote MCP identity setup as one atomic plan", async () => {
    renderSheet("remote-mcp");

    await submitManualClient();

    await waitFor(() => {
      expect(mocks.commit).toHaveBeenCalledWith({
        commitServerUserIdentityConfigurationForm: expect.objectContaining({
          mcpServerId: "mcp-server-1",
          clientMode: "manual",
          clientConfiguration: expect.objectContaining({
            clientId: "client-1",
          }),
          createProvider: expect.objectContaining({
            issuer: "https://id.example.test",
            slug: "id-example-test",
          }),
        }),
      });
    });
    expect(mocks.createUserSessionIssuer).not.toHaveBeenCalled();
    expect(mocks.createProvider).not.toHaveBeenCalled();
    expect(mocks.createClient).not.toHaveBeenCalled();
  });

  it("keeps standard targets on the existing multi-call setup flow", async () => {
    renderSheet("standard");

    await submitManualClient();

    await waitFor(() => {
      expect(mocks.createUserSessionIssuer).toHaveBeenCalled();
      expect(mocks.createProvider).toHaveBeenCalled();
      expect(mocks.createClient).toHaveBeenCalled();
      expect(mocks.linkTarget).toHaveBeenCalledWith("user-session-issuer-1");
    });
    expect(mocks.commit).not.toHaveBeenCalled();
  });
});
