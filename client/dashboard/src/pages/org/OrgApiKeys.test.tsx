import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import OrgApiKeys from "./OrgApiKeys";
import { createKeyFormToJSON } from "@gram/client/models/components/createkeyform.js";

type ListRequest = { limit?: number; cursor?: string; search?: string };
type ListPage = { keys: object[]; nextCursor?: string };

const mocks = vi.hoisted(() => ({
  organization: {
    id: "org_test",
    projects: [
      {
        id: "11111111-1111-4111-8111-111111111111",
        name: "Test project",
        slug: "test-project",
      },
    ],
  },
  mutate: vi.fn(),
  pending: false,
  onCreated: async (_key: object) => {},
  keys: [] as object[],
  // The fake keys.list: by default one page holding mocks.keys. Tests that
  // page or search replace it.
  listKeys: vi.fn<(request: ListRequest) => Promise<ListPage>>(),
  revoke: vi.fn(),
  admin: true,
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => mocks.organization,
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) =>
    mocks.admin ? children : null,
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({ keys: { list: mocks.listKeys } }),
}));
// Mirrors the template's loading, empty and toolbar branches. The real search
// box debounces; this one reports each change at once.
vi.mock("@/components/page-templates", () => ({
  ResourceListPage: ({
    children,
    primaryAction,
    search,
    isLoading,
    isEmpty,
    empty,
  }: {
    children: ReactNode;
    primaryAction: ReactNode;
    search: { value: string; onChange: (value: string) => void };
    isLoading?: boolean;
    isEmpty?: boolean;
    empty: { heading: string };
  }) => (
    <>
      {primaryAction}
      {isLoading ? (
        <div>Loading keys</div>
      ) : isEmpty ? (
        <div>{empty.heading}</div>
      ) : (
        <>
          <input
            aria-label="Search API keys"
            value={search.value}
            onChange={(e) => search.onChange(e.target.value)}
          />
          {children}
        </>
      )}
    </>
  ),
}));
vi.mock("@gram/client/react-query/createAPIKey", () => ({
  useCreateAPIKeyMutation: (options: {
    onSuccess: (key: object) => Promise<void>;
  }) => {
    mocks.onCreated = options.onSuccess;
    return { mutate: mocks.mutate, isPending: mocks.pending };
  },
}));
vi.mock("@gram/client/react-query/revokeAPIKey", () => ({
  useRevokeAPIKeyMutation: (options: { onSuccess: () => Promise<void> }) => ({
    mutate: (variables: unknown) => {
      mocks.revoke(variables);
      void options.onSuccess();
    },
    isPending: false,
  }),
}));

let queryClient: QueryClient;
function Providers({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
}
function renderPage() {
  return render(<OrgApiKeys />, { wrapper: Providers });
}

function apiKey(id: string, name: string) {
  return {
    id,
    name,
    keyPrefix: "test-prefix",
    scopes: ["consumer"],
    createdAt: new Date(),
  };
}

beforeEach(() => {
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  mocks.mutate.mockClear();
  mocks.revoke.mockClear();
  mocks.listKeys.mockReset();
  mocks.listKeys.mockImplementation(async () => ({ keys: mocks.keys }));
  mocks.pending = false;
  mocks.keys = [];
  mocks.admin = true;
  mocks.organization = {
    id: "org_test",
    projects: [
      {
        id: "11111111-1111-4111-8111-111111111111",
        name: "Test project",
        slug: "test-project",
      },
    ],
  };
  // Radix scrolls the selected option into view in browsers.
  Element.prototype.scrollIntoView = vi.fn<() => void>();
});
afterEach(() => {
  cleanup();
  queryClient.clear();
});

async function openForm() {
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "New API Key" }));
  await user.type(screen.getByLabelText("Key name"), "Test key");
  return user;
}
async function selectProject(
  user: ReturnType<typeof userEvent.setup>,
  name: string,
) {
  await user.click(screen.getByRole("combobox", { name: "Project" }));
  await user.click(screen.getByRole("option", { name }));
}

