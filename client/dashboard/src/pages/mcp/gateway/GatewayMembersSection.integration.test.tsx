import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { MemoryRouter } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { MetaMcpServer } from "@gram/client/models/components/metamcpserver.js";
import { queryKeyMcpServers } from "@gram/client/react-query/mcpServers.js";
import { GatewayMembersSection } from "./GatewayMembersSection";

const api = vi.hoisted(() => ({
  servers: vi.fn(),
  members: vi.fn(),
  toolsets: vi.fn(),
  create: vi.fn(),
  attach: vi.fn(),
  markSettled: vi.fn(),
}));
// Generated query hooks, keys and invalidators remain production code.
// Observe the settlement callback without replacing its reconciliation behavior.
vi.mock("./useGatewayMemberRows", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("./useGatewayMemberRows")>();
  return {
    ...actual,
    useReconcileWrappers: (
      ...args: Parameters<typeof actual.useReconcileWrappers>
    ) => {
      const markSettled = actual.useReconcileWrappers(...args);
      return () => {
        api.markSettled();
        markSettled();
      };
    },
  };
});
vi.mock("@gram/client/funcs/mcpServersList.js", () => ({
  mcpServersList: api.servers,
}));
vi.mock("@gram/client/funcs/metaMcpListMembers.js", () => ({
  metaMcpListMembers: api.members,
}));
vi.mock("@gram/client/funcs/toolsetsList.js", () => ({
  toolsetsList: api.toolsets,
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "project",
  useSdkClient: () => ({
    mcpServers: { create: api.create },
    metaMcp: { addMember: api.attach },
  }),
}));
vi.mock("@/hooks/useRBAC", () => {
  const grants = new Set([
    "mcp:write:gateway",
    "mcp:write:project",
    "project:write:project",
  ]);
  const hasScope = (scope: string, resource?: string) =>
    grants.has(`${scope}:${resource}`);
  return {
    useRBAC: () => ({
      hasScope,
      hasAnyScope: (scopes: string[], resource?: string) =>
        scopes.some((scope) => hasScope(scope, resource)),
      hasAllScopes: (scopes: string[], resource?: string) =>
        scopes.every((scope) => hasScope(scope, resource)),
      isLoading: false,
    }),
  };
});
vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ isFeatureEnabled: () => false }),
}));
vi.mock("@/components/ui/Icon", () => ({ Icon: () => <span /> }));
vi.mock("@/components/sources/SourceCard", () => ({
  SourceMcpIcon: () => <span />,
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: {
      catalog: { href: () => "/catalog" },
      x: { overview: { href: () => "/server" } },
      add: Object.fromEntries(
        ["remote", "tunneled", "openapi", "fromSource", "function"].map(
          (key) => [key, { href: () => "/" + key }],
        ),
      ),
    },
  }),
}));

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: Error) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}
const ok = <T,>(value: T) => ({ ok: true as const, value });

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

