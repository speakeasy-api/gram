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
    stage,
  }: {
    children: ReactNode;
    primaryAction: ReactNode;
    stage?: string;
  }) => (
    <>
      {stage && <span data-testid="stage">{stage}</span>}
      {primaryAction}
      {children}
    </>
  ),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
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

function renderPageOnCatalog() {
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
}

function renderPage() {
  renderPageOnCatalog();
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

it("marks the Access Hub as a preview", () => {
  renderPage();

  expect(screen.getByTestId("stage").textContent).toBe("preview");
});

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

it.each([
  ["a name, ignoring case", "LEG", ["legacy"]],
  ["a description", "pull request", ["staging"]],
  ["an issuer URL", "build.example.com", ["build"]],
])("matches %s", async (_, query, expected) => {
  renderPage();

  await search(query, expected);
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
  expect(staging.textContent).not.toContain("https://staging.example.com");

  // Without a description the issuer URL stands where the description would be.
  const build = screen.getByRole("link", { name: /^build/ });
  expect(build.textContent).toContain("https://build.example.com");
});

it("leaves the issuer and keys URLs to the platform's own page", () => {
  renderPage();

  for (const link of screen.getAllByRole("link")) {
    expect(link.textContent).not.toContain("Issuer:");
    expect(link.textContent).not.toContain("Keys:");
    expect(link.textContent).not.toContain("/jwks");
  }
});

it("opens on the catalog, which is empty until presets exist", () => {
  renderPageOnCatalog();

  // Catalog leads: an operator arrives asking which platform they are
  // connecting, so presets are the first thing shown.
  expect(screen.getByText("No catalog platforms yet")).toBeTruthy();
  expect(screen.queryAllByRole("link")).toHaveLength(0);
});
