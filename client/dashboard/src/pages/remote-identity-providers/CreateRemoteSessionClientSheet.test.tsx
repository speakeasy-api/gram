import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";

const isPlatformAdmin = vi.fn();
const proxyRegister = vi.fn();
const createClient = vi.fn();
const onOpenChange = vi.fn<(open: boolean) => void>();

vi.mock("@/contexts/Auth", () => ({
  useIsPlatformAdmin: () => isPlatformAdmin(),
  useOrganization: () => ({
    id: "org-1",
    projects: [{ id: "project-1", name: "Project One", slug: "project-one" }],
  }),
}));

vi.mock("@/contexts/Fetcher", () => ({
  useFetcher: () => ({ fetch: vi.fn() }),
}));

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    organizationRemoteSessionClients: {
      create: createClient,
      createCimd: vi.fn(),
    },
  }),
}));

vi.mock("@/lib/proxyRegisterUpstreamClient", () => ({
  proxyRegisterUpstreamClient: (...args: unknown[]) => proxyRegister(...args),
}));

vi.mock("@gram/client/react-query/organizationRemoteSessionClients.js", () => ({
  invalidateAllOrganizationRemoteSessionClients: vi.fn(() => Promise.resolve()),
}));

vi.mock("@gram/client/react-query/organizationRemoteSessionIssuers.js", () => ({
  invalidateAllOrganizationRemoteSessionIssuers: vi.fn(() => Promise.resolve()),
}));

vi.mock(
  "../mcp/x/tabs/settings/sections/authentication/IssuerFormFields",
  () => ({
    ClientTypeFields: ({ availableTypes }: { availableTypes: string[] }) => (
      <output data-testid="client-types">{availableTypes.join(",")}</output>
    ),
    OverridesFields: () => null,
  }),
);

vi.mock("@/components/ui/Sheet", () => ({
  Sheet: ({ open, children }: { open: boolean; children: ReactNode }) =>
    open ? children : null,
  SheetContent: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
  SheetFooter: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  SheetHeader: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  SheetTitle: ({ children }: { children: ReactNode }) => <h2>{children}</h2>,
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), warning: vi.fn() },
}));

import { CreateRemoteSessionClientSheet } from "./CreateRemoteSessionClientSheet";

beforeEach(() => {
  vi.clearAllMocks();
  proxyRegister.mockResolvedValue({
    clientId: "registered-client",
    clientSecret: "registered-secret",
    tokenEndpointAuthMethod: "client_secret_basic",
  });
  createClient.mockResolvedValue({ id: "client-1" });
});

afterEach(() => {
  cleanup();
});

describe("CreateRemoteSessionClientSheet", () => {
  it("resolves the tunnel project synchronously before a platform admin submits DCR", async () => {
    isPlatformAdmin.mockReturnValue(true);
    renderSheet(tunneledIssuer());

    expect(screen.getByTestId("client-types").textContent).toContain("dcr");
    fireEvent.click(screen.getByRole("button", { name: "Create" }));

    await waitFor(() => {
      expect(proxyRegister).toHaveBeenCalledWith(
        expect.any(Function),
        expect.objectContaining({
          tunneledMcpServerId: "tunnel-1",
          projectSlug: "project-one",
        }),
      );
    });
  });

  it("offers Manual or CIMD instead of tunneled DCR to non-platform admins", () => {
    isPlatformAdmin.mockReturnValue(false);
    renderSheet(tunneledIssuer({ clientIdMetadataDocumentSupported: true }));

    expect(screen.getByTestId("client-types").textContent).toBe("cimd,manual");
    expect(
      screen.getByText(/requires platform admin access\. Use Manual/),
    ).toBeTruthy();
  });
});

function renderSheet(issuer: RemoteSessionIssuer): void {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <CreateRemoteSessionClientSheet
        open
        onOpenChange={onOpenChange}
        issuer={issuer}
      />
    </QueryClientProvider>,
  );
}

function tunneledIssuer(
  overrides: Partial<RemoteSessionIssuer> = {},
): RemoteSessionIssuer {
  return {
    id: "issuer-1",
    projectId: "project-1",
    organizationId: "org-1",
    slug: "issuer",
    issuer: "https://idp.example.com",
    registrationEndpoint: "http://idp.internal/register",
    scopesSupported: [],
    grantTypesSupported: [],
    responseTypesSupported: [],
    tokenEndpointAuthMethodsSupported: [],
    clientIdMetadataDocumentSupported: false,
    tunneledMcpServerId: "tunnel-1",
    oidc: false,
    passthrough: false,
    createdAt: new Date(0),
    updatedAt: new Date(0),
    ...overrides,
  };
}
