import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { ClientsAndSessionsTab } from "./ClientsAndSessionsTab";

const {
  useUserSessionClientsInfinite,
  useUserSessionsInfinite,
  invalidateAllUserSessionClients,
  revokeSessionMutate,
} = vi.hoisted(() => ({
  useUserSessionClientsInfinite: vi.fn(),
  useUserSessionsInfinite: vi.fn(),
  invalidateAllUserSessionClients: vi.fn(),
  revokeSessionMutate: vi.fn(),
}));

vi.mock("@gram/client/react-query/userSessionClients.js", () => ({
  useUserSessionClientsInfinite: (...args: unknown[]) =>
    useUserSessionClientsInfinite(...args),
  invalidateAllUserSessionClients: (...args: unknown[]) =>
    invalidateAllUserSessionClients(...args),
}));

vi.mock("@gram/client/react-query/userSessions.js", () => ({
  useUserSessionsInfinite: (...args: unknown[]) =>
    useUserSessionsInfinite(...args),
  invalidateAllUserSessions: vi.fn(),
}));

// The revoke dialogs are mounted (closed) by every row, and their mutation
// hooks reach for the SDK context that only GramProvider supplies.
vi.mock("@gram/client/react-query/revokeUserSessionClient.js", () => ({
  useRevokeUserSessionClientMutation: () => ({
    mutate: (_vars: unknown, opts?: { onSuccess?: () => void }) =>
      opts?.onSuccess?.(),
    isPending: false,
  }),
}));

vi.mock("@gram/client/react-query/revokeUserSession.js", () => ({
  useRevokeUserSessionMutation: () => ({
    mutate: (...args: unknown[]) => revokeSessionMutate(...args),
    isPending: false,
  }),
}));

// Radix's dropdown does not open under happy-dom's pointer-event model, so
// the action menu is stubbed to plain buttons. The behaviour under test is the
// tab's filter wiring, not Radix.
vi.mock("@/components/ui/MoreActions", () => ({
  MoreActions: ({
    actions,
  }: {
    actions: { label: string; onClick: () => void }[];
  }) => (
    <>
      {actions.map((action) => (
        <button key={action.label} onClick={action.onClick}>
          {action.label}
        </button>
      ))}
    </>
  ),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true, hasAnyScope: () => true }),
}));

// Reaches for the org route helpers and the PostHog flag context, neither of
// which this test mounts. Its own gating is covered in
// ViewOrgSessionsButton.test.tsx.
vi.mock("./ViewOrgSessionsButton", () => ({
  ViewOrgSessionsButton: () => null,
}));

vi.mock("./RevokeClientDialog", () => ({
  RevokeClientDialog: ({
    open,
    onRevoked,
  }: {
    open: boolean;
    onRevoked: () => void;
  }) => (open ? <button onClick={onRevoked}>Confirm revoke</button> : null),
}));

vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project-1", slug: "project-1" }),
}));

function client(overrides: Record<string, unknown>) {
  return {
    id: "client-1",
    userSessionIssuerId: "issuer-1",
    clientId: "abc123",
    clientName: "Test Client",
    redirectUris: [],
    clientIdIssuedAt: new Date("2026-01-01T00:00:00Z"),
    createdAt: new Date("2026-01-01T00:00:00Z"),
    updatedAt: new Date("2026-01-01T00:00:00Z"),
    activeSessionCount: 0,
    ...overrides,
  };
}

function session(overrides: Record<string, unknown>) {
  return {
    id: "session-1",
    userSessionIssuerId: "issuer-1",
    userSessionClientId: "client-1",
    issuerSlug: "the-server",
    jti: "jti-1",
    subjectUrn: "user:u1",
    subjectType: "user",
    subjectDisplayName: "Ada Lovelace",
    clientName: "Test Client",
    expiresAt: new Date("2027-01-01T00:00:00Z"),
    refreshExpiresAt: new Date("2027-01-01T00:00:00Z"),
    createdAt: new Date("2026-01-01T00:00:00Z"),
    updatedAt: new Date("2026-01-01T00:00:00Z"),
    ...overrides,
  };
}

