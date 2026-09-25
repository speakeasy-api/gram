import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import { WorkloadIssuersPage } from "./WorkloadIssuers";

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
  useRoutes: () => ({
    workloadIssuers: {
      issuerDetail: { href: (id: string) => `/access-hub/${id}` },
    },
  }),
}));

function issuer(name: string, tags: string[]) {
  return {
    id: `issuer-${name}`,
    organizationId: "example-org",
    projectId: "",
    name,
    issuer: `https://${name}.example.com`,
    jwksUri: `https://${name}.example.com/jwks`,
    allowWildcardAdmission: true,
    tags,
    createdAt: new Date("2026-09-25T00:00:00Z"),
    updatedAt: new Date("2026-09-25T00:00:00Z"),
  };
}

vi.mock("@gram/client/react-query/workloadIdentities.js", () => ({
  useWorkloadIdentities: () => ({
    data: {
      issuers: [
        issuer("build", ["production", "ci"]),
        issuer("staging", ["ci"]),
        issuer("legacy", []),
      ],
      admissions: [],
    },
    isPending: false,
  }),
  invalidateAllWorkloadIdentities: vi.fn(),
}));
vi.mock("@gram/client/react-query/registerWorkloadIssuer.js", () => ({
  useRegisterWorkloadIssuerMutation: () => ({
    mutate: vi.fn(),
    isPending: false,
  }),
}));

afterEach(cleanup);

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <WorkloadIssuersPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  // Catalog leads, so the platforms live behind the Private toggle.
  fireEvent.click(screen.getByRole("button", { name: /^Private/ }));
}

function visiblePlatforms(): string[] {
  return screen
    .getAllByRole("link")
    .map((link) => link.textContent ?? "")
    .map((text) => text.split("https://")[0]?.trim() ?? "");
}

it("offers only the tags actually in use, so a filter cannot empty the list", () => {
  renderPage();

  const chips = screen
    .getAllByRole("button", { pressed: false })
    .map((button) => button.textContent);

  expect(chips).toContain("production");
  expect(chips).toContain("ci");
  // "legacy" carries no tags, so its name must not become a filter.
  expect(chips).not.toContain("legacy");
});

it("narrows the platforms to the ones carrying the selected tag", () => {
  renderPage();

  expect(visiblePlatforms()).toEqual(["build", "staging", "legacy"]);

  fireEvent.click(screen.getByRole("button", { name: "production" }));

  expect(visiblePlatforms()).toEqual(["build"]);
});

it("clears the filter when the active tag is clicked again", () => {
  renderPage();

  fireEvent.click(screen.getByRole("button", { name: "ci" }));
  expect(visiblePlatforms()).toEqual(["build", "staging"]);

  fireEvent.click(screen.getByRole("button", { name: "ci" }));
  expect(visiblePlatforms()).toEqual(["build", "staging", "legacy"]);
});

it("restores every platform through All", () => {
  renderPage();

  fireEvent.click(screen.getByRole("button", { name: "production" }));
  fireEvent.click(screen.getByRole("button", { name: "All" }));

  expect(visiblePlatforms()).toEqual(["build", "staging", "legacy"]);
});
