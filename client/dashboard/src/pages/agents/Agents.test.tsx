import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import AgentsPage from "./Agents";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
function setup() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  client.setQueryData(
    ["managed-agents", mocks.organizationId, "list", mocks.user.id],
    mocks.agents,
  );
  client.setQueryData(
    ["managed-agents", mocks.organizationId, "detail", "agent_example"],
    mocks.agents[0],
  );
  const seedOwners = () => {
    if (
      !mocks.ownersFetching &&
      !mocks.ownersError &&
      mocks.creationOwners.length > 0
    ) {
      client.setQueryData(ownerQueryKey(), {
        pages: [{ owners: mocks.owners, nextCursor: mocks.nextCursor }],
        pageParams: [undefined],
      });
    }
  };
  seedOwners();
  const view = render(
    <QueryClientProvider client={client}>
      <AgentsPage />
    </QueryClientProvider>,
  );
  return {
    ...view,
    client,
    rerenderPage: () => {
      seedOwners();
      view.rerender(
        <QueryClientProvider client={client}>
          <AgentsPage />
        </QueryClientProvider>,
      );
    },
  };
}
function ownerQueryKey() {
  return [
    "agent-eligible-owners",
    mocks.organizationId,
    mocks.user.id,
    undefined,
    mocks.creationOwners,
  ];
}
async function loadMoreOwners() {
  const button = screen.getByRole("button", { name: "Load more" });
  await waitFor(() => expect(button).toHaveProperty("disabled", false));
  fireEvent.click(button);
}
vi.mock("./AgentAPIKeys", () => ({
  AgentAPIKeys: ({
    creation,
    onCreate,
    onDone,
    onBusy,
  }: {
    creation?: boolean;
    onCreate?: () => void;
    onDone?: () => void;
    onBusy?: (busy: boolean) => void;
  }) =>
    creation ? (
      <>
        <button onClick={onDone}>Wizard done</button>
        <button onClick={() => onBusy?.(true)}>Start issuing</button>
        <button onClick={() => onBusy?.(false)}>Finish issuing</button>
      </>
    ) : (
      <div>
        API keys<button onClick={onCreate}>Create API key</button>
      </div>
    ),
}));
vi.mock("./ManagedAgentSessions", () => ({
  ManagedAgentSessions: () => <div>Sessions</div>,
}));

