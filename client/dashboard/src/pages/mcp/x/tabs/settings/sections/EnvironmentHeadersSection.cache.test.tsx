import { TooltipProvider } from "@/components/ui/Tooltip";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { McpServerEnvironmentHeaders } from "@gram/client/models/components/mcpserverenvironmentheaders.js";
import type { GetMcpServerEnvironmentHeadersRequest } from "@gram/client/models/operations/getmcpserverenvironmentheaders.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { EnvironmentHeadersSection } from "./EnvironmentHeadersSection";

// These tests keep the generated preview hook and a real QueryClient, so they
// prove what the cache does; only the transport, access grants, the save
// mutation and the Select widget are doubles.

const PROJECT = "project-1";

const mocks = vi.hoisted(() => ({
  preview: vi.fn<(request: GetMcpServerEnvironmentHeadersRequest) => unknown>(),
  saveSuccess: undefined as undefined | (() => Promise<void>),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: () => true,
    hasAnyScope: () => true,
    hasAllScopes: () => true,
    isLoading: false,
  }),
}));

vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));

vi.mock("@gram/client/funcs/mcpServersGetEnvironmentHeaders.js", () => ({
  mcpServersGetEnvironmentHeaders: async (
    _client: unknown,
    request: GetMcpServerEnvironmentHeadersRequest,
  ) => ({ ok: true, value: mocks.preview(request) }),
}));

vi.mock(
  "@gram/client/react-query/updateMcpServer.js",
  async (importOriginal) => ({
    ...(await importOriginal<
      typeof import("@gram/client/react-query/updateMcpServer.js")
    >()),
    useUpdateMcpServerMutation: (options: {
      onSuccess: () => Promise<void>;
    }) => {
      mocks.saveSuccess = options.onSuccess;
      return { mutate: vi.fn(), isPending: false };
    },
  }),
);

vi.mock("@/contexts/Sdk", () => ({ useSdkClient: () => ({}) }));

vi.mock("@/components/ui/Select", () => ({
  Select: ({
    value,
    onValueChange,
    children,
  }: {
    value: string;
    onValueChange: (value: string) => void;
    children: ReactNode;
  }) => (
    <select
      aria-label="Environment"
      value={value}
      onChange={(event) => onValueChange(event.target.value)}
    >
      {children}
    </select>
  ),
  SelectTrigger: () => null,
  SelectValue: () => null,
  SelectContent: ({ children }: { children: ReactNode }) => <>{children}</>,
  SelectItem: ({ value, children }: { value: string; children: ReactNode }) => (
    <option value={value}>{children}</option>
  ),
}));

const prod = { id: "env-prod", name: "Prod", slug: "prod" };
const sandbox = { id: "env-sandbox", name: "Sandbox", slug: "sandbox" };

function preview(
  request: GetMcpServerEnvironmentHeadersRequest,
): McpServerEnvironmentHeaders {
  const base = { environments: [prod, sandbox], entries: [] };
  if (request.selection !== "environment") {
    return {
      ...base,
      environmentStatus: "none",
      environmentConfigurationInvalid: false,
    };
  }
  if (request.environmentId === sandbox.id) {
    return {
      ...base,
      environment: sandbox,
      environmentStatus: "ok",
      environmentConfigurationInvalid: true,
      entries: [
        {
          entryName: "MCP_HEADER_Gram-Key",
          headerName: "Gram-Key",
          status: "reserved",
        },
      ],
    };
  }
  return {
    ...base,
    environment: prod,
    environmentStatus: "ok",
    environmentConfigurationInvalid: false,
    entries: [
      {
        entryName: "MCP_HEADER_X-Prod",
        headerName: "X-Prod",
        status: "mapped",
      },
    ],
  };
}

function server(environmentId: string | undefined): McpServer {
  return {
    id: "server-1",
    projectId: PROJECT,
    name: "jamf",
    tunneledMcpServerId: "tunnel-1",
    environmentId,
    visibility: "private",
  } as McpServer;
}

function ui(client: QueryClient, mcpServer: McpServer): JSX.Element {
  return (
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <TooltipProvider>
          <EnvironmentHeadersSection key={mcpServer.id} mcpServer={mcpServer} />
        </TooltipProvider>
      </MemoryRouter>
    </QueryClientProvider>
  );
}

function newClient(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

beforeEach(() => {
  mocks.preview.mockImplementation(preview);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("EnvironmentHeadersSection preview cache", () => {
  it("shows the new environment's preview and validity after a relink elsewhere", async () => {
    const client = newClient();
    const view = render(ui(client, server(prod.id)));
    await waitFor(() =>
      expect(screen.getByText("MCP_HEADER_X-Prod")).toBeTruthy(),
    );
    expect(screen.queryByText(/refused/i)).toBeNull();

    view.rerender(ui(client, server(sandbox.id)));
    await waitFor(() =>
      expect(screen.getByText("MCP_HEADER_Gram-Key")).toBeTruthy(),
    );
    expect(screen.queryByText("MCP_HEADER_X-Prod")).toBeNull();
    expect(
      screen.getByText(/requests to this server are refused while/i),
    ).toBeTruthy();
  });

  it("drops the old environment's preview after an unlink elsewhere", async () => {
    const client = newClient();
    const view = render(ui(client, server(sandbox.id)));
    await waitFor(() =>
      expect(screen.getByText("MCP_HEADER_Gram-Key")).toBeTruthy(),
    );

    view.rerender(ui(client, server(undefined)));
    await waitFor(() =>
      expect(screen.queryByText("MCP_HEADER_Gram-Key")).toBeNull(),
    );
    expect(screen.queryByText(/refused/i)).toBeNull();
  });

  it("invalidates the cached upstream tool listing after a save", async () => {
    const client = newClient();
    const toolsKey = [
      "proxiedMcpTools",
      "https://upstream.example.invalid/mcp/fixture",
      [],
    ];
    client.setQueryData(toolsKey, { prodOnlyTool: {} });
    render(ui(client, server(prod.id)));
    await waitFor(() => expect(mocks.saveSuccess).toBeDefined());

    await mocks.saveSuccess?.();
    expect(client.getQueryState(toolsKey)?.isInvalidated).toBe(true);
  });
});
