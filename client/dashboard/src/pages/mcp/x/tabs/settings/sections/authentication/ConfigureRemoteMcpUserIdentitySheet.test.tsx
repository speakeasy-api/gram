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
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { ConfigureRemoteMcpUserIdentitySheet } from "./ConfigureRemoteMcpUserIdentitySheet";
import type { AuthTarget } from "./authTarget";

const mocks = vi.hoisted(() => ({
  clients: vi.fn(),
  commit: vi.fn(),
  hasScope: vi.fn(),
  invalidate: vi.fn(),
  onOpenChange: vi.fn(),
}));

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    remoteSessions: {
      commitServerUserIdentityConfiguration: mocks.commit,
    },
  }),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: mocks.hasScope, isLoading: false }),
}));

vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    remoteIdentityProviders: { href: () => "/org/remote-identity-providers" },
  }),
}));

vi.mock("./useAllRemoteSessionClients", () => ({
  useAllRemoteSessionClients: () => {
    const result = mocks.clients();
    return Array.isArray(result)
      ? { items: result, isLoading: false, isError: false }
      : result;
  },
}));

const target: AuthTarget = {
  kind: "remote-mcp",
  slug: "example-server",
  projectId: "project-1",
  resourceId: "mcp-server-1",
  userSessionIssuerId: "user-session-issuer-1",
  remoteMcpServerId: "remote-source-1",
  invalidate: mocks.invalidate,
};

const provider: RemoteSessionIssuer = {
  id: "provider-1",
  projectId: "project-1",
  organizationId: "organization-1",
  slug: "example-provider",
  name: "Example provider",
  issuer: "https://identity.example.test",
  authorizationEndpoint: "https://identity.example.test/authorize",
  tokenEndpoint: "https://identity.example.test/token",
  registrationEndpoint: "https://identity.example.test/register",
  clientIdMetadataDocumentSupported: true,
  oidc: false,
  passthrough: false,
  scopesSupported: [],
  grantTypesSupported: [],
  responseTypesSupported: [],
  tokenEndpointAuthMethodsSupported: [],
  createdAt: new Date(0),
  updatedAt: new Date(0),
};

function renderSheet(overrides: Partial<RemoteSessionIssuer> = {}): void {
  render(
    <MemoryRouter>
      <QueryClientProvider client={new QueryClient()}>
        <ConfigureRemoteMcpUserIdentitySheet
          open
          onOpenChange={(nextOpen) => {
            mocks.onOpenChange(nextOpen);
          }}
          target={target}
          issuers={[{ ...provider, ...overrides }]}
        />
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  mocks.clients.mockReturnValue([]);
  mocks.commit.mockResolvedValue({ status: "registered" });
  mocks.hasScope.mockReturnValue(true);
  mocks.invalidate.mockResolvedValue(undefined);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("ConfigureRemoteMcpUserIdentitySheet", () => {
  it("uses the atomic auto mode with CIMD preferred over DCR", async () => {
    renderSheet();

    await waitFor(() =>
      expect(screen.getByText("Automatic (CIMD)")).toBeDefined(),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Configure User Identity" }),
    );

    await waitFor(() =>
      expect(mocks.commit).toHaveBeenCalledWith({
        commitServerUserIdentityConfigurationForm: expect.objectContaining({
          mcpServerId: "mcp-server-1",
          providerId: "provider-1",
          clientMode: "auto",
        }),
      }),
    );
  });

  it("keeps Manual available without rejecting a duplicate client ID", async () => {
    mocks.clients.mockReturnValue([
      {
        id: "client-1",
        clientId: "shared-client-id",
        remoteSessionIssuerId: "provider-1",
        userSessionIssuerIds: [],
      },
      {
        id: "client-2",
        clientId: "shared-client-id",
        remoteSessionIssuerId: "provider-1",
        userSessionIssuerIds: [],
      },
    ]);
    renderSheet({
      clientIdMetadataDocumentSupported: false,
      registrationEndpoint: undefined,
    });

    fireEvent.click(screen.getByRole("combobox", { name: "OAuth client" }));
    expect(
      screen.getAllByRole("option", { name: /shared-client-id/ }),
    ).toHaveLength(2);
    fireEvent.keyDown(document.activeElement ?? document.body, {
      key: "Escape",
    });
    fireEvent.change(screen.getByLabelText("Client ID"), {
      target: { value: "shared-client-id" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Configure User Identity" }),
    );

    await waitFor(() =>
      expect(mocks.commit).toHaveBeenCalledWith({
        commitServerUserIdentityConfigurationForm: expect.objectContaining({
          clientMode: "manual",
          clientConfiguration: expect.objectContaining({
            clientId: "shared-client-id",
          }),
        }),
      }),
    );
  });

  it("blocks configuration when existing clients cannot be loaded", () => {
    mocks.clients.mockReturnValue({
      items: [],
      isLoading: false,
      isError: true,
    });
    renderSheet();

    expect(
      screen.getByText(/Existing OAuth clients could not be loaded/i),
    ).toBeDefined();
    expect(
      (
        screen.getByRole("button", {
          name: "Configure User Identity",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    fireEvent.click(
      screen.getByRole("button", { name: "Configure User Identity" }),
    );
    expect(mocks.commit).not.toHaveBeenCalled();
  });
});