const mocks = vi.hoisted(() => ({
  sdkClient: vi.fn(),
  params: new URLSearchParams(),
  navigate: vi.fn(),
  list: vi.fn(),
  detail: vi.fn(),
  organizationId: "org_example",
  projectId: "00000000-0000-4000-8000-000000000001",
  createMutate: vi.fn(),
  creationOwners: ["user_owner"] as string[],
  grantsLoading: false,
  ownerQuery: vi.fn(),
  owners: [] as { userId: string; displayName: string }[],
  nextCursor: undefined as string | undefined,
  ownersFetching: false,
  ownersError: false,
  directoryRead: true,
  impersonatorEmail: undefined as string | undefined,
  user: {
    id: "user_owner",
    displayName: "Example Owner",
    email: "owner@example.test",
    photoUrl: "https://example.test/avatar.png",
  },
  unsupported: false,
  scopeOverride: null as string | null,
  agents: [
    {
      id: "agent_example",
      name: "Example agent",
      ownerUserId: "user_owner",
      ownerProfile: undefined as
        | { displayName: string; photoUrl?: string }
        | undefined,
      projectId: undefined as string | undefined,
      lifecycle: "active",
      permissions: { read: true, write: true, authorize: true, transfer: true },
    },
  ],
}));
vi.mock("@/components/ui/Avatar", () => ({
  Avatar: ({ children }: { children: ReactNode }) => <span>{children}</span>,
  AvatarImage: ({ src, alt }: { src?: string; alt: string }) => (
    <img src={src} alt={alt} />
  ),
  AvatarFallback: ({ children }: { children: ReactNode }) => (
    <span>{children}</span>
  ),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    id: mocks.organizationId,
    slug: "example",
    name: "Example Org",
  }),
  useProject: () => ({ id: mocks.projectId, slug: "alpha", name: "Alpha" }),
  useSession: () => ({
    user: mocks.user,
    organizationOverride: mocks.unsupported,
    impersonatorEmail: mocks.impersonatorEmail,
  }),
  useIsPlatformAdmin: () => false,
}));
vi.mock("@/components/dev-toolbar-utils", () => ({
  getRBACScopeOverrideHeader: () => mocks.scopeOverride,
}));
vi.mock("react-router", () => ({
  useSearchParams: () => [mocks.params, mocks.navigate],
}));
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: "enabled" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string) =>
      scope === "agent:create"
        ? mocks.creationOwners.length > 0
        : mocks.directoryRead,
    hasAnyScope: () => mocks.creationOwners.length > 0,
    hasAllScopes: () => mocks.creationOwners.length > 0,
    canCreateAgentFor: (id: string) => mocks.creationOwners.includes(id),
    grants: mocks.creationOwners,
    isLoading: mocks.grantsLoading,
  }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => mocks.sdkClient(),
}));
vi.mock("@gram/client/react-query/createAgent.js", () => ({
  useCreateAgentMutation: () => ({ mutate: mocks.createMutate }),
}));
vi.mock("@gram/client/react-query/renameAgent.js", () => ({
  useRenameAgentMutation: () => ({ mutate: vi.fn() }),
}));
vi.mock("@gram/client/react-query/agentsDelete.js", () => ({
  useAgentsDeleteMutation: () => ({ mutate: vi.fn() }),
}));
vi.mock("@gram/client/react-query/agentsResume.js", () => ({
  useAgentsResumeMutation: () => ({ mutate: vi.fn() }),
}));
vi.mock("@gram/client/react-query/agentsRevoke.js", () => ({
  useAgentsRevokeMutation: () => ({ mutate: vi.fn() }),
}));
vi.mock("@gram/client/react-query/agentsSuspend.js", () => ({
  useAgentsSuspendMutation: () => ({ mutate: vi.fn() }),
}));
vi.mock("@/components/page-templates", () => {
  const Part = ({ children }: { children: ReactNode }) => <div>{children}</div>;
  const Section = Object.assign(Part, {
    Header: Part,
    Title: Part,
    Description: Part,
    Panel: Part,
    Body: Part,
    Footer: Part,
  });
  const Page = ({
    children,
    title,
    description,
    stage,
    primaryAction,
    search,
    isEmpty = false,
    empty,
  }: {
    children: ReactNode;
    title: string;
    description: string;
    stage?: string;
    primaryAction: ReactNode;
    search?: {
      value: string;
      onChange: (value: string) => void;
      placeholder: string;
    };
    isEmpty?: boolean;
    empty?: { heading: string };
  }) => (
    <div>
      <h1>{title}</h1>
      {stage && <span>{stage === "preview" ? "Preview" : stage}</span>}
      <p>{description}</p>
      {primaryAction}
      {!isEmpty && search && (
        <input
          placeholder={search.placeholder}
          value={search.value}
          onChange={(event) => search.onChange(event.target.value)}
        />
      )}
      {isEmpty ? <h2>{empty?.heading}</h2> : children}
    </div>
  );
  return {
    ResourceListPage: Page,
    SettingsPage: Page,
    FormPage: Page,
    SettingsSection: Section,
    DangerSettingsSection: Section,
  };
});

