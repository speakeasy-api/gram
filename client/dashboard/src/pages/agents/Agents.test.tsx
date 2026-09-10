import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import AgentsPage from "./Agents";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
function setup() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  client.setQueryData(
    ["managed-agents", mocks.organizationId, "list"],
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
vi.mock("./AgentAPIKeys", () => ({ AgentAPIKeys: () => <div>API keys</div> }));
vi.mock("./ManagedAgentSessions", () => ({
  ManagedAgentSessions: () => <div>Sessions</div>,
}));

const mocks = vi.hoisted(() => ({
  params: new URLSearchParams(),
  navigate: vi.fn(),
  list: vi.fn(),
  detail: vi.fn(),
  organizationId: "org_example",
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
  useOrganization: () => ({ id: mocks.organizationId, slug: "example" }),
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
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({ agents: { list: mocks.list, get: mocks.detail } }),
}));
vi.mock("@gram/client/react-query/createAgent.js", () => ({
  useCreateAgentMutation: () => ({ mutate: vi.fn() }),
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
  mocks.params = new URLSearchParams();
  mocks.unsupported = false;
  mocks.organizationId = "org_example";
  mocks.impersonatorEmail = undefined;
  mocks.scopeOverride = null;
  mocks.agents[0]!.ownerUserId = "user_owner";
  mocks.agents[0]!.ownerProfile = undefined;
  vi.clearAllMocks();
  mocks.list.mockResolvedValue(mocks.agents);
  mocks.detail.mockResolvedValue(mocks.agents[0]);
});

describe("Agent owner access", () => {
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
  it("keeps creation separate from the inventory", () => {
    setup();
    fireEvent.click(screen.getByRole("button", { name: "Create agent" }));
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
});
