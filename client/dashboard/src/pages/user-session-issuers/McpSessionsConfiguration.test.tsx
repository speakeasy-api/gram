import { cleanup, render, screen, within } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import { McpSessionsConfiguration } from "./McpSessionsConfiguration";

const tiers = vi.hoisted(() => ({
  organization: [] as Record<string, unknown>[],
  project: [] as Record<string, unknown>[],
}));

vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project_a", slug: "first", name: "First" }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "first" }),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => children,
}));
vi.mock("@/components/page-templates", () => ({
  TabbedPage: ({
    children,
    activeTab,
  }: {
    children: ReactNode;
    activeTab: string;
  }) => (
    <div data-testid="tabbed-page" data-active={activeTab}>
      {children}
    </div>
  ),
}));
vi.mock("./useIssuerTier", () => ({
  useIssuerTier: (tier: "organization" | "project") => ({
    items: tiers[tier],
    isLoading: false,
    isError: false,
    retry: () => undefined,
  }),
  retryFailedTiers: () => undefined,
}));

afterEach(cleanup);

const issuer = (slug: string, projectId: string) => ({
  id: `usi_${slug}`,
  slug,
  projectId,
  projectName: projectId ? "Some project" : "",
  issuerUrl: "",
  mode: "per_server",
});

it("lists the organization-wide providers and this project's in one list by name, without other projects'", () => {
  tiers.organization = [issuer("workforce", "")];
  tiers.project = [
    issuer("linear", "project_a"),
    issuer("acme", "project_a"),
    issuer("elsewhere", "project_b"),
  ];
  render(
    <MemoryRouter>
      <McpSessionsConfiguration />
    </MemoryRouter>,
  );

  expect(screen.getByTestId("tabbed-page").getAttribute("data-active")).toBe(
    "configuration",
  );
  const rows = screen
    .getAllByRole("link", { name: /^View MCP Authentication Provider / })
    .map((row) => row.getAttribute("aria-label"));
  expect(rows).toEqual([
    "View MCP Authentication Provider acme",
    "View MCP Authentication Provider linear",
    "View MCP Authentication Provider workforce",
  ]);
  const workforce = screen.getByText("workforce").closest("tr") as HTMLElement;
  expect(within(workforce).getByText("Organization-wide")).toBeTruthy();
  const linear = screen.getByText("linear").closest("tr") as HTMLElement;
  expect(within(linear).getByText("This project")).toBeTruthy();
});
