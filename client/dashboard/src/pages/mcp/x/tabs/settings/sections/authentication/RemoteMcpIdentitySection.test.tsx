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
  rbac: vi.fn(),
  hasScope: vi.fn(),
  create: vi.fn(),
  update: vi.fn(),
  remove: vi.fn(),
  invalidateHeaders: vi.fn(),
  refetchHeaders: vi.fn(),
  authenticationProbe: vi.fn(),
  configureSheet: vi.fn(),
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: {
      x: { inspect: { href: (id: string) => `/mcp/x/${id}/inspect` } },
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

vi.mock("@gram/client/react-query/remoteSessionIssuers.js", () => ({
  useRemoteSessionIssuers: () => mocks.issuers(),
}));

vi.mock("./useAllRemoteSessionClients", () => ({
  useAllRemoteSessionClients: () => mocks.clients(),
}));

vi.mock("./useRemoteMcpAuthenticationProbe", () => ({
  useRemoteMcpAuthenticationProbe: (...args: unknown[]) =>
    mocks.authenticationProbe(...args),
}));

vi.mock("./ConfigureRemoteMcpUserIdentitySheet", () => ({
  ConfigureRemoteMcpUserIdentitySheet: (props: { open: boolean }) => {
    mocks.configureSheet(props);
    return props.open ? <div role="dialog">Configure User Identity</div> : null;
  },
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
        { id: "mcp-server-1", remoteMcpServerId: "remote-source-1" },
      ],
    },
    isLoading: false,
    isError: false,
  });
  mocks.issuers.mockReturnValue({
    data: { result: { items: [] } },
    isLoading: false,
    isError: false,
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
  mocks.invalidateHeaders.mockResolvedValue(undefined);
  mocks.authenticationProbe.mockReturnValue("available");
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("RemoteMcpIdentitySectionBody", () => {
  it("opens the atomic User Identity setup when no provider is linked", () => {
    renderIdentity();

    expect(
      (screen.getByRole("radio", { name: "User" }) as HTMLButtonElement)
        .disabled,
    ).toBe(false);
    fireEvent.click(screen.getByRole("radio", { name: "User" }));
    fireEvent.click(screen.getByRole("button", { name: "Configure" }));
    expect(screen.getByRole("dialog")).toBeDefined();
    expect(mocks.configureSheet).toHaveBeenLastCalledWith(
      expect.objectContaining({ open: true }),
    );
  });

  it("explains the difference between User and Agent Identity accessibly", () => {
    renderIdentity();

    fireEvent.click(screen.getByRole("button", { name: "What is this?" }));

    expect(
      screen.getByRole("dialog", { name: "User Identity and Agent Identity" }),
    ).toBeDefined();
    expect(screen.getByText(/access tokens remain separate/i)).toBeDefined();
    expect(screen.getByText(/one shared static Authorization/i)).toBeDefined();
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

  it("links an existing User Identity provider and client to management", () => {
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
            },
          ],
        },
      },
    });

    renderIdentity();

    expect(
      screen.getByRole("radio", { name: "User" }).getAttribute("aria-checked"),
    ).toBe("true");
    expect(
      (screen.getByRole("radio", { name: "Agent" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    expect(
      screen
        .getByRole("link", { name: "Example provider" })
        .getAttribute("href"),
    ).toBe("/org/providers/provider-1");
    expect(
      screen
        .getByRole("link", { name: "dashboard-client" })
        .getAttribute("href"),
    ).toBe("/org/providers/provider-1/clients/client-1");
    expect(screen.getByText(/1 connection/)).toBeDefined();
    expect(
      screen.getByRole("link", { name: "Inspect tab" }).getAttribute("href"),
    ).toBe("/mcp/x/mcp-server-1/inspect");
    fireEvent.click(screen.getByRole("button", { name: "Change" }));
    expect(screen.getByRole("dialog")).toBeDefined();
    expect(mocks.configureSheet).toHaveBeenLastCalledWith(
      expect.objectContaining({
        open: true,
        initialProviderId: "provider-1",
      }),
    );
    expect(mocks.authenticationProbe).toHaveBeenLastCalledWith(
      "remote-source-1",
      false,
    );
  });

  it("never renders entered credential values in the preview and clears them after save", async () => {
    const { container } = renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: "Agent" }));
    fireEvent.click(screen.getByRole("radio", { name: "Bearer" }));

    const input = screen.getByLabelText("Bearer token");
    fireEvent.change(input, { target: { value: "bearer-secret-value" } });

    expect((input as HTMLInputElement).type).toBe("password");
    expect(
      screen.getByRole("status", { name: "Authorization preview" }).textContent,
    ).toContain("Authorization: Bearer [redacted]");
    expect(container.textContent).not.toContain("bearer-secret-value");

    fireEvent.click(screen.getByRole("button", { name: "Save credential" }));
    await waitFor(() => expect(mocks.create).toHaveBeenCalledOnce());
    await waitFor(() => expect((input as HTMLInputElement).value).toBe(""));
  });

  it("labels Basic and Manual credential inputs without exposing their values", () => {
    const { container } = renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: "Agent" }));
    fireEvent.click(screen.getByRole("radio", { name: "Basic" }));

    fireEvent.change(screen.getByLabelText("Basic username"), {
      target: { value: "operator" },
    });
    fireEvent.change(screen.getByLabelText("Basic password"), {
      target: { value: "basic-secret" },
    });
    expect(
      (screen.getByLabelText("Basic password") as HTMLInputElement).type,
    ).toBe("password");
    expect(container.textContent).not.toContain("basic-secret");
    expect(container.textContent).not.toContain("b3BlcmF0b3I6YmFzaWMtc2VjcmV0");

    fireEvent.click(screen.getByRole("radio", { name: "Manual" }));
    fireEvent.change(screen.getByLabelText("Authorization value"), {
      target: { value: "Custom manual-secret" },
    });
    expect(
      (screen.getByLabelText("Authorization value") as HTMLInputElement).type,
    ).toBe("password");
    expect(container.textContent).not.toContain("manual-secret");
  });

  it("allows User setup past legacy Authorization while blocking Agent and None", () => {
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
      (screen.getByRole("radio", { name: "Agent" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    expect(
      (screen.getByRole("radio", { name: "None" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    fireEvent.click(screen.getByRole("radio", { name: "User" }));
    fireEvent.click(screen.getByRole("button", { name: "Configure" }));
    expect(screen.getByRole("dialog")).toBeDefined();
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
    expect(screen.getByText("Identity mode is unavailable.")).toBeDefined();
    expect(screen.queryByRole("radio", { name: "None" })).toBeNull();
    expect(
      screen.queryByText(/will not include an Authorization credential/i),
    ).toBeNull();
  });

  it("keeps User Identity configuration unavailable when issuers fail", () => {
    mocks.issuers.mockReturnValue({
      data: undefined,
      isLoading: false,
      isError: true,
    });

    renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: "User" }));

    expect(
      screen.getByText(/User Identity configuration is unavailable/i),
    ).toBeDefined();
    expect(
      (screen.getByRole("button", { name: "Configure" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
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
      (screen.getByRole("radio", { name: "Agent" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    fireEvent.click(screen.getByRole("radio", { name: "Agent" }));
    expect(screen.queryByText("Authorization credential")).toBeNull();
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
      (screen.getByRole("radio", { name: "Agent" }) as HTMLButtonElement)
        .disabled,
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
    fireEvent.click(screen.getByRole("radio", { name: "None" }));
    expect(screen.getByRole("dialog")).toBeDefined();

    fireEvent.click(screen.getByRole("button", { name: "Remove credential" }));
    await waitFor(() =>
      expect(mocks.remove).toHaveBeenCalledWith({
        request: { id: "header-1" },
      }),
    );
  });

  it("rechecks shared-source read-only state before destructive confirmation", () => {
    mocks.headers.mockReturnValue({
      data: { headers: [configuredHeader()] },
      isLoading: false,
      isError: false,
      error: null,
      refetch: mocks.refetchHeaders,
    });

    const view = renderIdentity();
    fireEvent.click(screen.getByRole("radio", { name: "None" }));
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
    view.rerender(
      <MemoryRouter>
        <QueryClientProvider client={new QueryClient()}>
          <TooltipProvider>
            <RemoteMcpIdentitySectionBody target={target} />
          </TooltipProvider>
        </QueryClientProvider>
      </MemoryRouter>,
    );

    expect(
      (
        screen.getByRole("button", {
          name: "Remove credential",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Remove credential" }));
    expect(mocks.remove).not.toHaveBeenCalled();
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
