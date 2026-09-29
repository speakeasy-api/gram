import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
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

function issuer(name: string, tags: string[], description = "") {
  return {
    id: `issuer-${name}`,
    organizationId: "example-org",
    projectId: "",
    name,
    issuer: `https://${name}.example.com`,
    jwksUri: `https://${name}.example.com/jwks`,
    description,
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
        issuer("staging", ["ci"], "Preview deploys for every pull request"),
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
  // Catalog leads, so the platforms live behind the Custom toggle.
  fireEvent.click(screen.getByRole("button", { name: /^Custom/ }));
}

const PLATFORM_NAMES = ["build", "staging", "legacy"];

function visiblePlatforms(): string[] {
  return screen
    .getAllByRole("link")
    .map((link) => link.textContent ?? "")
    .map((text) => PLATFORM_NAMES.find((name) => text.startsWith(name)) ?? "");
}

// The toolbar search reports its value on a timer, even undebounced, so each
// query is awaited until the list reflects it.
async function search(query: string, expected: string[]) {
  fireEvent.change(
    screen.getByPlaceholderText("Search name, description, URL or tag…"),
    { target: { value: query } },
  );
  await waitFor(() => expect(visiblePlatforms()).toEqual(expected));
}

it("finds platforms by free-text search, with no per-tag filter controls", () => {
  renderPage();

  // A tag appears only as a label on the cards that carry it: nothing outside
  // a card offers it as a filter, so a tag is found by typing it.
  const productionLabels = screen.getAllByText("production");
  expect(productionLabels.length).toBeGreaterThan(0);
  for (const label of productionLabels) {
    expect(label.closest("a")).not.toBeNull();
  }
  expect(visiblePlatforms()).toEqual(["build", "staging", "legacy"]);
});

it("narrows the platforms to the ones matching a tag", async () => {
  renderPage();

  await search("production", ["build"]);
});

it("matches the name, description and issuer URL, ignoring case", async () => {
  renderPage();

  await search("LEG", ["legacy"]);
  await search("pull request", ["staging"]);
  await search("build.example.com", ["build"]);
});

it("restores every platform when the search is cleared", async () => {
  renderPage();

  await search("production", ["build"]);
  await search("", ["build", "staging", "legacy"]);
});

it("says so when nothing matches", async () => {
  renderPage();

  fireEvent.change(
    screen.getByPlaceholderText("Search name, description, URL or tag…"),
    { target: { value: "no-such-platform" } },
  );

  expect(await screen.findByText("No platforms match")).toBeTruthy();
  expect(screen.queryAllByRole("link")).toHaveLength(0);
});

it("shows a platform's description in place of its issuer URL", () => {
  renderPage();

  const staging = screen.getByRole("link", { name: /^staging/ });
  expect(staging.textContent).toContain(
    "Preview deploys for every pull request",
  );
  // With a description, the URL sits on a labeled line beside the keys.
  expect(staging.textContent).toContain("Issuer: https://staging.example.com");

  // Without a description the issuer URL stays where the description would be,
  // so it is not repeated on a labeled line.
  const build = screen.getByRole("link", { name: /^build/ });
  expect(build.textContent).not.toContain("Issuer:");
  expect(build.textContent).toContain("https://build.example.com");
});
