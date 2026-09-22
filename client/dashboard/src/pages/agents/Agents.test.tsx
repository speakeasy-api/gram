import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import type { FeatureFlagResult } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
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
  const view = render(
    <QueryClientProvider client={client}>
      <AgentsPage />
    </QueryClientProvider>,
  );
  return {
    ...view,
    client,
    rerenderPage: () =>
      view.rerender(
        <QueryClientProvider client={client}>
          <AgentsPage />
        </QueryClientProvider>,
      ),
  };
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
  managementFlag: "enabled" as FeatureFlagResult["status"],
  sdkClient: vi.fn(),
  params: new URLSearchParams(),
  navigate: vi.fn(),
  list: vi.fn(),
  detail: vi.fn(),
  organizationId: "org_example",
  projectId: "00000000-0000-4000-8000-000000000001",
  createMutate: vi.fn(),
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
  useFeatureFlag: (flag: string) => ({
    status:
      flag === FEATURE_FLAGS.agentManagement ? mocks.managementFlag : "enabled",
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
    primaryAction,
    search,
    isEmpty = false,
    empty,
  }: {
    children: ReactNode;
    title: string;
    description: string;
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
  mocks.managementFlag = "enabled";
  mocks.params = new URLSearchParams();
  mocks.unsupported = false;
  mocks.organizationId = "org_example";
  mocks.projectId = "00000000-0000-4000-8000-000000000001";
  mocks.impersonatorEmail = undefined;
  mocks.scopeOverride = null;
  mocks.agents[0]!.ownerUserId = "user_owner";
  mocks.agents[0]!.ownerProfile = undefined;
  vi.clearAllMocks();
  mocks.sdkClient.mockReturnValue({
    agents: { list: mocks.list, get: mocks.detail },
  });
  mocks.list.mockResolvedValue(mocks.agents);
  mocks.detail.mockResolvedValue(mocks.agents[0]);
});

describe("Agent management rollout gate", () => {
  for (const status of ["loading", "disabled", "missing", "error"] as const) {
    it.each(["", "create=true", "id=agent_example"])(
      `blocks direct route %s while management is ${status}, even with credentials enabled`,
      (query) => {
        mocks.managementFlag = status;
        mocks.params = new URLSearchParams(query);
        setup();

        expect(
          screen.getByRole("heading", {
            name:
              status === "loading"
                ? "Loading agent management"
                : "Agent management unavailable",
          }),
        ).toBeTruthy();
        expect(
          screen.queryByRole("button", { name: "Create agent" }),
        ).toBeNull();
        expect(screen.queryByText("Example agent")).toBeNull();
        expect(screen.queryByText("API keys")).toBeNull();
        expect(screen.queryByText("Unable to load agents")).toBeNull();
        // Cached inventory must not leak, and no SDK-backed child may mount
        // (including creation's policy/server discovery).
        expect(mocks.sdkClient).not.toHaveBeenCalled();
        expect(mocks.list).not.toHaveBeenCalled();
        expect(mocks.detail).not.toHaveBeenCalled();
      },
    );
  }

  it("unmounts cached agent content when the management flag turns off", () => {
    const view = setup();
    expect(screen.getByRole("button", { name: "Example agent" })).toBeTruthy();
    mocks.managementFlag = "disabled";
    view.rerenderPage();
    expect(screen.queryByRole("button", { name: "Example agent" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Create agent" })).toBeNull();
  });
});

describe("Agent owner access", () => {
  it.each(["loading", "disabled", "missing", "error"] as const)(
    "does not request agents while the rollout is %s",
    (status) => {
      mocks.managementFlag = status;
      setup();
      expect(mocks.list).not.toHaveBeenCalled();
      expect(
        screen.queryByText("Unable to load agents. Try again."),
      ).toBeNull();
    },
  );

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

  it("defaults to organization scope and sends no project binding", () => {
    openCreateForm();
    expect(
      screen
        .getByRole("radio", { name: /Organization/ })
        .getAttribute("data-state"),
    ).toBe("checked");

    fireEvent.click(screen.getByRole("button", { name: "Create agent" }));

    expect(mocks.createMutate).toHaveBeenCalledTimes(1);
    const form = mocks.createMutate.mock.calls[0]![0].request.createAgentForm;
    expect(form.name).toBe("Scoped agent");
    // Omitted rather than blank: the server reads a missing binding as
    // organization-wide.
    expect("projectId" in form).toBe(false);
  });

  it("sends the active project when project scope is chosen", () => {
    openCreateForm();
    fireEvent.click(screen.getByRole("radio", { name: /Project/ }));
    fireEvent.click(screen.getByRole("button", { name: "Create agent" }));

    expect(mocks.createMutate).toHaveBeenCalledTimes(1);
    const form = mocks.createMutate.mock.calls[0]![0].request.createAgentForm;
    expect(form.projectId).toBe(mocks.projectId);
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

    const form = mocks.createMutate.mock.calls[0]?.[0].request.createAgentForm;
    expect(form?.projectId).toBeUndefined();
  });
});
