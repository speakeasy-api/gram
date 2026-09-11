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

const mocks = vi.hoisted(() => ({
  headers: vi.fn(),
  clients: vi.fn(),
  siblings: vi.fn(),
  issuers: vi.fn(),
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
  commit: vi.fn(),
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
  }),
  useOrgRoutes: () => ({
    remoteIdentityProviders: {
      href: () => "/org/remote-identity-providers",
      issuerDetail: { href: (id: string) => `/org/providers/${id}` },
      clientDetail: {
        href: (issuerId: string, clientId: string) =>
          `/org/providers/${issuerId}/clients/${clientId}`,
      },
    },
  }),
}));

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    remoteSessions: { commitServerUserIdentityConfiguration: mocks.commit },
    remoteSessionIssuers: { fetchMetadata: vi.fn() },
  }),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => mocks.rbac(),
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
  useRemoteSessionIssuers: () => mocks.issuers(),
  invalidateAllRemoteSessionIssuers: vi.fn(),
}));

vi.mock("@gram/client/react-query/remoteSessionClients.js", () => ({
  invalidateAllRemoteSessionClients: vi.fn(),
}));

vi.mock("./useAllRemoteSessionClients", () => ({
  useAllRemoteSessionClients: () => mocks.clients(),
}));

vi.mock("./useRemoteMcpAuthenticationProbe", () => ({
  useRemoteMcpAuthenticationProbe: (...args: unknown[]) =>
    mocks.authenticationProbe(...args),
}));

vi.mock("./useProtectedResourceMetadata", () => ({
  useProtectedResourceMetadata: (...args: unknown[]) =>
    mocks.protectedResourceMetadata(...args),
}));

vi.mock("@gram/client/react-query/createRemoteMcpServerHeader.js", () => ({
  useCreateRemoteMcpServerHeaderMutation: () => ({
    mutateAsync: mocks.create,
    isPending: false,
    error: null,
  }),
}));

vi.mock("@gram/client/react-query/updateRemoteMcpServerHeader.js", () => ({
  useUpdateRemoteMcpServerHeaderMutation: () => ({
    mutateAsync: mocks.update,
    isPending: false,
    error: null,
  }),
}));

vi.mock("@gram/client/react-query/deleteRemoteMcpServerHeader.js", () => ({
  useDeleteRemoteMcpServerHeaderMutation: () => ({
    mutateAsync: mocks.remove,
    isPending: false,
    error: null,
  }),
}));

function renderIdentity(): ReturnType<typeof render> {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <MemoryRouter>
      <QueryClientProvider client={queryClient}>
        <TooltipProvider>
          <RemoteMcpIdentitySectionBody target={target} />
        </TooltipProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
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

const target: AuthTarget = {
  kind: "remote-mcp",
  slug: "remote-server",
  projectId: "project-1",
  resourceId: "mcp-server-1",
  userSessionIssuerId: "user-session-issuer-1",
  remoteMcpServerId: "remote-source-1",
  invalidate: vi.fn(),
};

beforeEach(() => {
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
  mocks.invalidateHeaders.mockResolvedValue(undefined);
  mocks.authenticationProbe.mockReturnValue("available");
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
    expect(screen.getByRole("radio", { name: /Agent Identity/ })).toBeDefined();
    expect(screen.getByRole("radio", { name: /No Identity/ })).toBeDefined();
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
            },
          ],
        },
      },
    });

    renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: /User Identity/ }));

    // One provider, one registration choice — no issuer, DCR or CIMD wording.
    expect(screen.getByLabelText("Identity provider")).toBeDefined();
    expect(screen.getByLabelText("Registration")).toBeDefined();
    expect(screen.getByText("Auto-Configure")).toBeDefined();
    expect(screen.queryByText(/CIMD/i)).toBeNull();
    expect(screen.queryByText(/DCR/i)).toBeNull();
    expect(screen.queryByText(/issuer/i)).toBeNull();
    expect(screen.queryByText(/audience/i)).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(mocks.commit).toHaveBeenCalledOnce());
    expect(mocks.commit).toHaveBeenCalledWith(
      expect.objectContaining({
        commitServerUserIdentityConfigurationForm: expect.objectContaining({
          mcpServerId: "mcp-server-1",
          providerId: "provider-1",
          clientMode: "auto",
        }),
      }),
    );
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

  it("warns when the structured probe says No Identity cannot authenticate", () => {
    mocks.authenticationProbe.mockReturnValue("authentication-required");

    renderIdentity();

    expect(
      screen.getByText(
        /upstream server reported that authentication is required/i,
      ),
    ).toBeDefined();
    expect(mocks.authenticationProbe).toHaveBeenCalledWith(
      "remote-source-1",
      true,
    );
  });

  it("locks the mode to User once a client is linked", () => {
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

    expect(
      screen
        .getByRole("radio", { name: /User Identity/ })
        .getAttribute("aria-checked"),
    ).toBe("true");
    expect(
      (
        screen.getByRole("radio", {
          name: /Agent Identity/,
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    expect(screen.getByText("Example provider")).toBeDefined();
    expect(mocks.authenticationProbe).toHaveBeenLastCalledWith(
      "remote-source-1",
      false,
    );
  });

  it("previews the Authorization header without revealing the credential", async () => {
    const { container } = renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: /Agent Identity/ }));

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
    fireEvent.click(screen.getByRole("radio", { name: /Agent Identity/ }));
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

  it("keeps Client Credentials visible but unselectable", () => {
    renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: /Agent Identity/ }));

    expect(screen.getByText("Coming soon")).toBeDefined();
    fireEvent.click(screen.getByRole("button", { name: /Client credentials/ }));
    // Still on Bearer: the segment advertises the roadmap, it does not switch.
    expect(screen.getByLabelText("Token")).toBeDefined();
  });

  it("blocks Agent Identity and describes legacy pass-through Authorization honestly", () => {
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

    expect(
      screen.getAllByText(/legacy pass-through Authorization header/i),
    ).toHaveLength(2);
    expect(
      (
        screen.getByRole("radio", {
          name: /Agent Identity/,
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    expect(
      screen.getByText(/can still send a credential upstream/i),
    ).toBeDefined();
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
    expect(screen.queryByRole("radio", { name: /No Identity/ })).toBeNull();
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
          name: /Agent Identity/,
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    fireEvent.click(screen.getByRole("radio", { name: /Agent Identity/ }));
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
          name: /Agent Identity/,
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  });

  it("confirms and removes Agent Identity with target permission", async () => {
    mocks.headers.mockReturnValue({
      data: { headers: [configuredHeader()] },
      isLoading: false,
      isError: false,
      error: null,
      refetch: mocks.refetchHeaders,
    });

    renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: /No Identity/ }));
    expect(screen.getByRole("dialog")).toBeDefined();

    fireEvent.click(screen.getByRole("button", { name: "Remove credential" }));
    await waitFor(() =>
      expect(mocks.remove).toHaveBeenCalledWith({
        request: { id: "header-1" },
      }),
    );
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
    ).toBe("/org/remote-identity-providers");
  });
});
