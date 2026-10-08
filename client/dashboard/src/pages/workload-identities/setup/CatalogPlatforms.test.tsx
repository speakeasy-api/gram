import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import { CatalogPlatformPage } from "./CatalogPlatformPage";
import { CatalogPlatforms } from "./CatalogPlatforms";
import { testPlatform } from "./testPlatform";

const policy = vi.hoisted(() => ({
  issuers: [] as unknown[],
  isError: false,
}));
const catalogQuery = vi.hoisted(() => ({ isError: false }));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@/components/page-templates", () => ({
  ResourceListPage: ({
    title,
    children,
    primaryAction,
  }: {
    title: ReactNode;
    children: ReactNode;
    primaryAction: ReactNode;
  }) => (
    <>
      <h1>{title}</h1>
      {primaryAction}
      {children}
    </>
  ),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    workloadIssuers: {
      href: () => "/access-hub",
      catalogPlatform: { href: (key: string) => `/access-hub/catalog/${key}` },
    },
  }),
}));
vi.mock("../WorkloadIssuerDetail", () => ({
  CatalogEmptyState: ({
    catalog,
  }: {
    catalog: { name: string; setupButton: ReactNode };
  }) => (
    <div>
      <p>{`No ${catalog.name} connections yet`}</p>
      {catalog.setupButton}
    </div>
  ),
  IssuerDetail: ({
    issuerId,
    catalog,
  }: {
    issuerId: string;
    catalog: { title: ReactNode; registerButton: ReactNode };
  }) => (
    <div data-testid="issuer-detail" data-issuer={issuerId}>
      <h1>{catalog.title}</h1>
      {catalog.registerButton}
    </div>
  ),
}));
vi.mock("@gram/client/react-query/workloadPlatforms.js", () => ({
  useWorkloadPlatforms: () =>
    catalogQuery.isError
      ? { data: undefined, isPending: false, isError: true, refetch: vi.fn() }
      : {
          data: { platforms: [testPlatform] },
          isPending: false,
          isError: false,
        },
}));
vi.mock("@gram/client/react-query/workloadIdentities.js", () => ({
  useWorkloadIdentities: () =>
    policy.isError
      ? { data: undefined, isPending: false, isError: true, refetch: vi.fn() }
      : {
          data: { issuers: policy.issuers, admissions: [] },
          isPending: false,
          isError: false,
        },
  invalidateAllWorkloadIdentities: vi.fn(),
}));
vi.mock("@gram/client/react-query/createAgent.js", () => ({
  useCreateAgentMutation: () => ({ mutateAsync: vi.fn() }),
}));
// The picker drains every page of agents.list, so the list arrives through
// the sdk client rather than a generated hook.
vi.mock("@gram/client/react-query/agents.js", () => ({
  invalidateAllAgents: vi.fn(),
  queryKeyAgents: () => ["agents"],
}));
vi.mock("@/components/sessions/collectPageItems", () => ({
  collectPageItems: () =>
    Promise.resolve([
      { id: "agent-1", name: "Support bot", lifecycle: "active" },
    ]),
}));
vi.mock("@gram/client/react-query/workloadTokenEndpoints.js", () => ({
  useWorkloadTokenEndpoints: () => ({
    data: { items: [] },
    isPending: false,
    isError: false,
  }),
}));
vi.mock("@gram/client/react-query/registerWorkloadIssuer.js", () => ({
  useRegisterWorkloadIssuerMutation: () => ({ mutateAsync: vi.fn() }),
}));
vi.mock("@gram/client/react-query/admitWorkloadSubject.js", () => ({
  useAdmitWorkloadSubjectMutation: () => ({ mutateAsync: vi.fn() }),
}));

afterEach(() => {
  cleanup();
  policy.issuers = [];
  policy.isError = false;
  catalogQuery.isError = false;
});

const claudeTagIssuer = {
  id: "issuer-1",
  issuer: "https://identity.anthropic.com/agents",
  jwksUri: "https://identity.anthropic.com/agents/jwks.json",
};

function renderPage(url: string) {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter initialEntries={[url]}>
        <Routes>
          <Route
            path="/access-hub/catalog/:platformKey"
            element={<CatalogPlatformPage />}
          />
          <Route path="/access-hub" element={<p>Access Hub list</p>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function currentStep(): string | null {
  return (
    screen
      .getAllByRole("button")
      .find((button) => button.getAttribute("aria-current") === "step")
      ?.getAttribute("aria-label") ?? null
  );
}

it("links each catalog card to its platform's page", () => {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>
        <CatalogPlatforms issuers={[]} isPending={false} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  expect(screen.getByRole("link").getAttribute("href")).toBe(
    "/access-hub/catalog/claude-tag",
  );
  expect(screen.getByText("Set up")).toBeTruthy();
});

it("offers to register access for a platform not yet trusted", () => {
  renderPage("/access-hub/catalog/claude-tag");
  expect(screen.getByText("No Claude Tag connections yet")).toBeTruthy();
  expect(screen.queryByTestId("issuer-detail")).toBeNull();
});

it("lists a trusted platform's access on the trusted platform's page", () => {
  policy.issuers = [claudeTagIssuer];
  renderPage("/access-hub/catalog/claude-tag");
  expect(screen.getByTestId("issuer-detail").getAttribute("data-issuer")).toBe(
    "issuer-1",
  );
});

it("opens the guided setup at its first step from Register new access", () => {
  policy.issuers = [claudeTagIssuer];
  renderPage("/access-hub/catalog/claude-tag");
  fireEvent.click(screen.getByRole("button", { name: "Register new access" }));
  expect(screen.getByText("Set up Claude Tag")).toBeTruthy();
  expect(currentStep()).toBe("Step 1: Before you start");
});

it("does not let a link skip past steps that collect values", () => {
  renderPage("/access-hub/catalog/claude-tag?setup=&step=agent");
  expect(currentStep()).toBe("Step 1: Before you start");
});

it("sends an unknown platform back to the Access Hub", () => {
  renderPage("/access-hub/catalog/nope");
  expect(screen.getByText("Access Hub list")).toBeTruthy();
});

it("opens the guided setup from the empty state's Set one up", () => {
  renderPage("/access-hub/catalog/claude-tag");
  fireEvent.click(screen.getByRole("button", { name: "Set one up" }));
  expect(currentStep()).toBe("Step 1: Before you start");
});

it("offers a retry rather than an empty grid when the catalog fails to load", () => {
  catalogQuery.isError = true;
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>
        <CatalogPlatforms issuers={[]} isPending={false} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  expect(screen.getByText("Couldn’t load the catalog")).toBeTruthy();
  expect(screen.getByRole("button", { name: "Retry" })).toBeTruthy();
});

it("stays on the platform's page when the catalog fails to load", () => {
  catalogQuery.isError = true;
  renderPage("/access-hub/catalog/claude-tag");
  expect(screen.getByText("Couldn’t load this platform")).toBeTruthy();
  expect(screen.queryByText("Access Hub list")).toBeNull();
});

it("does not read a failed policy load as a platform not yet trusted", () => {
  policy.isError = true;
  renderPage("/access-hub/catalog/claude-tag");
  expect(screen.getByText("Couldn’t load this platform")).toBeTruthy();
  expect(screen.queryByText("No Claude Tag connections yet")).toBeNull();
});