describe("API key project binding", () => {
  it("defaults to organization-wide and preserves permission scope", async () => {
    renderPage();
    const user = await openForm();
    await user.click(screen.getByRole("radio", { name: "Producer" }));
    await user.click(screen.getByRole("button", { name: "Create" }));
    expect(
      JSON.parse(
        createKeyFormToJSON(
          mocks.mutate.mock.calls[0]?.[0].request.createKeyForm,
        ),
      ),
    ).toEqual({ name: "Test key", scopes: ["producer"] });
    expect(mocks.mutate.mock.calls[0]?.[0]).toEqual({
      security: { sessionHeaderGramSession: "" },
      request: {
        createKeyForm: {
          name: "Test key",
          scopes: ["producer"],
          projectId: undefined,
        },
      },
    });
  });

  it("sends the selected project ID in the body, not a project header", async () => {
    renderPage();
    const user = await openForm();
    await selectProject(user, "Test project");
    await user.click(screen.getByRole("button", { name: "Create" }));
    expect(
      JSON.parse(
        createKeyFormToJSON(
          mocks.mutate.mock.calls[0]?.[0].request.createKeyForm,
        ),
      ),
    ).toEqual({
      name: "Test key",
      scopes: ["consumer"],
      project_id: mocks.organization.projects[0]!.id,
    });
    expect(mocks.mutate.mock.calls[0]?.[0].request).toEqual({
      createKeyForm: {
        name: "Test key",
        scopes: ["consumer"],
        projectId: mocks.organization.projects[0]!.id,
      },
    });
  });

  it("can explicitly switch back to organization-wide", async () => {
    renderPage();
    const user = await openForm();
    await selectProject(user, "Test project");
    await selectProject(user, "Organization-wide");
    await user.click(screen.getByRole("button", { name: "Create" }));
    expect(
      mocks.mutate.mock.calls[0]?.[0].request.createKeyForm.projectId,
    ).toBeUndefined();
  });

  it("clears selection on cancel and organization changes", async () => {
    const view = renderPage();
    const user = await openForm();
    await selectProject(user, "Test project");
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    await openForm();
    expect(screen.getByRole("combobox").textContent).toContain(
      "Organization-wide",
    );
    await selectProject(user, "Test project");
    mocks.organization = { id: "org_other", projects: [] };
    view.rerender(<OrgApiKeys />);
    expect(screen.queryByRole("dialog")).toBeNull();
    await openForm();
    expect(screen.getByRole("combobox").textContent).toContain(
      "Organization-wide",
    );
    await user.click(screen.getByRole("button", { name: "Create" }));
    expect(
      mocks.mutate.mock.calls[0]?.[0].request.createKeyForm.projectId,
    ).toBeUndefined();
  });

  it("lists projects alphabetically, not in the API's creation order", async () => {
    mocks.organization = {
      id: "org_test",
      projects: [
        { id: "p3", name: "zeta", slug: "zeta" },
        { id: "p1", name: "Alpha", slug: "alpha" },
        { id: "p2", name: "middle", slug: "middle" },
      ],
    };
    renderPage();
    const user = await openForm();
    await user.click(screen.getByRole("combobox", { name: "Project" }));
    expect(
      screen.getAllByRole("option").map((option) => option.textContent),
    ).toEqual(["Organization-wide", "Alpha", "middle", "zeta"]);
  });

  it("does not silently broaden a no-longer-authorized project selection", async () => {
    const view = renderPage();
    const user = await openForm();
    await selectProject(user, "Test project");
    mocks.organization = { ...mocks.organization, projects: [] };
    view.rerender(<OrgApiKeys />);
    expect(
      (
        screen.getByRole("button", {
          name: "Create",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    fireEvent.submit(screen.getByLabelText("Key name").closest("form")!);
    expect(mocks.mutate).not.toHaveBeenCalled();
    expect(screen.getByRole("combobox", { name: "Project" }).textContent).toBe(
      "Unavailable project",
    );
    expect(
      screen.getByText(
        "This project is no longer available. Select a project or Organization-wide.",
      ),
    ).toBeTruthy();
    await selectProject(user, "Organization-wide");
    await user.click(screen.getByRole("button", { name: "Create" }));
    expect(
      mocks.mutate.mock.calls[0]?.[0].request.createKeyForm.projectId,
    ).toBeUndefined();
  });

  it("blocks duplicate submissions while creation is pending", async () => {
    const view = renderPage();
    await openForm();
    mocks.pending = true;
    view.rerender(<OrgApiKeys />);
    fireEvent.submit(screen.getByLabelText("Key name").closest("form")!);
    expect(mocks.mutate).not.toHaveBeenCalled();
  });

  it("does not show an old organization's delayed creation result", async () => {
    const view = renderPage();
    const user = await openForm();
    await selectProject(user, "Test project");
    await user.click(screen.getByRole("button", { name: "Create" }));
    const oldOnCreated = mocks.onCreated;
    mocks.organization = { id: "org_other", projects: [] };
    view.rerender(<OrgApiKeys />);
    await openForm();
    await act(() =>
      oldOnCreated({
        id: "old_key",
        name: "Old key",
        key: "synthetic-old-secret",
        projectId: "11111111-1111-4111-8111-111111111111",
      }),
    );
    expect(screen.queryByText("synthetic-old-secret")).toBeNull();
    expect(screen.getByRole("combobox").textContent).toContain(
      "Organization-wide",
    );
  });

  it("shows binding separately from permission scopes in the list", async () => {
    mocks.keys = [
      {
        id: "key_test",
        name: "Bound key",
        keyPrefix: "test-prefix",
        scopes: ["consumer"],
        projectId: mocks.organization.projects[0]!.id,
        createdAt: new Date(),
      },
      {
        id: "key_org",
        name: "Org key",
        keyPrefix: "test-prefix",
        scopes: ["consumer"],
        createdAt: new Date(),
      },
    ];
    renderPage();
    expect(await screen.findByText("Project binding")).toBeTruthy();
    expect(screen.getByText("Test project")).toBeTruthy();
    expect(screen.getByText("Organization-wide")).toBeTruthy();
  });

  it("shows the returned binding with the one-time secret and clears both on close", async () => {
    renderPage();
    const user = await openForm();
    await act(() =>
      mocks.onCreated({
        id: "key_created",
        name: "Test key",
        key: "synthetic-test-secret",
        projectId: mocks.organization.projects[0]!.id,
      }),
    );
    expect(screen.getByText("Project binding: Test project")).toBeTruthy();
    expect(screen.getByText("synthetic-test-secret")).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "Done" }));
    await openForm();
    expect(screen.queryByText("synthetic-test-secret")).toBeNull();
    expect(screen.getByRole("combobox").textContent).toContain(
      "Organization-wide",
    );
  });

  it("retains the admin gate", () => {
    mocks.admin = false;
    renderPage();
    expect(screen.queryByRole("button", { name: "New API Key" })).toBeNull();
  });
});

describe("API key scope options", () => {
  it("offers every scope, grouped, with the narrowest one preselected", async () => {
    renderPage();
    await openForm();
    expect(
      screen
        .getAllByRole("radio", {
          name: /^(Consumer|Producer|Hooks|Agent)/,
        })
        .map((radio) => radio.getAttribute("value")),
    ).toEqual(["consumer", "producer", "hooks", "agent"]);
    // The chat scope is no longer provisioned, so it must not be offered.
    expect(screen.queryByRole("radio", { name: /^Chat/ })).toBeNull();
    expect(
      screen.getByRole("radio", { checked: true }).getAttribute("value"),
    ).toBe("consumer");
    expect(screen.getByText("Platform access")).toBeTruthy();
    expect(screen.getByText("Purpose-built keys")).toBeTruthy();
  });

  it("summarizes each scope in one line, with the access list behind a toggle", async () => {
    renderPage();
    await openForm();
    expect(
      screen.getByText(
        "For clients that call your MCP servers and tools at runtime.",
      ),
    ).toBeTruthy();
    expect(
      screen.getByText(
        "For automation that sets up and updates a project, such as CI.",
      ),
    ).toBeTruthy();
    expect(
      screen.queryByText("Call MCP servers and the tools they expose"),
    ).toBeNull();
    expect(
      screen.getByText(
        "A key's scope is fixed once it is created. Pick the narrowest scope that covers the job.",
      ),
    ).toBeTruthy();
  });

  it("reveals grants and exclusions on request without changing the selection", async () => {
    renderPage();
    const user = await openForm();
    await user.click(
      screen.getByRole("button", { name: "Access details for Producer" }),
    );
    expect(
      screen.getByText("Upload OpenAPI documents and trigger deployments"),
    ).toBeTruthy();
    expect(
      screen.getByText("Sending AI traffic in, enrolling device agents"),
    ).toBeTruthy();
    // producer implies chat in auth.effectiveScopes, so the card must not
    // understate itself now that the chat scope is no longer offered.
    expect(
      screen.getByText("Create chat sessions and run agent workflows"),
    ).toBeTruthy();
    expect(
      screen.getByRole("radio", { checked: true }).getAttribute("value"),
    ).toBe("consumer");
    await user.click(
      screen.getByRole("button", { name: "Access details for Producer" }),
    );
    expect(
      screen.queryByText("Upload OpenAPI documents and trigger deployments"),
    ).toBeNull();
  });

  it("keeps each scope's access details with its own card", async () => {
    renderPage();
    const user = await openForm();
    // Scoped to the owning card: a global text query would also pass if a
    // card rendered a neighbor's access list.
    const cardFor = (scope: string) => {
      const card = screen
        .getByRole("button", { name: `Access details for ${scope}` })
        .closest('[data-slot="radio-card"]');
      expect(card).toBeTruthy();
      return within(card as HTMLElement);
    };

    await user.click(
      screen.getByRole("button", { name: "Access details for Agent" }),
    );
    expect(
      cardFor("Agent").getByText(
        "Store it in managed.json as org_token, or have a developer run the enroll command from Device Agent setup.",
      ),
    ).toBeTruthy();
    expect(
      screen.queryByText("Send hook events, logs, metrics, and traces"),
    ).toBeNull();

    await user.click(
      screen.getByRole("button", { name: "Access details for Hooks" }),
    );
    expect(
      cardFor("Hooks").getByText("Send hook events, logs, metrics, and traces"),
    ).toBeTruthy();
    expect(
      cardFor("Agent").queryByText(
        "Send hook events, logs, metrics, and traces",
      ),
    ).toBeNull();
  });

  it("sends the scope value of the card the user picks", async () => {
    renderPage();
    const user = await openForm();
    await user.click(screen.getByRole("radio", { name: /^Hooks/ }));
    await user.click(screen.getByRole("button", { name: "Create" }));
    expect(
      mocks.mutate.mock.calls[0]?.[0].request.createKeyForm.scopes,
    ).toEqual(["hooks"]);
  });

  it("selects a scope when its card body is clicked, not just the radio", async () => {
    renderPage();
    const user = await openForm();
    await user.click(
      screen.getByText(
        "For rolling out the Speakeasy device agent across the organization.",
      ),
    );
    await user.click(screen.getByRole("button", { name: "Create" }));
    expect(
      mocks.mutate.mock.calls[0]?.[0].request.createKeyForm.scopes,
    ).toEqual(["agent"]);
  });
});

describe("API key list paging and search", () => {
  it("asks the server for one page of 50, newest first, without a search", async () => {
    mocks.keys = [apiKey("key_1", "First key")];
    renderPage();
    expect(await screen.findByText("First key")).toBeTruthy();
    expect(mocks.listKeys).toHaveBeenCalledTimes(1);
    expect(mocks.listKeys.mock.calls[0]?.[0]).toEqual({
      limit: 50,
      cursor: undefined,
      search: undefined,
    });
    expect(screen.queryByRole("button", { name: "Load more" })).toBeNull();
    expect(screen.getByText("1 key shown")).toBeTruthy();
  });

  it("loads the next page with the returned cursor and appends it", async () => {
    mocks.listKeys.mockImplementation(async (request) =>
      request.cursor === "cursor-2"
        ? { keys: [apiKey("key_3", "Third key")] }
        : {
            keys: [apiKey("key_1", "First key"), apiKey("key_2", "Second key")],
            nextCursor: "cursor-2",
          },
    );
    const user = userEvent.setup();
    renderPage();
    expect(await screen.findByText("Second key")).toBeTruthy();
    expect(screen.queryByText("Third key")).toBeNull();

    await user.click(screen.getByRole("button", { name: "Load more" }));
    expect(await screen.findByText("Third key")).toBeTruthy();
    expect(screen.getByText("First key")).toBeTruthy();
    expect(mocks.listKeys.mock.lastCall?.[0]).toEqual({
      limit: 50,
      cursor: "cursor-2",
      search: undefined,
    });
    expect(screen.getByText("3 keys shown")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Load more" })).toBeNull();
  });

  it("searches on the server instead of filtering loaded rows", async () => {
    mocks.listKeys.mockImplementation(async (request) =>
      request.search === "plugins"
        ? { keys: [apiKey("key_sys", "plugins-hooks-key")] }
        : { keys: [apiKey("key_1", "Deploy key")] },
    );
    const user = userEvent.setup();
    renderPage();
    expect(await screen.findByText("Deploy key")).toBeTruthy();

    await user.type(screen.getByLabelText("Search API keys"), " plugins ");
    // System keys come back from the server and are shown like any other.
    expect(await screen.findByText("plugins-hooks-key")).toBeTruthy();
    expect(screen.queryByText("Deploy key")).toBeNull();
    expect(mocks.listKeys.mock.lastCall?.[0]).toEqual({
      limit: 50,
      cursor: undefined,
      search: "plugins",
    });
  });

  it("keeps the search box when a search matches nothing", async () => {
    mocks.listKeys.mockImplementation(async (request) =>
      request.search ? { keys: [] } : { keys: [apiKey("key_1", "Deploy key")] },
    );
    const user = userEvent.setup();
    renderPage();
    expect(await screen.findByText("Deploy key")).toBeTruthy();

    await user.type(screen.getByLabelText("Search API keys"), "zzz");
    expect(await screen.findByText("No matching API keys")).toBeTruthy();
    expect(screen.getByLabelText("Search API keys")).toBeTruthy();
    expect(screen.queryByText("No API keys yet")).toBeNull();
  });

  it("shows the empty state only when the organization has no keys", async () => {
    renderPage();
    expect(await screen.findByText("No API keys yet")).toBeTruthy();
    expect(screen.queryByLabelText("Search API keys")).toBeNull();
  });

  it("reloads the list after a key is revoked", async () => {
    mocks.keys = [apiKey("key_1", "Doomed key")];
    const user = userEvent.setup();
    renderPage();
    expect(await screen.findByText("Doomed key")).toBeTruthy();
    expect(mocks.listKeys).toHaveBeenCalledTimes(1);

    mocks.keys = [];
    await user.click(screen.getByRole("button", { name: "Revoke API key" }));
    await user.click(screen.getByRole("button", { name: "Revoke Key" }));
    expect(mocks.revoke).toHaveBeenCalledWith({
      security: { sessionHeaderGramSession: "" },
      request: { id: "key_1" },
    });
    expect(await screen.findByText("No API keys yet")).toBeTruthy();
    expect(mocks.listKeys).toHaveBeenCalledTimes(2);
  });

  it("reloads the list after a key is created", async () => {
    mocks.keys = [apiKey("key_1", "Existing key")];
    renderPage();
    expect(await screen.findByText("Existing key")).toBeTruthy();
    expect(mocks.listKeys).toHaveBeenCalledTimes(1);

    mocks.keys = [apiKey("key_2", "New key"), apiKey("key_1", "Existing key")];
    await act(() =>
      mocks.onCreated({ id: "key_2", name: "New key", key: "synthetic-new" }),
    );
    expect(await screen.findByRole("cell", { name: "New key" })).toBeTruthy();
    expect(mocks.listKeys).toHaveBeenCalledTimes(2);
  });
});
