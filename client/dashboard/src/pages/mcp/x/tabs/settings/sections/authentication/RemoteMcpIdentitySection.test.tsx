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
  mocks.issuers.mockReturnValue({ data: { result: { items: [] } } });
  mocks.rbac.mockReturnValue({
    isLoading: false,
    hasScope: mocks.hasScope,
    hasAllScopes: () => true,
    hasAnyScope: (_scopes: string[], resourceId?: string) =>
      resourceId === "mcp-server-1",
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
  it("keeps User Identity unavailable until provider setup exists", () => {
    renderIdentity();

    expect(
      (screen.getByRole("radio", { name: "User" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    fireEvent.click(screen.getByRole("radio", { name: "User" }));
    expect(
      screen.getByRole("radio", { name: "None" }).getAttribute("aria-checked"),
    ).toBe("true");
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

  it("renders an existing User Identity as read-only", () => {
    mocks.clients.mockReturnValue({
      items: [{ remoteSessionIssuerId: "provider-1" }],
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
    expect(screen.getByText("Example provider")).toBeDefined();
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
      (screen.getByRole("radio", { name: "Agent" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    expect(
      screen.getByText(/can still send a credential upstream/i),
    ).toBeDefined();
    expect(
      screen.queryByText(
        "Requests to the upstream server will not include an Authorization credential.",
      ),
    ).toBeNull();
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

  it("shows non-link guidance when a shared source cannot be edited here", () => {
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
      screen.getByText(/when source management is available/i),
    ).toBeDefined();
    expect(
      screen.queryByRole("link", { name: /Remote MCP source/i }),
    ).toBeNull();
  });
});
