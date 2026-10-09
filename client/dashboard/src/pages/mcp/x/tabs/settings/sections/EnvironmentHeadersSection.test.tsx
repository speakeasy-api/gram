import { TooltipProvider } from "@/components/ui/Tooltip";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { McpServerEnvironmentHeaders } from "@gram/client/models/components/mcpserverenvironmentheaders.js";
import type { GetMcpServerEnvironmentHeadersRequest } from "@gram/client/models/operations/getmcpserverenvironmentheaders.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { EnvironmentHeadersSection } from "./EnvironmentHeadersSection";

const PROJECT = "project-1";
const SYNTHETIC_VALUE = "synthetic-environment-value";

const mocks = vi.hoisted(() => ({
  scopes: new Set<string>(),
  previewRequests: [] as GetMcpServerEnvironmentHeadersRequest[],
  preview: vi.fn<
    (request: GetMcpServerEnvironmentHeadersRequest) => {
      data?: unknown;
      isError: boolean;
    }
  >(),
  mutate: vi.fn(),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string, resourceId?: string, projectId?: string) =>
      mocks.scopes.has(`${scope}|${resourceId}|${projectId}`),
    hasAnyScope: (scopes: string[], resourceId?: string, projectId?: string) =>
      scopes.some((scope) =>
        mocks.scopes.has(`${scope}|${resourceId}|${projectId}`),
      ),
    hasAllScopes: (scopes: string[], resourceId?: string, projectId?: string) =>
      scopes.every((scope) =>
        mocks.scopes.has(`${scope}|${resourceId}|${projectId}`),
      ),
    isLoading: false,
  }),
}));

vi.mock("@gram/client/react-query/getMcpServerEnvironmentHeaders.js", () => ({
  useGetMcpServerEnvironmentHeaders: (
    request: GetMcpServerEnvironmentHeadersRequest,
  ) => {
    mocks.previewRequests.push(request);
    return mocks.preview(request);
  },
  invalidateAllGetMcpServerEnvironmentHeaders: vi.fn(),
}));

vi.mock("@gram/client/react-query/updateMcpServer.js", () => ({
  useUpdateMcpServerMutation: () => ({
    mutate: mocks.mutate,
    isPending: false,
  }),
}));