// Text of the metric card carrying `subtext`. Walks up from the subtext to the
// nearest ancestor that also holds the card title, since the titles collide
// with the table column and section headings of the same name.
function metricCard(subtext: string, title: string): string {
  let node: HTMLElement | null = screen.getByText(subtext);
  while (node && !node.textContent?.includes(title)) {
    node = node.parentElement;
  }
  if (!node) throw new Error(`No metric card titled "${title}"`);
  return node.textContent ?? "";
}

// Data-row order as one table on the tab renders it, for the sort and filter
// assertions. Located by a column only that table has, since indexing into
// getAllByRole("table") shifts as soon as the other table is empty.
function rowTexts(table: "sessions" | "clients"): string[] {
  const columnLabel = table === "sessions" ? "Subject" : "Active sessions";
  const found = screen
    .getAllByRole("table")
    .find((candidate) => within(candidate).queryByText(columnLabel));
  if (!found) throw new Error(`No ${table} table rendered`);
  // The header row is dropped so the indices line up with the data.
  return within(found)
    .getAllByRole("row")
    .slice(1)
    .map((row) => row.textContent ?? "");
}

function queryResult(
  items: unknown[],
  overrides: Record<string, unknown> = {},
) {
  return {
    data: { pages: [{ result: { items } }] },
    isPending: false,
    isError: false,
    hasNextPage: false,
    fetchNextPage: vi.fn(),
    isFetchingNextPage: false,
    refetch: vi.fn(),
    ...overrides,
  };
}

// The app mounts one TooltipProvider at the root (see App.tsx); the source
// badge relies on it, so tests supply their own rather than adding a nested
// provider to the component.
function wrap(ui: React.ReactElement, initialEntries: string[] = ["/"]) {
  // The tab invalidates session queries after a revoke, so it needs a real
  // QueryClient even though every data hook is mocked.
  return (
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter initialEntries={initialEntries}>
        <TooltipProvider>{ui}</TooltipProvider>
      </MemoryRouter>
    </QueryClientProvider>
  );
}

function renderTab(ui: React.ReactElement, initialEntries?: string[]) {
  return render(wrap(ui, initialEntries));
}