afterEach(cleanup);
beforeEach(() => {
  mocks.params = new URLSearchParams();
  mocks.unsupported = false;
  mocks.organizationId = "org_example";
  mocks.projectId = "00000000-0000-4000-8000-000000000001";
  mocks.impersonatorEmail = undefined;
  mocks.scopeOverride = null;
  mocks.creationOwners = ["user_owner"];
  mocks.grantsLoading = false;
  mocks.ownersFetching = false;
  mocks.ownersError = false;
  mocks.nextCursor = undefined;
  mocks.owners = [
    { userId: "user_other", displayName: "Other Owner" },
    { userId: "user_denied", displayName: "Unpermitted Owner" },
  ];
  mocks.directoryRead = true;
  mocks.agents[0]!.ownerUserId = "user_owner";
  mocks.agents[0]!.ownerProfile = undefined;
  vi.clearAllMocks();
  mocks.sdkClient.mockReturnValue({
    agents: {
      list: mocks.list,
      get: mocks.detail,
      listEligibleOwners: mocks.ownerQuery,
    },
  });
  mocks.ownerQuery.mockImplementation(() => {
    if (mocks.ownersFetching) return new Promise(() => {});
    if (mocks.ownersError)
      return Promise.reject(new Error("Owner discovery failed"));
    return Promise.resolve({
      result: { owners: mocks.owners, nextCursor: mocks.nextCursor },
    });
  });
  mocks.list.mockImplementation(async () => mocks.agents);
  mocks.detail.mockResolvedValue(mocks.agents[0]);
});

