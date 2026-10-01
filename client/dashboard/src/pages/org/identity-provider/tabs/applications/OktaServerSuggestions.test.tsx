import { afterEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TooltipProvider } from "@/components/ui/Tooltip";
import type { OktaServerSuggestion } from "@gram/client/models/components/oktaserversuggestion.js";
import { OktaServerSuggestions } from "./OktaServerSuggestions";
import { suggestionToCatalogServer } from "./suggestionToCatalogServer";

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  dismiss: vi.fn(),
  restore: vi.fn(),
  dialog: vi.fn(),
  projects: [{ id: "p1", slug: "default", name: "Default" }],
}));
vi.mock("@gram/client/react-query/oktaServerSuggestions.js", () => ({
  useOktaServerSuggestions: mocks.list,
  invalidateAllOktaServerSuggestions: () => Promise.resolve(),
}));
vi.mock("@gram/client/react-query/dismissOktaServerSuggestion.js", () => ({
  useDismissOktaServerSuggestionMutation: () => ({
    isPending: false,
    mutate: mocks.dismiss,
  }),
}));
vi.mock("@gram/client/react-query/restoreOktaServerSuggestion.js", () => ({
  useRestoreOktaServerSuggestionMutation: () => ({
    isPending: false,
    mutate: mocks.restore,
  }),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org", slug: "org", projects: mocks.projects }),
}));
vi.mock("@/pages/catalog/AddServerDialog", () => ({
  AddServerDialog: (props: unknown) => {
    mocks.dialog(props);
    return <div data-testid="add-server-dialog" />;
  },
}));
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  mocks.projects.splice(1);
});

function suggestion(
  overrides: Partial<OktaServerSuggestion> = {},
): OktaServerSuggestion {
  return {
    registryEntryId: "entry-1",
    serverName: "com.example/mcp",
    title: "Example",
    description: "Example MCP server.",
    documentationUrl: "https://example.com/docs",
    remotes: [
      {
        type: "streamable-http",
        url: "https://mcp.example.com/mcp",
        headers: [
          {
            name: "Authorization",
            description: "Bearer token",
            isRequired: true,
            isSecret: true,
          },
        ],
      },
    ],
    oktaApplications: [
      {
        oktaAppId: "app-1",
        label: "Example SSO",
        name: "example",
        signOnMode: "SAML_2_0",
        xaaSupported: true,
        userAssignments: 4,
        groupAssignments: 1,
      },
    ],
    state: "open",
    installedUrls: [],
    ...overrides,
  };
}

function show(suggestions: OktaServerSuggestion[]) {
  mocks.list.mockReturnValue({
    data: {
      suggestions,
      openCount: suggestions.filter((s) => s.state === "open").length,
      totalCount: suggestions.length,
      snapshotAt: new Date("2026-01-01T00:00:00Z"),
    },
    isPending: false,
    isError: false,
  });
  render(
    <QueryClientProvider client={new QueryClient()}>
      <TooltipProvider>
        <OktaServerSuggestions />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

it("opens the install dialog for the suggested server with the project", () => {
  show([suggestion()]);
  expect(screen.getByText("Example SSO")).toBeTruthy();
  expect(screen.queryByTestId("add-server-dialog")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Add server" }));
  expect(screen.getByTestId("add-server-dialog")).toBeTruthy();
  const props = mocks.dialog.mock.calls[0]?.[0] as {
    servers: { registrySpecifier: string; remotes: { url: string }[] }[];
    projectSlug: string;
  };
  expect(props.projectSlug).toBe("default");
  expect(props.servers[0]?.registrySpecifier).toBe("com.example/mcp");
  expect(props.servers[0]?.remotes[0]?.url).toBe("https://mcp.example.com/mcp");
});

it("dismisses and restores by registry entry id", () => {
  show([
    suggestion(),
    suggestion({
      registryEntryId: "entry-2",
      serverName: "com.other/mcp",
      title: "Other",
      state: "dismissed",
      dismissedAt: new Date("2026-01-02T00:00:00Z"),
    }),
  ]);
  fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
  expect(mocks.dismiss).toHaveBeenCalledWith(
    expect.objectContaining({
      request: {
        dismissOktaServerSuggestionRequestBody: { registryEntryId: "entry-1" },
      },
    }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Restore" }));
  expect(mocks.restore).toHaveBeenCalledWith(
    expect.objectContaining({
      request: {
        restoreOktaServerSuggestionRequestBody: { registryEntryId: "entry-2" },
      },
    }),
  );
});

it("offers no actions for an installed suggestion", () => {
  show([suggestion({ state: "installed" })]);
  expect(screen.getByText("Installed")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Add server" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Dismiss" })).toBeNull();
});

it("requests every suggestion when the filter switches to All", () => {
  show([]);
  expect(mocks.list.mock.calls[0]?.[0]).toEqual({ includeAll: false });
  expect(screen.getByText("No new suggestions")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "All" }));
  expect(mocks.list.mock.lastCall?.[0]).toEqual({ includeAll: true });
});

it("shows a project picker only when the organization has several projects", () => {
  show([suggestion()]);
  expect(
    screen.queryByRole("combobox", { name: "Project to add servers to" }),
  ).toBeNull();
  cleanup();
  mocks.projects.push({ id: "p2", slug: "second", name: "Second" });
  show([suggestion()]);
  expect(
    screen.getByRole("combobox", { name: "Project to add servers to" }),
  ).toBeTruthy();
});

it("maps SSE remotes and headers onto the catalog server", () => {
  const server = suggestionToCatalogServer(
    suggestion({
      remotes: [
        { type: "sse", url: "https://mcp.example.com/sse", headers: [] },
      ],
    }),
  );
  expect(server.remotes?.[0]?.transportType).toBe("sse");
  expect(server.title).toBe("Example");
  expect(server.registryId).toBeUndefined();
});