describe("ClientsAndSessionsTab", () => {
  beforeEach(() => {
    useUserSessionsInfinite.mockReturnValue(queryResult([]));
    useUserSessionClientsInfinite.mockReturnValue(queryResult([]));
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("explains why there is nothing to show when no issuer is attached", () => {
    renderTab(<ClientsAndSessionsTab issuerId={undefined} />);

    expect(screen.getByText(/has no session issuer/i)).toBeDefined();
    // Without an issuer to scope to, the listing endpoints would fall back to
    // the whole project, so both queries have to be disabled rather than
    // merely unrendered.
    expect(useUserSessionClientsInfinite).toHaveBeenCalledWith(
      expect.anything(),
      undefined,
      expect.objectContaining({ enabled: false }),
    );
    expect(useUserSessionsInfinite).toHaveBeenCalledWith(
      expect.anything(),
      undefined,
      expect.objectContaining({ enabled: false }),
    );
  });

  it("renders skeletons while the clients list loads", () => {
    useUserSessionClientsInfinite.mockReturnValue(
      queryResult([], { isPending: true, data: undefined }),
    );

    const { container } = renderTab(
      <ClientsAndSessionsTab issuerId="issuer-1" />,
    );

    expect(container.querySelectorAll(".skeleton").length).toBeGreaterThan(0);
  });

  it("shows an empty state when the issuer has no clients", () => {
    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);

    expect(
      screen.getByText("No clients have registered with this server yet"),
    ).toBeDefined();
  });

  it("offers a retry when the clients list fails to load", () => {
    const refetch = vi.fn();
    useUserSessionClientsInfinite.mockReturnValue(
      queryResult([], { isError: true, data: undefined, refetch }),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    expect(refetch).toHaveBeenCalled();
  });

  // The whole point of the issue: the two registration modes have to be
  // distinguishable at a glance, and the distinction has to come from the
  // backend field rather than from re-parsing client_id.
  it("distinguishes CIMD clients from DCR clients", () => {
    useUserSessionClientsInfinite.mockReturnValue(
      queryResult([
        client({ id: "dcr-1", clientName: "DCR Client" }),
        client({
          id: "cimd-1",
          clientName: "CIMD Client",
          clientId: "https://client.example.com/oauth.json",
          clientIdMetadataUri: "https://client.example.com/oauth.json",
        }),
      ]),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);

    // Scoped per row: asserting the two strings merely exist would still pass
    // if the badges were swapped between rows.
    const dcrRow = screen.getByText("DCR Client").closest("tr");
    const cimdRow = screen.getByText("CIMD Client").closest("tr");
    expect(within(dcrRow as HTMLElement).getByText("DCR")).toBeDefined();
    expect(within(cimdRow as HTMLElement).getByText("CIMD")).toBeDefined();
  });

  // A DCR client has no document origin, so the secondary line falls back to
  // the client_id Gram minted, which operators correlate against logs.
  it("labels a DCR client with its client_id", () => {
    useUserSessionClientsInfinite.mockReturnValue(
      queryResult([client({ id: "dcr-1", clientId: "minted-client-id" })]),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);

    expect(screen.getByText("minted-client-id")).toBeDefined();
  });

  // The origin parse has to fail closed rather than throw: client_id_metadata_uri
  // is validated server-side, but the view must not blow up on a bad value.
  it("falls back to the client_id when the metadata URI will not parse", () => {
    useUserSessionClientsInfinite.mockReturnValue(
      queryResult([
        client({
          id: "cimd-bad",
          clientId: "not-a-url",
          clientIdMetadataUri: "not-a-url",
        }),
      ]),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);

    expect(screen.getByText("CIMD")).toBeDefined();
    expect(screen.getByText("not-a-url")).toBeDefined();
  });

  // client_name is chosen by the client and verified by nobody, so the CIMD
  // document origin is what an operator can actually trust.
  it("shows the metadata document origin for a CIMD client", () => {
    useUserSessionClientsInfinite.mockReturnValue(
      queryResult([
        client({
          id: "cimd-1",
          clientName: "Totally Legit Agent",
          clientId: "https://evil.example.com/oauth.json",
          clientIdMetadataUri: "https://evil.example.com/oauth.json",
        }),
      ]),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);

    expect(screen.getByText("https://evil.example.com")).toBeDefined();
  });

  it("clears a client filter that pointed at a different issuer", () => {
    useUserSessionClientsInfinite.mockReturnValue(
      queryResult([client({ id: "client-1", clientName: "Server A Client" })]),
    );

    const { rerender } = renderTab(
      <ClientsAndSessionsTab issuerId="issuer-1" />,
    );
    fireEvent.click(screen.getByRole("button", { name: "View sessions" }));
    expect(screen.getByText(/Filtered to Server A Client/)).toBeDefined();

    // Navigating to a server backed by a different issuer reuses this
    // component instance; the filter must not carry over.
    rerender(wrap(<ClientsAndSessionsTab issuerId="issuer-2" />));

    expect(screen.queryByText(/Filtered to/)).toBeNull();
    expect(useUserSessionsInfinite).toHaveBeenLastCalledWith(
      expect.objectContaining({ userSessionIssuerId: "issuer-2" }),
      undefined,
      expect.objectContaining({ enabled: true }),
    );
  });

  // A failed page-2 fetch must not take the rows already on screen with it.
  it("keeps loaded clients visible when a later page fails", () => {
    useUserSessionClientsInfinite.mockReturnValue(
      queryResult([client({ id: "client-1", clientName: "Loaded Client" })], {
        isError: true,
      }),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);

    expect(screen.getByText("Loaded Client")).toBeDefined();
    expect(screen.queryByText("Couldn't load clients.")).toBeNull();
  });

  // Drilling in condenses the page rather than scrolling to the sessions: the
  // clients table collapses to the selected row, so the (also narrowed)
  // sessions listing above it comes into view.
  it("narrows the clients table to the client being drilled into", () => {
    useUserSessionClientsInfinite.mockReturnValue(
      queryResult([
        client({ id: "client-1", clientName: "Filtered Client" }),
        client({ id: "client-2", clientName: "Other Client" }),
      ]),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);

    const filteredRow = screen.getByText("Filtered Client").closest("tr");
    fireEvent.click(
      within(filteredRow as HTMLElement).getByRole("button", {
        name: "View sessions",
      }),
    );

    expect(screen.getByText("Filtered Client")).toBeDefined();
    expect(screen.queryByText("Other Client")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Clear filter" }));

    expect(screen.getByText("Other Client")).toBeDefined();
  });

  // Revoking the client the page is drilled into leaves nothing to show, so
  // the filter has to lift rather than stranding the operator on an empty
  // table with no way back.
  it("clears the filter when the client it points at is revoked", () => {
    useUserSessionClientsInfinite.mockReturnValue(
      queryResult([
        client({ id: "client-1", clientName: "Filtered Client" }),
        client({ id: "client-2", clientName: "Other Client" }),
      ]),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);

    const filteredRow = screen.getByText("Filtered Client").closest("tr");
    fireEvent.click(
      within(filteredRow as HTMLElement).getByRole("button", {
        name: "View sessions",
      }),
    );
    expect(screen.getByText(/Filtered to Filtered Client/)).toBeDefined();

    fireEvent.click(screen.getByRole("button", { name: "Revoke" }));
    fireEvent.click(screen.getByRole("button", { name: "Confirm revoke" }));

    expect(screen.queryByText(/Filtered to/)).toBeNull();
    expect(screen.getByText("Other Client")).toBeDefined();
  });

  // The clients table reports a live-session tally per client, so a session
  // revoked from the table above it leaves that count over-reporting until the
  // clients query is invalidated too.
  it("refreshes the clients listing after a session is revoked", () => {
    revokeSessionMutate.mockImplementation(
      (_vars: unknown, opts?: { onSuccess?: () => void }) =>
        opts?.onSuccess?.(),
    );
    useUserSessionsInfinite.mockReturnValue(
      queryResult([session({ id: "session-1" })]),
    );
    useUserSessionClientsInfinite.mockReturnValue(
      queryResult([
        client({
          id: "client-1",
          clientName: "Test Client",
          activeSessionCount: 1,
        }),
      ]),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);

    const sessionRow = screen.getByText("Ada Lovelace").closest("tr");
    fireEvent.click(
      within(sessionRow as HTMLElement).getByRole("button", { name: "Revoke" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Revoke" }));

    expect(invalidateAllUserSessionClients).toHaveBeenCalled();
  });

  // The count is the same drill-down the kebab menu offers, so an operator
  // reading "3 sessions" can click straight through to them.
  it("drills into a client's sessions from its active session count", () => {
    useUserSessionClientsInfinite.mockReturnValue(
      queryResult([
        client({
          id: "client-1",
          clientName: "Busy Client",
          activeSessionCount: 3,
        }),
      ]),
    );
    useUserSessionsInfinite.mockReturnValue(
      queryResult([
        session({
          id: "session-mine",
          userSessionClientId: "client-1",
          subjectDisplayName: "Mine",
        }),
        session({
          id: "session-theirs",
          userSessionClientId: "client-2",
          subjectDisplayName: "Theirs",
        }),
      ]),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);

    fireEvent.click(
      screen.getByRole("button", {
        name: "View 3 active sessions for Busy Client",
      }),
    );

    expect(rowTexts("sessions")).toHaveLength(1);
    expect(rowTexts("sessions")[0]).toContain("Mine");
  });

  // A client holding no sessions has nothing to drill into, so its zero must
  // not masquerade as a control.
  it("leaves a zero session count inert", () => {
    useUserSessionClientsInfinite.mockReturnValue(
      queryResult([
        client({
          id: "client-1",
          clientName: "Idle Client",
          activeSessionCount: 0,
        }),
      ]),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);

    expect(rowTexts("clients")[0]).toContain("Idle Client");
    expect(
      screen.queryByRole("button", { name: /active sessions for Idle Client/ }),
    ).toBeNull();
  });

  it("summarizes both listings in the metric cards", () => {
    useUserSessionsInfinite.mockReturnValue(
      queryResult([session({ id: "s1" }), session({ id: "s2" })]),
    );
    useUserSessionClientsInfinite.mockReturnValue(
      queryResult([client({ id: "c1" })]),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);

    expect(
      metricCard("Currently authenticated MCP sessions", "Active sessions"),
    ).toContain("2");
    expect(metricCard("Registered against this server", "Clients")).toContain(
      "1",
    );
  });

  // The rows are all in memory, so the table pages over them rather than
  // fetching more.
  it("pages the sessions table ten rows at a time", () => {
    useUserSessionsInfinite.mockReturnValue(
      queryResult(
        Array.from({ length: 12 }, (_, i) =>
          session({
            id: `session-${i}`,
            subjectDisplayName: `User ${String(i).padStart(2, "0")}`,
          }),
        ),
      ),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);

    expect(rowTexts("sessions")).toHaveLength(10);
    expect(screen.getByText("1–10 of 12")).toBeDefined();

    fireEvent.click(screen.getByRole("button", { name: "Next page" }));

    expect(rowTexts("sessions")).toHaveLength(2);
    expect(screen.getByText("11–12 of 12")).toBeDefined();
  });

  it("narrows the sessions table by created window", () => {
    const now = Date.now();
    useUserSessionsInfinite.mockReturnValue(
      queryResult([
        session({
          id: "fresh",
          subjectDisplayName: "Fresh",
          createdAt: new Date(now - 60 * 60 * 1000),
        }),
        session({
          id: "stale",
          subjectDisplayName: "Stale",
          createdAt: new Date(now - 40 * 24 * 60 * 60 * 1000),
        }),
      ]),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);
    expect(rowTexts("sessions")).toHaveLength(2);
    cleanup();

    // useFilterState reads its dimensions off the URL, so a seeded param is
    // the same input the chip would produce.
    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />, [
      "/?sessionCreated=24h",
    ]);

    expect(rowTexts("sessions")).toHaveLength(1);
    expect(rowTexts("sessions")[0]).toContain("Fresh");
  });

  // A user subject gets an avatar (initials until the photo loads); an API key
  // has no directory identity, so it must not be given fake initials.
  it("gives user subjects an avatar and other subjects a glyph", () => {
    useUserSessionsInfinite.mockReturnValue(
      queryResult([
        session({
          id: "human",
          subjectType: "user",
          subjectDisplayName: "Ada Lovelace",
          subjectPhotoUrl: "https://example.com/ada.png",
        }),
        session({
          id: "robot",
          subjectType: "apikey",
          subjectDisplayName: undefined,
          subjectUrn: "apikey:abc",
        }),
      ]),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);

    const humanRow = screen.getByText("Ada Lovelace").closest("tr");
    expect(within(humanRow as HTMLElement).getByText("AL")).toBeDefined();

    const keyRow = screen.getByText("API key").closest("tr");
    expect(within(keyRow as HTMLElement).queryByText("AK")).toBeNull();
  });

  // Two "filter by client" controls would silently AND together, emptying the
  // table while the banner above still named the first client.
  it("drops the client filter chip while drilled into a client", () => {
    useUserSessionClientsInfinite.mockReturnValue(
      queryResult([client({ id: "client-1", clientName: "Test Client" })]),
    );
    useUserSessionsInfinite.mockReturnValue(
      queryResult([session({ id: "s1", userSessionClientId: "client-1" })]),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);
    expect(screen.getByRole("button", { name: /All clients/ })).toBeDefined();

    fireEvent.click(
      screen.getAllByRole("button", { name: "View sessions" })[0]!,
    );

    expect(screen.queryByRole("button", { name: /All clients/ })).toBeNull();
  });

  // Both listings page in id order, which is meaningless to an operator
  // scanning for a client by name. Sorting is client-side over the rows already
  // loaded, matching the loaded-count semantics of the Load more button.
  it("sorts the clients table by name in both directions", () => {
    useUserSessionClientsInfinite.mockReturnValue(
      queryResult([
        client({ id: "client-1", clientName: "Zeta" }),
        client({ id: "client-2", clientName: "Alpha" }),
      ]),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);
    expect(rowTexts("clients")[0]).toContain("Zeta");

    fireEvent.click(
      screen.getByRole("button", { name: "Sort by Client ascending" }),
    );
    expect(rowTexts("clients")[0]).toContain("Alpha");

    fireEvent.click(
      screen.getByRole("button", { name: "Sort by Client descending" }),
    );
    expect(rowTexts("clients")[0]).toContain("Zeta");
  });

  it("sorts the sessions table by expiry", () => {
    useUserSessionsInfinite.mockReturnValue(
      queryResult([
        session({
          id: "session-late",
          subjectDisplayName: "Later",
          refreshExpiresAt: new Date("2027-06-01T00:00:00Z"),
        }),
        session({
          id: "session-soon",
          subjectDisplayName: "Sooner",
          refreshExpiresAt: new Date("2027-02-01T00:00:00Z"),
        }),
      ]),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);
    expect(rowTexts("sessions")[0]).toContain("Later");

    fireEvent.click(
      screen.getByRole("button", { name: "Sort by Expires ascending" }),
    );
    expect(rowTexts("sessions")[0]).toContain("Sooner");
  });

  // The listing is already scoped to one server, so naming that server on every
  // row was noise the tab no longer carries.
  it("does not repeat the issuer on every session row", () => {
    useUserSessionsInfinite.mockReturnValue(
      queryResult([session({ id: "session-1" })]),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);

    expect(screen.getByText("Ada Lovelace")).toBeDefined();
    expect(screen.queryByText(/the-server/)).toBeNull();
  });

  // The sessions are all in memory, so drilling in narrows the rendered rows
  // rather than refetching with a clientId param.
  it("scopes the sessions list to a client picked from the clients list", () => {
    useUserSessionClientsInfinite.mockReturnValue(
      queryResult([client({ id: "client-1", clientName: "Test Client" })]),
    );
    useUserSessionsInfinite.mockReturnValue(
      queryResult([
        session({
          id: "session-mine",
          userSessionClientId: "client-1",
          subjectDisplayName: "Mine",
        }),
        session({
          id: "session-theirs",
          userSessionClientId: "client-2",
          subjectDisplayName: "Theirs",
        }),
      ]),
    );

    renderTab(<ClientsAndSessionsTab issuerId="issuer-1" />);
    expect(rowTexts("sessions")).toHaveLength(2);

    fireEvent.click(
      screen.getAllByRole("button", { name: "View sessions" })[0]!,
    );

    expect(rowTexts("sessions")).toHaveLength(1);
    expect(rowTexts("sessions")[0]).toContain("Mine");
    expect(screen.getByText(/Filtered to Test Client/)).toBeDefined();
  });
});