describe("Agent owner access", () => {
  it("labels agent identity as preview", () => {
    setup();

    expect(screen.getByText("Preview")).toBeTruthy();
  });

  it("keeps search available after no results and restores agents when cleared", () => {
    setup();
    fireEvent.change(screen.getByPlaceholderText("Search agents"), {
      target: { value: "no-such-agent" },
    });
    expect(screen.getByText("No matching agents")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Example agent" })).toBeNull();
    fireEvent.change(screen.getByPlaceholderText("Search agents"), {
      target: { value: "" },
    });
    expect(screen.getByRole("button", { name: "Example agent" })).toBeTruthy();
    expect(screen.queryByText("No matching agents")).toBeNull();
  });
  it("renders agent detail sections without duplicate key warnings", () => {
    mocks.params = new URLSearchParams({ id: "agent_example" });
    const consoleError = vi.spyOn(console, "error");
    try {
      setup();
      expect(screen.getByText("Identity")).toBeTruthy();
      expect(screen.getByText("Sessions")).toBeTruthy();
      expect(
        consoleError.mock.calls.filter((args) =>
          args.some((arg) => String(arg).includes("same key")),
        ),
      ).toEqual([]);
    } finally {
      consoleError.mockRestore();
    }
  });
  it("uses the authorized owner profile rather than the viewer's profile", () => {
    mocks.agents[0]!.ownerUserId = "user_another";
    mocks.agents[0]!.ownerProfile = {
      displayName: "Another Owner",
      photoUrl: "https://example.test/another.png",
    };
    setup();
    expect(screen.getByText("Another Owner")).toBeTruthy();
    expect(document.querySelector("img")?.getAttribute("src")).toBe(
      "https://example.test/another.png",
    );
    expect(screen.queryByText("Example Owner")).toBeNull();
    expect(screen.queryByText("user_another")).toBeNull();
  });
  it("does not expose an unresolved owner's raw ID or substitute the viewer", () => {
    mocks.agents[0]!.ownerUserId = "user_unavailable";
    setup();
    expect(screen.getByText("Unavailable owner")).toBeTruthy();
    expect(screen.queryByText("user_unavailable")).toBeNull();
    expect(screen.queryByText("Example Owner")).toBeNull();
  });
  it("fetches and shows the authorized inventory without requiring an RBAC grant", () => {
    setup();
    expect(mocks.list).toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Example agent" })).toBeTruthy();
    expect(screen.getByText("Example Owner")).toBeTruthy();
    expect(screen.queryByText("user_owner")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Example agent" }));
    expect(mocks.navigate).toHaveBeenCalledWith({ id: "agent_example" });
  });
  it("provides an independent exit from a direct credential creation URL", () => {
    mocks.params = new URLSearchParams({
      id: "agent_example",
      credential: "new",
    });
    setup();
    fireEvent.click(screen.getByRole("button", { name: "Back to agent" }));
    expect(mocks.navigate).toHaveBeenCalledWith({ id: "agent_example" });
  });
  it("disables the creation page header while the wizard reports busy", () => {
    mocks.params = new URLSearchParams({
      id: "agent_example",
      credential: "new",
    });
    setup();
    fireEvent.click(screen.getByRole("button", { name: "Start issuing" }));
    expect(
      screen.getByRole("button", { name: "Back to agent" }),
    ).toHaveProperty("disabled", true);
    fireEvent.click(screen.getByRole("button", { name: "Back to agent" }));
    expect(mocks.navigate).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Finish issuing" }));
    expect(
      screen.getByRole("button", { name: "Back to agent" }),
    ).toHaveProperty("disabled", false);
  });
  it("keeps creation separate from the inventory", () => {
    setup();
    fireEvent.click(screen.getByRole("button", { name: "New agent identity" }));
    expect(mocks.navigate).toHaveBeenCalledWith({ create: "true" });
  });
  it("does not fetch agent data in an organization override session", () => {
    mocks.unsupported = true;
    setup();
    expect(mocks.list).not.toHaveBeenCalled();
    expect(screen.getByText("Agent management unavailable")).toBeTruthy();
  });
  it("does not fetch agent data with impersonatorEmail alone", () => {
    mocks.impersonatorEmail = "support@example.test";
    setup();
    expect(mocks.list).not.toHaveBeenCalled();
    expect(mocks.detail).not.toHaveBeenCalled();
    expect(screen.getByText("Agent management unavailable")).toBeTruthy();
  });
  it.each([false, true])(
    "isolates organization cache for detail=%s",
    (detail) => {
      if (detail) mocks.params = new URLSearchParams({ id: "agent_example" });
      const view = setup();
      mocks.organizationId = "org_other";
      mocks.list.mockReturnValue(new Promise(() => {}));
      mocks.detail.mockReturnValue(new Promise(() => {}));
      view.rerenderPage();
      expect(screen.queryByText("Example agent")).toBeNull();
      const query = view.client.getQueryCache().find({
        queryKey: ["managed-agents", "org_other", detail ? "detail" : "list"],
        exact: false,
      });
      expect(query?.state.data).toBeUndefined();
      expect(query?.queryHash).toContain("org_other");
    },
  );
  it("does not use ownership to bypass an explicit scope override", () => {
    mocks.scopeOverride = "agent:read";
    setup();
    expect(mocks.list).not.toHaveBeenCalled();
  });
  it("routes key creation to a dedicated page and returns to the agent", async () => {
    mocks.params = new URLSearchParams({ id: "agent_example" });
    const view = setup();
    fireEvent.click(
      await screen.findByRole("button", { name: "Create API key" }),
    );
    expect(mocks.navigate).toHaveBeenCalledWith({
      id: "agent_example",
      credential: "new",
    });
    mocks.params = new URLSearchParams({
      id: "agent_example",
      credential: "new",
    });
    view.rerenderPage();
    expect(
      screen.getByRole("heading", { name: "Create API key" }),
    ).toBeTruthy();
    expect(
      screen.getByText("Choose what Example agent can access."),
    ).toBeTruthy();
    expect(screen.queryByText("Identity")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Wizard done" }));
    expect(mocks.navigate).toHaveBeenLastCalledWith({ id: "agent_example" });
  });
});

describe("Agent scope", () => {
  function openCreateForm() {
    mocks.params = new URLSearchParams("create=true");
    setup();
    fireEvent.change(screen.getByLabelText("Agent name"), {
      target: { value: "Scoped agent" },
    });
  }

  it("defaults to project scope and sends the active project", () => {
    openCreateForm();
    expect(
      screen.getByRole("radio", { name: /Project/ }).getAttribute("data-state"),
    ).toBe("checked");

    fireEvent.click(screen.getByRole("button", { name: "Create agent" }));

    expect(mocks.createMutate).toHaveBeenCalledTimes(1);
    const form = mocks.createMutate.mock.calls[0]![0].request.createAgentForm;
    expect(form.name).toBe("Scoped agent");
    expect(form.projectId).toBe(mocks.projectId);
    expect(form.ownerUserId).toBe("user_owner");
  });

  it("sends no project binding when organization scope is chosen", () => {
    openCreateForm();
    fireEvent.click(screen.getByRole("radio", { name: /Organization/ }));
    fireEvent.click(screen.getByRole("button", { name: "Create agent" }));

    expect(mocks.createMutate).toHaveBeenCalledTimes(1);
    const form = mocks.createMutate.mock.calls[0]![0].request.createAgentForm;
    // Omitted rather than blank: the server reads a missing binding as
    // organization-wide.
    expect("projectId" in form).toBe(false);
  });
});

describe("Agent creation eligibility", () => {
  it("keeps owned agents visible when creation permission is removed", () => {
    mocks.creationOwners = [];
    setup();
    expect(screen.getByRole("button", { name: "Example agent" })).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "New agent identity" }),
    ).toBeNull();
  });
  it("blocks direct create URLs without creation eligibility", () => {
    mocks.creationOwners = [];
    mocks.params = new URLSearchParams("create=true");
    setup();
    fireEvent.change(screen.getByLabelText("Agent name"), {
      target: { value: "Denied" },
    });
    const button = screen.getByRole("button", { name: "Create agent" });
    expect(button.hasAttribute("disabled")).toBe(true);
    fireEvent.click(button);
    expect(mocks.createMutate).not.toHaveBeenCalled();
  });
  it("provisions for an allowed owner without self-creation permission", () => {
    mocks.creationOwners = ["user_other"];
    mocks.directoryRead = false;
    mocks.params = new URLSearchParams("create=true");
    setup();
    expect(mocks.ownerQuery).toHaveBeenCalledWith(
      expect.objectContaining({ limit: 50 }),
      undefined,
      expect.objectContaining({ signal: expect.anything() }),
    );
    fireEvent.change(screen.getByLabelText("Agent name"), {
      target: { value: "Provisioned" },
    });
    fireEvent.click(screen.getByRole("combobox", { name: "Owner" }));
    expect(screen.getByRole("option", { name: "Other Owner" })).toBeTruthy();
    expect(screen.queryByText("Unpermitted Owner")).toBeNull();
    fireEvent.click(screen.getByRole("option", { name: "Other Owner" }));
    fireEvent.click(screen.getByRole("button", { name: "Create agent" }));
    expect(
      mocks.createMutate.mock.calls[0]?.[0].request.createAgentForm.ownerUserId,
    ).toBe("user_other");
  });
  it("removes a cached owner and their selected label when permission is lost", () => {
    mocks.creationOwners = ["user_owner", "user_other"];
    mocks.params = new URLSearchParams("create=true");
    const view = setup();
    fireEvent.click(screen.getByRole("combobox", { name: "Owner" }));
    fireEvent.click(screen.getByRole("option", { name: "Other Owner" }));
    expect(
      screen.getByRole("combobox", { name: "Owner" }).textContent,
    ).toContain("Other Owner");
    mocks.creationOwners = ["user_owner"];
    view.rerenderPage();
    expect(
      screen.getByRole("combobox", { name: "Owner" }).textContent,
    ).toContain("You");
    expect(screen.queryByText("Other Owner")).toBeNull();
    fireEvent.click(screen.getByRole("combobox", { name: "Owner" }));
    expect(screen.queryByRole("option", { name: "Other Owner" })).toBeNull();
  });
  it.each([true, false])(
    "drops a removed owner even when its grant remains (self eligible=%s)",
    (canCreateForSelf) => {
      mocks.creationOwners = canCreateForSelf
        ? ["user_owner", "user_other"]
        : ["user_other"];
      mocks.params = new URLSearchParams("create=true");
      const view = setup();
      fireEvent.change(screen.getByLabelText("Agent name"), {
        target: { value: "Roster regression" },
      });
      fireEvent.click(screen.getByRole("combobox", { name: "Owner" }));
      fireEvent.click(screen.getByRole("option", { name: "Other Owner" }));

      mocks.owners = [];
      view.rerenderPage();

      expect(
        screen.getByRole("combobox", { name: "Owner" }).textContent,
      ).not.toContain("Other Owner");
      const create = screen.getByRole("button", { name: "Create agent" });
      expect(create.hasAttribute("disabled")).toBe(!canCreateForSelf);
      fireEvent.click(create);
      if (canCreateForSelf) {
        expect(
          mocks.createMutate.mock.calls[0]?.[0].request.createAgentForm
            .ownerUserId,
        ).toBe("user_owner");
      } else {
        expect(mocks.createMutate).not.toHaveBeenCalled();
      }
    },
  );
  it("uses distinct non-email labels for owners without display names", () => {
    mocks.creationOwners = ["user_other", "user_denied"];
    mocks.owners = [
      { userId: "user_other", displayName: "" },
      { userId: "user_denied", displayName: "   " },
    ];
    mocks.params = new URLSearchParams("create=true");
    setup();
    fireEvent.click(screen.getByRole("combobox", { name: "Owner" }));
    expect(screen.getByRole("option", { name: "user_other" })).toBeTruthy();
    expect(screen.getByRole("option", { name: "user_denied" })).toBeTruthy();
    expect(screen.queryByText(mocks.user.email)).toBeNull();
  });
  it("does not show owner query loading or errors when discovery is disabled", () => {
    mocks.creationOwners = [];
    mocks.ownersFetching = true;
    mocks.ownersError = true;
    mocks.params = new URLSearchParams("create=true");
    setup();
    expect(mocks.ownerQuery).not.toHaveBeenCalled();
    expect(screen.queryByText("Loading available owners…")).toBeNull();
    expect(screen.queryByText("Unable to load available owners.")).toBeNull();
    expect(screen.getByText(/Ask an organization admin/)).toBeTruthy();
  });
  it("keeps appended owners selected while the next page loads", async () => {
    mocks.creationOwners = ["user_owner", "user_other", "user_later"];
    mocks.nextCursor = "second";
    mocks.params = new URLSearchParams("create=true");
    let resolveThird!: (value: {
      result: { owners: typeof mocks.owners };
    }) => void;
    mocks.ownerQuery.mockImplementation(({ cursor }: { cursor?: string }) => {
      if (cursor === "second")
        return Promise.resolve({
          result: {
            owners: [{ userId: "user_later", displayName: "Later Owner" }],
            nextCursor: "third",
          },
        });
      if (cursor === "third")
        return new Promise((resolve) => {
          resolveThird = resolve;
        });
      return Promise.resolve({
        result: { owners: mocks.owners, nextCursor: "second" },
      });
    });
    setup();
    fireEvent.click(screen.getByRole("combobox", { name: "Owner" }));
    await loadMoreOwners();
    fireEvent.click(await screen.findByRole("option", { name: "Later Owner" }));
    fireEvent.click(screen.getByRole("combobox", { name: "Owner" }));
    await loadMoreOwners();
    expect(
      await screen.findByRole("button", { name: "Loading more…" }),
    ).toHaveProperty("disabled", true);
    expect(
      screen.getByRole("combobox", { name: "Owner" }).textContent,
    ).toContain("Later Owner");
    expect(screen.getByRole("option", { name: "Other Owner" })).toBeTruthy();
    expect(screen.getByRole("option", { name: "Later Owner" })).toBeTruthy();
    await act(async () => resolveThird({ result: { owners: [] } }));
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Load more" })).toBeNull(),
    );
    expect(
      screen.getByRole("combobox", { name: "Owner" }).textContent,
    ).toContain("Later Owner");
    expect(mocks.createMutate).not.toHaveBeenCalled();
  });
  it("retries a failed next page without discarding earlier owners", async () => {
    mocks.creationOwners = ["user_owner", "user_other", "user_later"];
    mocks.nextCursor = "second";
    mocks.params = new URLSearchParams("create=true");
    let failNextPage = true;
    mocks.ownerQuery.mockImplementation(({ cursor }: { cursor?: string }) => {
      if (!cursor)
        return Promise.resolve({
          result: { owners: mocks.owners, nextCursor: "second" },
        });
      if (failNextPage) return Promise.reject(new Error("Next page failed"));
      return Promise.resolve({
        result: {
          owners: [{ userId: "user_later", displayName: "Later Owner" }],
        },
      });
    });
    setup();
    fireEvent.click(screen.getByRole("combobox", { name: "Owner" }));
    await loadMoreOwners();
    await screen.findByRole("alert");
    expect(screen.getByRole("option", { name: "Other Owner" })).toBeTruthy();
    failNextPage = false;
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(
      await screen.findByRole("option", { name: "Later Owner" }),
    ).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
    expect(mocks.createMutate).not.toHaveBeenCalled();
  });
  it("replaces accumulated pages on roster refresh and drops inactive selected owners", async () => {
    mocks.creationOwners = ["user_owner", "user_other", "user_later"];
    mocks.nextCursor = "second";
    mocks.params = new URLSearchParams("create=true");
    mocks.ownerQuery.mockImplementation(({ cursor }: { cursor?: string }) => {
      if (cursor)
        return Promise.resolve({
          result: {
            owners: [{ userId: "user_later", displayName: "Later Owner" }],
          },
        });
      return Promise.resolve({
        result: { owners: mocks.owners, nextCursor: mocks.nextCursor },
      });
    });
    const { client } = setup();
    fireEvent.click(screen.getByRole("combobox", { name: "Owner" }));
    await loadMoreOwners();
    fireEvent.click(await screen.findByRole("option", { name: "Later Owner" }));
    mocks.nextCursor = undefined;
    mocks.owners = [];
    await act(async () => {
      await client.invalidateQueries({ queryKey: ownerQueryKey() });
    });
    await waitFor(() =>
      expect(
        screen.getByRole("combobox", { name: "Owner" }).textContent,
      ).toContain("You"),
    );
    fireEvent.click(screen.getByRole("combobox", { name: "Owner" }));
    expect(screen.queryByText("Later Owner")).toBeNull();
    expect(screen.queryByText("Other Owner")).toBeNull();
    expect(screen.queryByRole("button", { name: "Load more" })).toBeNull();
  });
  it("does not reuse accumulated pages after the grants context changes", async () => {
    mocks.creationOwners = ["user_owner", "user_other", "user_later"];
    mocks.nextCursor = "second";
    mocks.params = new URLSearchParams("create=true");
    mocks.ownerQuery.mockImplementation(({ cursor }: { cursor?: string }) => {
      if (cursor)
        return Promise.resolve({
          result: {
            owners: [{ userId: "user_later", displayName: "Later Owner" }],
          },
        });
      return Promise.resolve({
        result: { owners: mocks.owners, nextCursor: "second" },
      });
    });
    const view = setup();
    fireEvent.click(screen.getByRole("combobox", { name: "Owner" }));
    await loadMoreOwners();
    fireEvent.click(await screen.findByRole("option", { name: "Later Owner" }));
    // The selected owner's grant still matches, but this is a fresh roster context.
    mocks.creationOwners = ["user_owner", "user_later"];
    mocks.ownersFetching = true;
    mocks.ownerQuery.mockImplementation(() => new Promise(() => {}));
    view.rerenderPage();
    expect(
      screen.getByRole("combobox", { name: "Owner" }).textContent,
    ).toContain("You");
    fireEvent.click(screen.getByRole("combobox", { name: "Owner" }));
    expect(screen.queryByText("Later Owner")).toBeNull();
    expect(screen.queryByText("Other Owner")).toBeNull();
  });
  it("supports choosing an owner with the keyboard without a search input", async () => {
    mocks.creationOwners = ["user_owner", "user_other"];
    mocks.params = new URLSearchParams("create=true");
    setup();
    fireEvent.click(screen.getByRole("combobox", { name: "Owner" }));
    const command = document.querySelector("[cmdk-root]")!;
    await waitFor(() => expect(document.activeElement).toBe(command));
    fireEvent.keyDown(command, { key: "ArrowDown" });
    await waitFor(() =>
      expect(
        screen
          .getByRole("option", { name: "Other Owner" })
          .getAttribute("data-selected"),
      ).toBe("true"),
    );
    fireEvent.keyDown(command, { key: "Enter" });
    await waitFor(() =>
      expect(
        screen.getByRole("combobox", { name: "Owner" }).textContent,
      ).toContain("Other Owner"),
    );
    expect(screen.queryByRole("option")).toBeNull();
  });
  it("waits for grants before reporting creation is denied", () => {
    mocks.creationOwners = [];
    mocks.grantsLoading = true;
    mocks.params = new URLSearchParams("create=true");
    const view = setup();
    expect(screen.getByText("Loading available owners…")).toBeTruthy();
    expect(screen.queryByText(/Ask an organization admin/)).toBeNull();
    mocks.grantsLoading = false;
    view.rerenderPage();
    expect(screen.getByText(/Ask an organization admin/)).toBeTruthy();
  });
  it("does not report a denial while available owners are loading", () => {
    mocks.creationOwners = ["user_not_loaded"];
    mocks.ownersFetching = true;
    mocks.params = new URLSearchParams("create=true");
    setup();
    expect(screen.getByText("Loading available owners…")).toBeTruthy();
    expect(screen.queryByText(/Ask an organization admin/)).toBeNull();
  });
  it("offers a retry rather than a denial when owner discovery fails", async () => {
    mocks.creationOwners = ["user_not_loaded"];
    mocks.ownersError = true;
    mocks.params = new URLSearchParams("create=true");
    setup();
    expect((await screen.findByRole("alert")).textContent).toContain(
      "Unable to load available owners.",
    );
    expect(screen.queryByText(/Ask an organization admin/)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(mocks.ownerQuery).toHaveBeenCalledTimes(2);
  });
});