// A native select stands in for the Radix one so tests can pick a value.
vi.mock("@/components/ui/Select", () => ({
  Select: ({
    value,
    onValueChange,
    disabled,
    children,
  }: {
    value: string;
    onValueChange: (value: string) => void;
    disabled?: boolean;
    children: ReactNode;
  }) => (
    <select
      aria-label="Environment"
      value={value}
      disabled={disabled}
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

function result(
  overrides: Partial<McpServerEnvironmentHeaders> = {},
): McpServerEnvironmentHeaders {
  return {
    environmentStatus: "none",
    environmentConfigurationInvalid: false,
    entries: [],
    environments: [prod, sandbox],
    ...overrides,
  };
}

function server(overrides: Partial<McpServer> = {}): McpServer {
  return {
    id: "server-1",
    projectId: PROJECT,
    name: "jamf-prod",
    tunneledMcpServerId: "tunnel-1",
    environmentId: prod.id,
    visibility: "private",
    toolVariationsGroupId: "group-1",
    ...overrides,
  } as McpServer;
}

function grantAll(serverId = "server-1"): void {
  mocks.scopes.add(`environment:read|${PROJECT}|${PROJECT}`);
  mocks.scopes.add(`mcp:write|${serverId}|${PROJECT}`);
}

function renderSection(mcpServer: McpServer): ReturnType<typeof render> {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>
        <TooltipProvider>
          <EnvironmentHeadersSection key={mcpServer.id} mcpServer={mcpServer} />
        </TooltipProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  mocks.scopes.clear();
  mocks.previewRequests = [];
  mocks.preview.mockImplementation((request) => {
    if (request.selection === "none") return { data: result(), isError: false };
    return {
      data: result({
        environment: prod,
        environmentStatus: "ok",
        entries: [
          {
            entryName: "MCP_HEADER_X-Instance-Url",
            headerName: "X-Instance-Url",
            status: "overrides_source",
          },
          {
            entryName: "MCP_HEADER_Gram-Key",
            headerName: "Gram-Key",
            status: "reserved",
          },
          { entryName: "mcp_header_X-Typo", status: "not_mapped" },
        ],
        environmentConfigurationInvalid: true,
      }),
      isError: false,
    };
  });
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("EnvironmentHeadersSection", () => {
  it("asks for project-wide environment access before loading anything", () => {
    mocks.scopes.add(`mcp:write|server-1|${PROJECT}`);
    renderSection(server());

    expect(
      screen.getByText(/requires read access to every environment/i),
    ).toBeTruthy();
    expect(mocks.previewRequests).toHaveLength(0);
  });

  it("previews the linked environment by name and status only", () => {
    grantAll();
    renderSection(server());

    expect(mocks.previewRequests).toContainEqual({
      id: "server-1",
      selection: "linked",
    });
    expect(screen.getByText("MCP_HEADER_X-Instance-Url")).toBeTruthy();
    expect(screen.getByText("Overrides source header")).toBeTruthy();
    expect(screen.getByText("Reserved header")).toBeTruthy();
    expect(screen.getByText("Not used")).toBeTruthy();
    expect(
      screen.getByText(/requests to this server are refused/i),
    ).toBeTruthy();
    expect(document.body.textContent).not.toContain(SYNTHETIC_VALUE);
    expect(screen.queryByRole("button", { name: /save/i })).toBeNull();
  });

  it("previews a picked candidate and saves the link with every other field", () => {
    grantAll();
    renderSection(server());

    fireEvent.change(screen.getByLabelText("Environment"), {
      target: { value: sandbox.id },
    });
    expect(mocks.previewRequests.at(-1)).toEqual({
      id: "server-1",
      selection: "environment",
      environmentId: sandbox.id,
    });

    fireEvent.click(screen.getByRole("button", { name: /save/i }));
    expect(mocks.mutate).toHaveBeenCalledWith({
      request: {
        updateMcpServerForm: {
          id: "server-1",
          name: "jamf-prod",
          remoteMcpServerId: undefined,
          tunneledMcpServerId: "tunnel-1",
          toolsetId: undefined,
          unproxiedMcpServerId: undefined,
          toolVariationsGroupId: "group-1",
          environmentId: sandbox.id,
          visibility: "private",
        },
      },
    });
  });

  it("previews None and saves it as an unlink, even from a deleted environment", () => {
    grantAll();
    mocks.preview.mockImplementation((request) => {
      if (request.selection === "linked") {
        return {
          data: result({
            environmentStatus: "unavailable",
            environmentConfigurationInvalid: true,
          }),
          isError: false,
        };
      }
      return { data: result(), isError: false };
    });
    renderSection(server({ environmentId: "env-deleted" }));

    expect(screen.getByText(/deleted or unavailable/i)).toBeTruthy();
    const select = screen.getByLabelText("Environment");
    expect(
      within(select).getByRole("option", { name: "Unavailable environment" }),
    ).toBeTruthy();

    fireEvent.change(select, { target: { value: "__none__" } });
    expect(mocks.previewRequests.at(-1)).toEqual({
      id: "server-1",
      selection: "none",
    });
    expect(screen.queryByText(/deleted or unavailable/i)).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: /save/i }));
    expect(
      mocks.mutate.mock.calls[0]?.[0].request.updateMcpServerForm,
    ).toMatchObject({ id: "server-1", environmentId: undefined });
  });

  it("disables saving when the preview fails to load", () => {
    grantAll();
    mocks.preview.mockImplementation((request) =>
      request.selection === "none"
        ? { data: result(), isError: false }
        : { data: undefined, isError: true },
    );
    renderSection(server());

    fireEvent.change(screen.getByLabelText("Environment"), {
      target: { value: sandbox.id },
    });
    expect(screen.getByText(/could not load/i)).toBeTruthy();
    expect(
      (screen.getByRole("button", { name: /save/i }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
  });

  it("cannot change the link without write access to this server", () => {
    mocks.scopes.add(`environment:read|${PROJECT}|${PROJECT}`);
    mocks.scopes.add(`mcp:write|other-server|${PROJECT}`);
    renderSection(server());

    expect(
      (screen.getByLabelText("Environment") as HTMLSelectElement).disabled,
    ).toBe(true);
  });

  it("does not carry a draft to another server on the same tunnel", () => {
    grantAll("server-1");
    grantAll("server-2");
    const { rerender } = renderSection(server());

    fireEvent.change(screen.getByLabelText("Environment"), {
      target: { value: sandbox.id },
    });
    expect(screen.getByRole("button", { name: /save/i })).toBeTruthy();

    const sibling = server({
      id: "server-2",
      name: "jamf-sandbox",
      environmentId: prod.id,
    });
    rerender(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter>
          <TooltipProvider>
            <EnvironmentHeadersSection key={sibling.id} mcpServer={sibling} />
          </TooltipProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    );

    expect(
      (screen.getByLabelText("Environment") as HTMLSelectElement).value,
    ).toBe(prod.id);
    expect(screen.queryByRole("button", { name: /save/i })).toBeNull();
  });

  it("describes an invalid unsaved selection as what saving would do", () => {
    grantAll();
    mocks.preview.mockImplementation((request) => {
      if (request.selection === "none")
        return { data: result(), isError: false };
      if (request.selection === "linked") {
        return {
          data: result({
            environment: prod,
            environmentStatus: "ok",
            entries: [
              {
                entryName: "MCP_HEADER_X-Ok",
                headerName: "X-Ok",
                status: "mapped",
              },
            ],
          }),
          isError: false,
        };
      }
      return {
        data: result({
          environment: sandbox,
          environmentStatus: "ok",
          environmentConfigurationInvalid: true,
          entries: [
            {
              entryName: "MCP_HEADER_Gram-Key",
              headerName: "Gram-Key",
              status: "reserved",
            },
            {
              entryName: "MCP_HEADER_X-Ok",
              headerName: "X-Ok",
              status: "mapped",
            },
          ],
        }),
        isError: false,
      };
    });
    renderSection(server());
    expect(screen.queryByText(/refused/i)).toBeNull();
    expect(screen.getByText("Mapped")).toBeTruthy();

    fireEvent.change(screen.getByLabelText("Environment"), {
      target: { value: sandbox.id },
    });
    expect(
      screen.getByText(/would be refused if you save this environment/i),
    ).toBeTruthy();
    expect(
      screen.queryByText(/requests to this server are refused while/i),
    ).toBeNull();
    expect(screen.queryByText("Sent")).toBeNull();
  });

  function rerenderWith(
    view: ReturnType<typeof render>,
    mcpServer: McpServer,
  ): void {
    view.rerender(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter>
          <TooltipProvider>
            <EnvironmentHeadersSection
              key={mcpServer.id}
              mcpServer={mcpServer}
            />
          </TooltipProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    );
  }

  it("follows a link changed elsewhere while the picker is untouched", () => {
    grantAll();
    const view = renderSection(server());

    rerenderWith(view, server({ environmentId: sandbox.id }));
    expect(
      (screen.getByLabelText("Environment") as HTMLSelectElement).value,
    ).toBe(sandbox.id);
    expect(screen.queryByRole("button", { name: /save/i })).toBeNull();

    rerenderWith(view, server({ environmentId: undefined }));
    expect(
      (screen.getByLabelText("Environment") as HTMLSelectElement).value,
    ).toBe("__none__");
    expect(screen.queryByRole("button", { name: /save/i })).toBeNull();
    expect(screen.queryByText(/changed elsewhere/i)).toBeNull();
  });

  it("keeps an edited selection and flags a link changed elsewhere", () => {
    grantAll();
    const view = renderSection(server());
    fireEvent.change(screen.getByLabelText("Environment"), {
      target: { value: "__none__" },
    });

    rerenderWith(view, server({ environmentId: sandbox.id }));
    expect(
      (screen.getByLabelText("Environment") as HTMLSelectElement).value,
    ).toBe("__none__");
    expect(screen.getByText(/changed elsewhere/i)).toBeTruthy();
  });

  it("treats its own saved link as the new baseline", () => {
    grantAll();
    const view = renderSection(server());

    fireEvent.change(screen.getByLabelText("Environment"), {
      target: { value: sandbox.id },
    });
    fireEvent.click(screen.getByRole("button", { name: /save/i }));
    rerenderWith(view, server({ environmentId: sandbox.id }));
    expect(screen.queryByText(/changed elsewhere/i)).toBeNull();
    expect(screen.queryByRole("button", { name: /save/i })).toBeNull();

    // A later change elsewhere is followed, not undone by a stale draft.
    rerenderWith(view, server({ environmentId: undefined }));
    expect(
      (screen.getByLabelText("Environment") as HTMLSelectElement).value,
    ).toBe("__none__");
    expect(screen.queryByRole("button", { name: /save/i })).toBeNull();
    expect(screen.queryByText(/changed elsewhere/i)).toBeNull();
  });

  it("treats a saved unlink as the new baseline", () => {
    grantAll();
    const view = renderSection(server());

    fireEvent.change(screen.getByLabelText("Environment"), {
      target: { value: "__none__" },
    });
    fireEvent.click(screen.getByRole("button", { name: /save/i }));
    rerenderWith(view, server({ environmentId: undefined }));
    expect(screen.queryByText(/changed elsewhere/i)).toBeNull();
    expect(screen.queryByRole("button", { name: /save/i })).toBeNull();

    rerenderWith(view, server({ environmentId: sandbox.id }));
    expect(
      (screen.getByLabelText("Environment") as HTMLSelectElement).value,
    ).toBe(sandbox.id);
    expect(screen.queryByRole("button", { name: /save/i })).toBeNull();
  });
});
