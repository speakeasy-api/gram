import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import { ConnectionsTab } from "./ConnectionsTab";

const state = vi.hoisted(() => ({
  project: { id: "project_a", slug: "first" },
  listParams: [] as unknown[],
  filterValues: { subjectUrn: "user:u1" } as Record<string, unknown>,
  clients: [] as { id: string; clientName: string }[],
  listedClients: undefined as unknown[] | undefined,
}));
vi.mock("@/contexts/Auth", () => ({ useProject: () => state.project }));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "first" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => false }),
}));
vi.mock("@/hooks/useReadableAgents", () => ({
  useReadableAgents: () => ({ data: [], isError: false }),
}));
vi.mock("@/components/filters", () => ({
  defineFilters: (filters: unknown) => filters,
  useFilterState: () => ({ values: state.filterValues }),
}));
vi.mock("@gram/client/react-query/userSessionFacets.js", () => ({
  useUserSessionFacets: () => ({ data: {} }),
}));
vi.mock("@gram/client/react-query/userSessions.js", () => ({
  useUserSessionsInfinite: (params: unknown) => {
    state.listParams.push(params);
    return { data: { pages: [] } };
  },
}));
vi.mock("@gram/client/react-query/userSessionClients.js", () => ({
  useUserSessionClientsInfinite: () => ({
    data: { pages: [{ result: { items: state.clients } }] },
  }),
}));
vi.mock("@/components/sessions/RegisterClientAction", () => ({
  RegisterClientAction: ({ issuerId }: { issuerId: string }) => (
    <button type="button">Register OAuth client for {issuerId}</button>
  ),
}));
vi.mock("@/components/connections/ConnectionsList", () => ({
  ConnectionsList: ({ clients }: { clients: unknown[] }) => {
    state.listedClients = clients;
    return <div>Connections list</div>;
  },
}));
vi.mock("@/components/page-layout", () => ({
  Page: {
    Toolbar: Object.assign(
      ({ children }: { children: ReactNode }) => <div>{children}</div>,
      {
        Search: () => <input aria-label="Search connections" />,
        Filters: () => null,
        Refresh: () => null,
      },
    ),
  },
}));

afterEach(() => {
  cleanup();
  state.listParams = [];
  state.filterValues = { subjectUrn: "user:u1" };
  state.clients = [];
  state.listedClients = undefined;
});

it("lists only the chosen provider's connections, keeping the query's filters", () => {
  render(
    <MemoryRouter>
      <ConnectionsTab providerId="issuer_1" />
    </MemoryRouter>,
  );
  expect(state.listParams.at(-1)).toMatchObject({
    gramProject: "first",
    userSessionIssuerId: "issuer_1",
    subjectUrn: "user:u1",
  });
  expect(
    screen.getByRole("textbox", { name: "Search connections" }),
  ).toBeTruthy();
});

it("offers registering an OAuth client against the chosen provider", () => {
  render(
    <MemoryRouter>
      <ConnectionsTab providerId="issuer_1" />
    </MemoryRouter>,
  );
  expect(
    screen.getByRole("button", { name: "Register OAuth client for issuer_1" }),
  ).toBeTruthy();
});

it("lists a registered agent that has not signed in yet under the agent grouping", () => {
  state.filterValues = {};
  state.clients = [{ id: "client_1", clientName: "Support triage agent" }];
  render(
    <MemoryRouter>
      <ConnectionsTab providerId="issuer_1" />
    </MemoryRouter>,
  );

  // Grouped by person, there is no session to list, and nothing was searched.
  expect(
    screen.getByText(/No one has signed in through this provider yet/),
  ).toBeTruthy();
  expect(screen.queryByText("No connections match your search")).toBeNull();

  fireEvent.click(screen.getByText("Agent"));
  expect(screen.getByText("Connections list")).toBeTruthy();
  expect(state.listedClients).toEqual(state.clients);
});