describe("Agent scope without an active project", () => {
  // useProject yields an empty id before a project resolves. Sending it would
  // read as "omitted" server-side, silently creating an organization-wide
  // agent after the user asked for a project one.
  it("cannot choose project scope until a project resolves", () => {
    mocks.projectId = "";
    mocks.params = new URLSearchParams("create=true");
    setup();
    fireEvent.change(screen.getByLabelText("Agent name"), {
      target: { value: "Unresolved project agent" },
    });

    const projectOption = screen.getByRole("radio", { name: /Project/ });
    expect(projectOption.getAttribute("data-disabled")).not.toBeNull();

    fireEvent.click(projectOption);
    fireEvent.click(screen.getByRole("button", { name: "Create agent" }));

    expect(mocks.createMutate).toHaveBeenCalledTimes(1);
    const form = mocks.createMutate.mock.calls[0]![0].request.createAgentForm;
    expect(form).not.toHaveProperty("projectId");
  });
});

describe("Agent list scope filtering", () => {
  const inventory = [...mocks.agents];
  afterEach(() => {
    mocks.agents = [...inventory];
  });

  it("shows the create empty state when only other projects have agents", () => {
    mocks.agents = [{ ...mocks.agents[0]!, projectId: "other-project" }];
    setup();
    expect(screen.getByText("No agents yet")).toBeTruthy();
    expect(screen.queryByText("No matching agents")).toBeNull();
  });

  it("shows no matches for a search of project-visible agents", () => {
    setup();
    fireEvent.change(screen.getByPlaceholderText("Search agents"), {
      target: { value: "missing agent" },
    });
    expect(screen.getByText("No matching agents")).toBeTruthy();
    expect(screen.queryByText("No agents yet")).toBeNull();
  });

  it("hides agents bound to another project and keeps organization-wide ones after refetch", async () => {
    mocks.agents = [
      {
        ...mocks.agents[0]!,
        id: "agent_here",
        name: "This project agent",
        projectId: mocks.projectId,
      },
      {
        ...mocks.agents[0]!,
        id: "agent_elsewhere",
        name: "Other project agent",
        projectId: "00000000-0000-4000-8000-0000000000ff",
      },
      {
        ...mocks.agents[0]!,
        id: "agent_org",
        name: "Org wide agent",
        projectId: undefined,
      },
    ];
    const { client } = setup();
    await client.invalidateQueries({ queryKey: ["managed-agents"] });
    expect(mocks.list).toHaveBeenCalled();
    expect(
      client.getQueryData([
        "managed-agents",
        mocks.organizationId,
        "list",
        mocks.user.id,
      ]),
    ).toEqual(mocks.agents);

    expect(
      screen.getByRole("button", { name: "This project agent" }),
    ).toBeTruthy();
    expect(screen.getByRole("button", { name: "Org wide agent" })).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Other project agent" }),
    ).toBeNull();
  });
});