it("reconciles a failed wrapper attachment through parent invalidation and retries the live wrapper in its reserved slot", async () => {
  const existing = {
    id: "existing",
    name: "Existing",
    slug: "existing",
    remoteMcpServerId: "remote",
    visibility: "private",
  } as McpServer;
  const wrapper = {
    id: "created-wrapper",
    name: "Alpha",
    slug: "alpha",
    toolsetId: "toolset",
    visibility: "private",
  } as McpServer;
  const unrelated = { ...wrapper, id: "other-wrapper", name: "Other wrapper" };
  const initialMembers = [
    { id: "member", mcpServerId: existing.id, sortOrder: 4 },
  ];
  const attach = deferred<void>();
  const refresh =
    deferred<ReturnType<typeof ok<{ mcpServers: McpServer[] }>>>();
  api.servers.mockResolvedValue(ok({ mcpServers: [existing] }));
  api.members.mockResolvedValue(ok({ members: initialMembers }));
  api.toolsets.mockResolvedValue(
    ok({ toolsets: [{ id: "toolset", name: "Alpha", slug: "alpha" }] }),
  );
  api.create.mockResolvedValue(wrapper);
  api.attach.mockReturnValueOnce(attach.promise).mockResolvedValue(undefined);
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: Infinity },
      mutations: { retry: false },
    },
  });
  const invalidation = vi.spyOn(client, "invalidateQueries");
  try {
    render(
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <TooltipProvider>
            <GatewayMembersSection
              metaMcpServer={
                { id: "gateway", projectId: "project" } as MetaMcpServer
              }
            />
          </TooltipProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    );
    await screen.findByText("Existing");
    fireEvent.click(screen.getByRole("button", { name: "Add servers" }));
    fireEvent.click(await screen.findByRole("checkbox", { name: "Alpha" }));
    fireEvent.click(
      screen.getByRole("button", { name: "Add selected servers" }),
    );
    await waitFor(() => expect(api.attach).toHaveBeenCalledTimes(1));
    expect(api.create).toHaveBeenCalledWith({
      createMcpServerForm: {
        name: "Alpha",
        toolsetId: "toolset",
        visibility: "private",
      },
    });
    expect(api.attach).toHaveBeenNthCalledWith(1, {
      addMetaMcpMemberForm: {
        metaMcpServerId: "gateway",
        mcpServerId: wrapper.id,
        sortOrder: 5,
      },
    });

    // A response arriving while attach is pending omits the newly minted row.
    // Drive a real query fetch, never an internal reconciliation callback.
    await act(async () => {
      await client.refetchQueries({
        queryKey: queryKeyMcpServers({ gramProject: "project" }),
      });
    });
    expect(api.servers).toHaveBeenCalledTimes(2);
    expect(
      client.getQueryData(queryKeyMcpServers({ gramProject: "project" })),
    ).toEqual({ mcpServers: [existing] });
    api.servers.mockReturnValueOnce(refresh.promise);
    // Membership changed concurrently: recomputing nextSortOrder would yield 41.
    api.members.mockResolvedValue(
      ok({ members: [{ ...initialMembers[0], sortOrder: 40 }] }),
    );
    await act(async () => {
      attach.reject(new Error("Attachment failed"));
    });
    await waitFor(() => expect(api.servers).toHaveBeenCalledTimes(3));
    expect(api.members).toHaveBeenCalledTimes(2);
    expect(api.markSettled).toHaveBeenCalledTimes(1);
    expect(api.markSettled.mock.invocationCallOrder[0]).toBeLessThan(
      invalidation.mock.invocationCallOrder[0]!,
    );
    expect(invalidation).toHaveBeenCalledWith({
      queryKey: ["gatewayInspection"],
    });
    expect(invalidation).toHaveBeenCalledWith({
      queryKey: ["gatewayDescribeServer"],
    });
    expect(
      (screen.getByRole("checkbox", { name: "Alpha" }) as HTMLInputElement)
        .disabled,
    ).toBe(true);

    // Parent finally marks writes settled and awaits authoritative invalidation.
    // The exact live wrapper wins even with another wrapper for the same toolset.
    api.servers.mockResolvedValue(
      ok({ mcpServers: [existing, unrelated, wrapper] }),
    );
    await act(async () => {
      refresh.resolve(ok({ mcpServers: [existing, unrelated, wrapper] }));
    });
    await screen.findByText(/Attachment failed/);
    await waitFor(() =>
      expect(
        (
          screen.getByRole("button", {
            name: "Add selected servers",
          }) as HTMLButtonElement
        ).disabled,
      ).toBe(false),
    );
    expect(
      screen
        .getByRole("checkbox", { name: "Alpha" })
        .getAttribute("aria-checked"),
    ).toBe("true");
    expect(
      screen
        .getByRole("checkbox", { name: "Other wrapper" })
        .getAttribute("aria-checked"),
    ).toBe("false");
    fireEvent.click(
      screen.getByRole("button", { name: "Add selected servers" }),
    );
    await waitFor(() => expect(api.attach).toHaveBeenCalledTimes(2));
    expect(api.attach).toHaveBeenNthCalledWith(2, {
      addMetaMcpMemberForm: {
        metaMcpServerId: "gateway",
        mcpServerId: wrapper.id,
        sortOrder: 5,
      },
    });
    expect(api.create).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(api.servers).toHaveBeenCalledTimes(4);
    expect(api.members).toHaveBeenCalledTimes(3);
    expect(api.markSettled).toHaveBeenCalledTimes(2);
  } finally {
    cleanup();
    client.clear();
  }
});
