import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import IdentityDetailRoot from "./IdentityDetailRoot";

const mocks = vi.hoisted(() => ({ orgRead: false }));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org_example", name: "Example Org" }),
  useProject: () => ({ id: "project_one", slug: "project-one" }),
  useSession: () => ({ user: { id: "user_example" } }),
  useIsPlatformAdmin: () => false,
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => mocks.orgRead, isLoading: false }),
}));
const page = (segment: string) => ({
  href: (urn: string) => `/identities/${urn}/${segment}`,
  active: false,
});
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    agents: { href: () => "/agents" },
    identities: {
      href: (urn?: string) => (urn ? `/identities/${urn}` : "/identities"),
      Link: ({ children }: { children?: React.ReactNode }) => (
        <span>{children}</span>
      ),
      agents: { href: () => "/identities/agents" },
      detail: {
        overview: page("overview"),
        access: page("access"),
        usage: page("usage"),
        security: page("security"),
        findings: page("findings"),
        cost: page("cost"),
        connections: page("connections"),
        devices: page("devices"),
        activity: page("activity"),
        permissions: page("permissions"),
        provisioning: page("provisioning"),
        sessions: page("sessions"),
      },
    },
  }),
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
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("./useIdentitySubject", () => ({
  useIdentitySubject: () => ({ isLoading: true, data: undefined }),
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

function setup(urn: string) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[`/identities/${encodeURIComponent(urn)}`]}>
        <Routes>
          <Route
            path="/identities/:identityUrn"
            element={<IdentityDetailRoot />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(cleanup);
beforeEach(() => {
  mocks.orgRead = false;
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
