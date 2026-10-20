import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { AccessHubScopeContext, type AccessHubScope } from "./accessHubScope";
import { WorkloadIssuersPage } from "./WorkloadIssuers";

const mocks = vi.hoisted(() => ({
  useWorkloadIdentities: vi.fn(),
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@/components/page-templates", () => ({
  ResourceListPage: ({
    children,
    primaryAction,
  }: {
    children: ReactNode;
    primaryAction: ReactNode;
  }) => (
    <>
      {primaryAction}
      {children}
    </>
  ),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    workloadIssuers: {
      href: () => "/org/access-hub",
      issuerDetail: { href: (id: string) => `/org/access-hub/${id}` },
      catalogPlatform: {
        href: (key: string) => `/org/access-hub/catalog/${key}`,
      },
    },
  }),
}));
vi.mock("@gram/client/react-query/workloadIdentities.js", () => ({
  useWorkloadIdentities: mocks.useWorkloadIdentities,
  invalidateAllWorkloadIdentities: vi.fn(),
}));
vi.mock("@gram/client/react-query/workloadPlatforms.js", () => ({
  useWorkloadPlatforms: () => ({ data: { platforms: [] }, isPending: false }),
}));
vi.mock("@gram/client/react-query/workloadCustomFlows.js", async () => {
  const { customFlows } = await import("./custom/customFlowsFixture");
  return {
    useWorkloadCustomFlows: () => ({
      data: customFlows,
      isPending: false,
      isError: false,
    }),
  };
});
vi.mock("@gram/client/react-query/registerWorkloadIssuer.js", () => ({
  useRegisterWorkloadIssuerMutation: () => ({
    mutate: vi.fn(),
    isPending: false,
  }),
}));

const projectScope: AccessHubScope = {
  projectScoped: true,
  hubHref: () => "/org/projects/project/access-hub",
  issuerHref: (id) => `/org/projects/project/access-hub/${id}`,
  catalogPlatformHref: (key) =>
    `/org/projects/project/access-hub/catalog/${key}`,
};

beforeEach(() => {
  mocks.useWorkloadIdentities.mockReturnValue({
    data: {
      issuers: [
        {
          id: "issuer-1",
          organizationId: "example-org",
          projectId: "project-1",
          name: "Build",
          issuer: "https://build.example.com",
          jwksUri: "https://build.example.com/jwks",
          description: "",
          allowWildcardAdmission: true,
          tags: [],
          createdAt: new Date("2026-10-19T00:00:00Z"),
          updatedAt: new Date("2026-10-19T00:00:00Z"),
        },
      ],
      admissions: [],
    },
    isPending: false,
  });
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function renderHub(scope?: AccessHubScope) {
  const page = <WorkloadIssuersPage />;
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>
        {scope ? (
          <AccessHubScopeContext.Provider value={scope}>
            {page}
          </AccessHubScopeContext.Provider>
        ) : (
          page
        )}
      </MemoryRouter>
    </QueryClientProvider>,
  );
  fireEvent.click(screen.getByRole("button", { name: /^Custom/ }));
}

it("reads the project tier and links within the project on a project's hub", () => {
  renderHub(projectScope);

  expect(mocks.useWorkloadIdentities).toHaveBeenCalledWith({
    projectScoped: true,
  });
  expect(screen.getByRole("link", { name: /Build/ }).getAttribute("href")).toBe(
    "/org/projects/project/access-hub/issuer-1",
  );
});

it("reads the organization tier outside a project's hub", () => {
  renderHub();

  expect(mocks.useWorkloadIdentities).toHaveBeenCalledWith({
    projectScoped: false,
  });
  expect(screen.getByRole("link", { name: /Build/ }).getAttribute("href")).toBe(
    "/org/access-hub/issuer-1",
  );
});
