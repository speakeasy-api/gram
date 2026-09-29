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
  sessions: vi.fn(),
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
  fetchMetadata: vi.fn(),
  discoverProtectedResource: vi.fn(),
  commit: vi.fn(),
  detach: vi.fn(),
  userSessionIssuer: vi.fn(),
  toastSuccess: vi.fn(),
}));

vi.mock("sonner", () => ({
  toast: {
    success: mocks.toastSuccess,
    error: vi.fn(),
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
  useRemoteSessionIssuers: () => mocks.issuers(),
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

  it("requests the protected resource's scopes, not every advertised one", async () => {
    // Left empty, the server asks for everything the issuer advertises — the
    // request that broke Salesforce logins. Settings has to send the same
    // RFC 9728 scopes the create flow does.
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
    expect(mocks.commit).toHaveBeenCalledWith(
      expect.objectContaining({
        commitServerIdentityConfigurationForm: expect.objectContaining({
          clientMode: "auto",
          clientConfiguration: expect.objectContaining({
            scope: ["read"],
            tokenEndpointAuthMethod: "client_secret_post",
          }),
        }),
      }),
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
        .getByRole("radio", { name: /Manual/ })
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
          .getByRole("radio", { name: /Manual/ })
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
      name: /No Identity/,
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
    expect(screen.getByTitle("read write")).toBeDefined();
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

  it("restores the connected client on undo", () => {
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
    fireEvent.click(screen.getByRole("button", { name: "Undo" }));

    expect(screen.getByText("Connected")).toBeDefined();
    expect(screen.queryByRole("radio", { name: /Existing client/ })).toBeNull();
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

  it("sends the scopes typed for a manual client", async () => {
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
    fireEvent.click(screen.getByRole("radio", { name: /Manual/ }));
    fireEvent.change(screen.getByLabelText("Client ID"), {
      target: { value: "manual-client" },
    });
    fireEvent.click(screen.getByRole("button", { name: /Advanced/ }));
    fireEvent.change(screen.getByLabelText("Scope"), {
      target: { value: "read  write read" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(mocks.commit).toHaveBeenCalledOnce());
    expect(mocks.commit).toHaveBeenCalledWith(
      expect.objectContaining({
        commitServerIdentityConfigurationForm: expect.objectContaining({
          clientMode: "manual",
          clientConfiguration: expect.objectContaining({
            clientId: "manual-client",
            scope: ["read", "write"],
          }),
        }),
      }),
    );
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

  it("locks identity when the session issuer is organization-wide", () => {
    mocks.userSessionIssuer.mockReturnValue({
      data: { id: "user-session-issuer-1", projectId: "" },
    });

    renderIdentity();

    expect(screen.getByText(/organization-wide session issuer/i)).toBeDefined();
    expect(
      (
        screen.getByRole("radio", {
          name: /Service Account/,
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
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
    fireEvent.click(screen.getByRole("radio", { name: /No Identity/ }));
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
    fireEvent.click(screen.getByRole("radio", { name: /No Identity/ }));
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
    fireEvent.click(screen.getByRole("radio", { name: /No Identity/ }));
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
    fireEvent.click(screen.getByRole("radio", { name: /No Identity/ }));
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
});
