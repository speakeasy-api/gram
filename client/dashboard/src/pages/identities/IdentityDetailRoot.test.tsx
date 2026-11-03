import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  getRecentLabelOverride,
  recordVisit,
} from "@/components/command-palette/recentlyVisited";
import IdentityDetailRoot from "./IdentityDetailRoot";

const mocks = vi.hoisted(() => ({
  orgRead: false,
  subject: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org_example", name: "Example Org" }),
  useProject: () => ({ id: "project_one", slug: "project-one" }),
  useSession: () => ({ user: { id: "user_example" } }),
  useIsPlatformAdmin: () => false,
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => mocks.orgRead, isLoading: false }),
}));
// Do not mount the panels: these regressions exercise the route's
// authorization boundary, not what the page then shows.
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ scope }: { scope: string[] }) => (
    <div>Scope: {scope.join(",")}</div>
  ),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({}),
  useProjectSlugForRequests: () => "example",
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project-one" }),
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("./useIdentitySubject", () => ({
  useIdentitySubject: () => mocks.subject(),
}));
vi.mock("@/components/page-layout", () => {
  const Box = ({ children }: { children?: React.ReactNode }) => (
    <div>{children}</div>
  );
  return {
    Page: Object.assign(Box, {
      Header: Object.assign(Box, { Breadcrumbs: () => null }),
      Body: Box,
    }),
  };
});

vi.mock("./useIdentityQueries", () => ({
  useCanReadRisk: () => false,
  useIdentityIsKnown: () => ({ isPending: false, known: true }),
}));
vi.mock("@gram/client/react-query/sessionInfo.js", () => ({
  useSessionInfo: () => ({ data: { result: { userId: "user_example" } } }),
}));
vi.mock("@/components/identity-rail", () => ({
  IdentityRail: () => <nav>Identity navigation</nav>,
}));
vi.mock("./AgentIdentityProfile", () => ({
  AgentIdentityProfile: ({
    agent,
  }: {
    agent: { permissions: { write: boolean; transfer: boolean } };
  }) => (
    <div>
      Agent profile: write={String(agent.permissions.write)}, transfer=
      {String(agent.permissions.transfer)}
    </div>
  ),
  AgentHeaderActions: () => <button>Agent header actions</button>,
  AgentRenameAction: () => <button>Rename agent</button>,
}));

function setup(urn: string) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter
        initialEntries={[
          `/org/projects/project-one/identities/${encodeURIComponent(urn)}/overview`,
        ]}
      >
        <Routes>
          <Route
            path="/org/projects/project-one/identities/:identityUrn"
            element={<IdentityDetailRoot />}
          >
            <Route path="overview" element={<h1>Identity overview</h1>} />
          </Route>
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
beforeEach(() => {
  localStorage.clear();
  mocks.orgRead = false;
  mocks.subject.mockReturnValue({ isPending: true, data: undefined });
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      disconnect() {}
    },
  );
});

// An agent resolves through its own management API, which authorizes per
// agent. Holding it behind org:read shut an owner out of their own agent, and
// used to be worked around by sending them to a second agent page. There is
// one agent page now, and ownership is enough to open it.
it("does not hold an agent behind organization read", () => {
  setup("agent:agent_example");
  expect(screen.queryByText("Scope: org:read")).toBeNull();
});

it("opens the same agent page for an organization reader", () => {
  mocks.orgRead = true;
  setup("agent:agent_example");
  expect(screen.queryByText("Scope: org:read")).toBeNull();
});

// A person is resolved by the directory, which the server gates on org:read,
// so the page says so rather than rendering and failing its first request.
it("keeps human profiles behind organization read", () => {
  setup("user:user_example");
  expect(screen.getByText("Scope: org:read")).toBeTruthy();
});

const cachedAgent = {
  canonicalUrn: "agent:agent_example",
  kind: "agent",
  displayName: "Private agent name",
  emails: [],
  directory: { groups: [] },
  agent: { id: "agent_example", permissions: { write: true, transfer: true } },
};

it.each([403, 404])(
  "hides cached agent details after a %s refetch",
  (statusCode) => {
    mocks.subject.mockReturnValue({
      data: cachedAgent,
      error: { statusCode },
      isPending: false,
    });
    setup("agent:agent_example");
    expect(screen.getByText("Agent unavailable")).toBeTruthy();
    expect(screen.queryByText(cachedAgent.displayName)).toBeNull();
    expect(screen.queryByText(/Agent profile:/)).toBeNull();
    expect(screen.queryByText("Agent header actions")).toBeNull();
    expect(screen.queryByText("Rename agent")).toBeNull();
    expect(screen.queryByText("Identity navigation")).toBeNull();
  },
);

it.each([{ statusCode: 500 }, new Error("Network unavailable")])(
  "keeps cached agent details on a transient error: %s",
  (error) => {
    mocks.subject.mockReturnValue({
      data: cachedAgent,
      error,
      isPending: false,
    });
    setup("agent:agent_example");
    expect(screen.queryByText("Agent unavailable")).toBeNull();
    expect(screen.getByText(cachedAgent.displayName)).toBeTruthy();
    expect(
      screen.getByText("Agent profile: write=true, transfer=true"),
    ).toBeTruthy();
  },
);

it("renders a successful retained-access result with updated permissions", () => {
  mocks.subject.mockReturnValue({
    data: {
      ...cachedAgent,
      agent: {
        ...cachedAgent.agent,
        permissions: { write: false, transfer: false },
      },
    },
    error: null,
    isPending: false,
  });
  setup("agent:agent_example");
  expect(screen.queryByText("Agent unavailable")).toBeNull();
  expect(screen.getByText(cachedAgent.displayName)).toBeTruthy();
  expect(
    screen.getByText("Agent profile: write=false, transfer=false"),
  ).toBeTruthy();
});

it.each([403, 404])(
  "forgets a previously readable agent in recents after %s",
  (statusCode) => {
    const href =
      "/org/projects/project-one/identities/agent%3Aagent_example/overview";
    mocks.subject.mockReturnValue({
      data: cachedAgent,
      error: null,
      isPending: false,
    });
    setup("agent:agent_example");
    expect(getRecentLabelOverride(href)).toBe(cachedAgent.displayName);
    recordVisit("user_example", "org", "project-one", {
      href,
      label: cachedAgent.displayName,
    });
    recordVisit("user_example", "org", "project-one", {
      href: "/org/projects/project-one/identities/agent%3Aother/overview",
      label: "Other agent",
    });
    cleanup();

    mocks.subject.mockReturnValue({
      data: cachedAgent,
      error: { statusCode },
      isPending: false,
    });
    setup("agent:agent_example");
    expect(getRecentLabelOverride(href)).toBeUndefined();
    const stored = Array.from({ length: localStorage.length }, (_, i) =>
      localStorage.getItem(localStorage.key(i)!),
    ).join("");
    expect(stored).not.toContain(cachedAgent.displayName);
    expect(stored).not.toContain(href);
    expect(stored).toContain("Other agent");
  },
);
