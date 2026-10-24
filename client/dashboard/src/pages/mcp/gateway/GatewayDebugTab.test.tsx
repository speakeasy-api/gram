import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Component, type ReactNode } from "react";
import { MemoryRouter, useLocation } from "react-router";
import { afterEach, beforeAll, expect, it, vi } from "vitest";
import { Operator as Op } from "@gram/client/models/components/logfilter";
import { serializeFilters } from "@/pages/logs/log-filter-url";
import { encodeValues } from "./sessionFilters";
import { GatewayDebugTab } from "./GatewayDebugTab";

type Request = Record<string, unknown>;
type Page = { result: { items: { id: string }[] }; "~next"?: string };
type Value = {
  value: string;
  displayName?: string;
  count: number;
  matchesSearch: boolean;
};
type Facet = { values: Value[]; distinctCount: number };

const server = vi.hoisted(() => ({
  list: vi.fn<(request: Request, cursor?: string) => Promise<Page>>(),
  facets: vi.fn<(request: Request) => Promise<Record<string, unknown>>>(),
  agents: [] as { id: string; name: string }[],
  agentsEnabled: [] as boolean[],
}));

vi.mock("@gram/client/react-query/userSessions.js", async () => {
  const { useInfiniteQuery } = await import("@tanstack/react-query");
  return {
    useUserSessionsInfinite: (
      request: Request,
      _security: unknown,
      options: Request,
    ) =>
      useInfiniteQuery({
        queryKey: ["user-sessions", request],
        queryFn: ({ pageParam }: { pageParam: string | undefined }) =>
          server.list(request, pageParam),
        initialPageParam: undefined as string | undefined,
        getNextPageParam: (page: Page) => page["~next"],
        ...options,
      }),
  };
});
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("@gram/client/react-query/userSessionFilterFacets.core.js", () => ({
  buildUserSessionFilterFacetsQuery: (_client: unknown, request: Request) => ({
    queryKey: ["facets", request],
    queryFn: () => server.facets(request),
  }),
}));
vi.mock("@/hooks/useReadableAgents", () => ({
  useReadableAgents: (enabled: boolean) => {
    server.agentsEnabled.push(enabled);
    return { data: enabled ? server.agents : undefined, isError: false };
  },
}));
vi.mock("@/hooks/useSessionAgents", () => ({
  useSessionAgents: (sessions: unknown[]) => sessions,
}));
vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project_a", slug: "first" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => false }),
}));
vi.mock("@/components/connections/ConnectionsList", () => ({
  ConnectionsList: ({
    sessions,
    grouping,
    groupingLabels,
  }: {
    sessions: { id: string }[];
    grouping: string;
    groupingLabels: Record<string, string>;
  }) => (
    <div>
      <span data-testid="grouping-header">{groupingLabels[grouping]}</span>
      <ul>
        {sessions.map((s) => (
          <li key={s.id}>{s.id}</li>
        ))}
      </ul>
    </div>
  ),
}));
vi.mock("@/components/page-layout", () => ({
  Page: {
    Toolbar: Object.assign(
      ({ children }: { children: ReactNode }) => <div>{children}</div>,
      {
        Leading: ({ children }: { children: ReactNode }) => <>{children}</>,
        Refresh: ({ onRefresh }: { onRefresh: () => void }) => (
          <button type="button" onClick={onRefresh}>
            Refresh
          </button>
        ),
      },
    ),
  },
}));

beforeAll(() => {
  globalThis.ResizeObserver ??= class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
  if (!("scrollIntoView" in Element.prototype)) {
    Object.defineProperty(Element.prototype, "scrollIntoView", {
      value: () => {},
    });
  }
});

afterEach(() => {
  cleanup();
  server.list.mockReset();
  server.facets.mockReset();
  server.agents = [];
  server.agentsEnabled = [];
});

class Boundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  render() {
    return this.state.failed ? <p>error boundary</p> : this.props.children;
  }
}

function Location() {
  const location = useLocation();
  return <output data-testid="location">{location.search}</output>;
}

function renderAt(path = "/debug") {
  const client = new QueryClient({
    defaultOptions: { queries: { throwOnError: true, retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        <Boundary>
          <GatewayDebugTab gatewayId="gateway-1" />
        </Boundary>
        <Location />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const page = (ids: string[], next?: string): Page => ({
  result: { items: ids.map((id) => ({ id })) },
  "~next": next,
});
const value = (
  v: string,
  count = 1,
  displayName?: string,
  matchesSearch = true,
): Value => ({ value: v, count, displayName, matchesSearch });
const facet = (values: Value[] = [], distinctCount = values.length): Facet => ({
  values,
  distinctCount,
});
const facets = (over: Record<string, unknown> = {}) => ({
  identity: facet(),
  identityType: facet(),
  mcpServer: facet(),
  client: facet(),
  clientRegistration: facet(),
  unattributedCount: 0,
  ...over,
});

const ISSUER = "0192f4c8-1a2b-7c3d-8e4f-5a6b7c8d9e01";
const FORMER = "0192f4c8-1a2b-7c3d-8e4f-5a6b7c8d9e02";
const CLIENT = "0192f4c8-1a2b-7c3d-8e4f-5a6b7c8d9e03";
const OTHER_CLIENT = "0192f4c8-1a2b-7c3d-8e4f-5a6b7c8d9e04";
const THIRD_CLIENT = "0192f4c8-1a2b-7c3d-8e4f-5a6b7c8d9e05";

const bar = () => screen.getByRole("combobox");
const location = () => screen.getByTestId("location").textContent ?? "";
const params = () => new URLSearchParams(location());
const lastList = () => server.list.mock.calls.at(-1)![0];

function pick(label: string) {
  const option = screen
    .getAllByRole("option")
    .find((o) =>
      [...o.querySelectorAll("span"), o].some((el) => el.textContent === label),
    );
  if (!option) throw new Error(`no option ${label}`);
  fireEvent.click(option);
}

function pickField(label: string) {
  fireEvent.focus(bar());
  pick(label);
}

function openGroup(label: string) {
  fireEvent.click(screen.getByRole("button", { name: label, expanded: false }));
}

function submitSearch(text: string) {
  fireEvent.change(bar(), { target: { value: text } });
  fireEvent.keyDown(bar(), { key: "Enter" });
}

it("scopes every list and facet request to the gateway", async () => {
  server.list.mockResolvedValue(page(["s1"]));
  server.facets.mockResolvedValue(
    facets({
      client: facet([value(CLIENT, 1, "Cursor"), value("dashboard")], 30),
    }),
  );
  renderAt("/debug?q=dana");
  expect(await screen.findByText("s1")).toBeTruthy();
  expect(
    screen.getByText(
      "Sessions issued for this gateway, identified by their recorded origin or consent selection. Unattributed sessions are not shown.",
    ),
  ).toBeTruthy();
  expect(lastList()).toMatchObject({
    gramProject: "first",
    metaMcpServerId: "gateway-1",
    search: "dana",
  });

  openGroup("Client");
  fireEvent.change(await screen.findByLabelText("Filter Client"), {
    target: { value: "cur" },
  });
  await waitFor(() =>
    expect(server.facets).toHaveBeenCalledWith(
      expect.objectContaining({ field: "client", fieldSearch: "cur" }),
    ),
  );
  fireEvent.click(screen.getByLabelText("Clear Client filter"));
  fireEvent.click(await screen.findByText("Cursor"));
  await waitFor(() => expect(lastList()).toMatchObject({ clients: [CLIENT] }));
  fireEvent.click(screen.getByRole("button", { name: "Clear all filters" }));
  await waitFor(() => expect(lastList().clients).toBeUndefined());

  for (const [request] of [
    ...server.list.mock.calls,
    ...server.facets.mock.calls,
  ]) {
    expect(request).toMatchObject({ metaMcpServerId: "gateway-1" });
  }
});

it("says when nothing has been recorded for the gateway", async () => {
  server.list.mockResolvedValue(page([]));
  server.facets.mockResolvedValue(facets());
  renderAt();
  expect(
    await screen.findByText(
      "No connections have been recorded for this gateway yet",
    ),
  ).toBeTruthy();
});

it("shows a failed first load inline and recovers on retry", async () => {
  server.facets.mockResolvedValue(facets());
  server.list.mockRejectedValueOnce(new Error("500"));
  renderAt();
  expect(await screen.findByText("Couldn't load connections.")).toBeTruthy();
  expect(screen.queryByText("error boundary")).toBeNull();

  server.list.mockResolvedValueOnce(page(["s1"]));
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  expect(await screen.findByText("s1")).toBeTruthy();
});

it("keeps rows through a failed refresh and recovers on retry", async () => {
  server.facets.mockResolvedValue(facets());
  server.list.mockResolvedValueOnce(page(["s1"]));
  renderAt();
  expect(await screen.findByText("s1")).toBeTruthy();

  server.list.mockRejectedValueOnce(new Error("500"));
  fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
  expect(await screen.findByText("Couldn’t refresh connections.")).toBeTruthy();
  expect(screen.getByText("s1")).toBeTruthy();

  server.list.mockResolvedValueOnce(page(["s1-updated"]));
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  expect(await screen.findByText("s1-updated")).toBeTruthy();
  expect(screen.queryByText("error boundary")).toBeNull();
});

it("keeps rows through a failed next page and recovers on retry", async () => {
  server.facets.mockResolvedValue(facets());
  server.list.mockResolvedValueOnce(page(["s1"], "cursor-2"));
  renderAt();
  expect(await screen.findByText("s1")).toBeTruthy();

  server.list.mockRejectedValueOnce(new Error("500"));
  fireEvent.click(screen.getByRole("button", { name: "Load more" }));
  expect(
    await screen.findByText("Couldn’t load more connections."),
  ).toBeTruthy();

  server.list.mockResolvedValueOnce(page(["s2"]));
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  expect(await screen.findByText("s2")).toBeTruthy();
  expect(screen.getByText("s1")).toBeTruthy();
  expect(server.list.mock.calls.map(([, cursor]) => cursor)).toEqual([
    undefined,
    "cursor-2",
    "cursor-2",
  ]);
});

it("keeps facet failures inline, with rows and filters, and recovers on retry", async () => {
  server.list.mockResolvedValue(page(["s1"]));
  server.facets.mockRejectedValueOnce(new Error("500"));
  renderAt(`/debug?sf=client:eq:dashboard`);
  expect(await screen.findByText("Couldn't load filter values.")).toBeTruthy();
  expect(screen.getByText("s1")).toBeTruthy();
  expect(screen.queryByText("error boundary")).toBeNull();
  expect(
    screen.getByRole("button", { name: "Edit filter Client" }).textContent,
  ).toBe("Client = Dashboard");
  expect(lastList()).toMatchObject({ clients: ["dashboard"] });

  pickField("Sign-in server");
  pick("=");
  expect(screen.getByText("Couldn't load values.")).toBeTruthy();

  server.facets.mockResolvedValue(
    facets({ mcpServer: facet([value(ISSUER, 1, "linear")]) }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  await waitFor(() =>
    expect(screen.queryByText("Couldn't load filter values.")).toBeNull(),
  );
  expect(screen.getAllByText("linear").length).toBeGreaterThan(0);
  expect(params().get("sf")).toBe("client:eq:dashboard");
});

it("shows a failed field search instead of unsearched values, and retries it", async () => {
  server.list.mockResolvedValue(page(["s1"]));
  server.facets.mockImplementation(async (request) =>
    request.fieldSearch
      ? Promise.reject(new Error("500"))
      : facets({ client: facet([value(CLIENT, 1, "Cursor")]) }),
  );
  renderAt();
  await screen.findByText("s1");

  pickField("Client");
  pick("=");
  fireEvent.change(screen.getByPlaceholderText("value"), {
    target: { value: "wind" },
  });
  expect(screen.getByText("Searching…")).toBeTruthy();
  expect(await screen.findByText("Couldn't search values.")).toBeTruthy();
  expect(screen.queryByText("Cursor")).toBeNull();

  server.facets.mockImplementation(async () =>
    facets({ client: facet([value(OTHER_CLIENT, 1, "Windsurf")]) }),
  );
  fireEvent.click(await screen.findByRole("button", { name: "Retry" }));
  expect(await screen.findByText("Windsurf")).toBeTruthy();
});

it("sends no list or facet request for an overlong search and recovers", async () => {
  server.list.mockResolvedValue(page(["s1"]));
  server.facets.mockResolvedValue(facets());
  const overlong = "a".repeat(201);
  renderAt(`/debug?q=${overlong}`);

  expect(
    screen.getByText("Search must be 200 characters or fewer."),
  ).toBeTruthy();
  expect(screen.getByText(`search: ${overlong}`)).toBeTruthy();
  expect(location()).toBe(`?q=${overlong}`);
  expect(screen.queryByRole("button", { name: "Refresh" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
  expect(server.list).not.toHaveBeenCalled();
  expect(server.facets).not.toHaveBeenCalled();

  submitSearch("dana");
  expect(await screen.findByText("s1")).toBeTruthy();
  expect(lastList()).toMatchObject({ search: "dana" });
  expect(server.facets).toHaveBeenCalledWith(
    expect.objectContaining({ search: "dana", metaMcpServerId: "gateway-1" }),
  );
});

it("counts the limit in code points, not UTF-16 units", async () => {
  server.list.mockResolvedValue(page([]));
  server.facets.mockResolvedValue(facets());
  const astral = "\u{1D49C}";
  renderAt(`/debug?q=${encodeURIComponent(astral.repeat(200))}`);
  await waitFor(() =>
    expect(lastList()).toMatchObject({ search: astral.repeat(200) }),
  );
  cleanup();
  server.list.mockClear();

  renderAt(`/debug?q=${encodeURIComponent(astral.repeat(201))}`);
  expect(
    screen.getByText("Search must be 200 characters or fewer."),
  ).toBeTruthy();
  expect(server.list).not.toHaveBeenCalled();
});

it("keeps an applied search visible and removable while chips are built", async () => {
  server.list.mockResolvedValue(page(["s1"]));
  server.facets.mockResolvedValue(
    facets({ identityType: facet([value("agent", 2)]) }),
  );
  renderAt();
  await screen.findByText("s1");

  submitSearch("dana");
  await waitFor(() => expect(params().get("q")).toBe("dana"));
  expect(screen.getByText("search: dana")).toBeTruthy();
  expect((bar() as HTMLInputElement).value).toBe("");

  pickField("Identity type");
  expect(screen.getByText("search: dana")).toBeTruthy();
  pick("=");
  await waitFor(() => pick("Agent"));

  await waitFor(() =>
    expect(lastList()).toMatchObject({
      search: "dana",
      identityTypes: ["agent"],
    }),
  );
  expect(params().get("q")).toBe("dana");
  expect(params().get("sf")).toBe("identity_type:eq:agent");
  expect(screen.getByText("search: dana")).toBeTruthy();

  fireEvent.change(bar(), { target: { value: "draft" } });
  fireEvent.change(bar(), { target: { value: "" } });
  expect(params().get("q")).toBe("dana");

  fireEvent.click(screen.getByRole("button", { name: "Remove search" }));
  await waitFor(() => expect(params().get("q")).toBeNull());
  expect(params().get("sf")).toBe("identity_type:eq:agent");
  await waitFor(() =>
    expect(lastList()).toMatchObject({ identityTypes: ["agent"] }),
  );
  expect(lastList().search).toBeUndefined();

  submitSearch("lee");
  await waitFor(() => expect(params().get("q")).toBe("lee"));
  fireEvent.click(
    screen.getByRole("button", { name: "Remove filter Identity type" }),
  );
  await waitFor(() => expect(params().get("sf")).toBeNull());
  expect(params().get("q")).toBe("lee");
  expect(lastList()).toMatchObject({
    search: "lee",
    metaMcpServerId: "gateway-1",
  });
});

it("round-trips special characters through the chip parameter", async () => {
  server.list.mockResolvedValue(page(["s1"]));
  const awkward =
    "workload:0192f4c8-1a2b-7c3d-8e4f-5a6b7c8d9e0f:repo:a,b%2F\u{1F600}";
  server.facets.mockResolvedValue(
    facets({ identity: facet([value(awkward, 1, "Awkward workload")]) }),
  );
  renderAt();
  await screen.findByText("s1");

  pickField("Identity");
  pick("in");
  fireEvent.click(await screen.findByText("Awkward workload"));
  await waitFor(() =>
    expect(lastList()).toMatchObject({ identities: [awkward] }),
  );
  const link = location();
  cleanup();

  renderAt(`/debug${link}`);
  await screen.findByText("s1");
  expect(lastList()).toMatchObject({ identities: [awkward] });
  expect(
    screen.getByRole("button", { name: "Edit filter Identity" }).textContent,
  ).toBe("Identity in Awkward workload");

  cleanup();
  const negated = new URLSearchParams({
    sf: serializeFilters([
      { id: "x", path: "identity", op: Op.NotEq, value: awkward },
    ])!,
    q: "50% off",
  });
  renderAt(`/debug?${negated}`);
  await screen.findByText("s1");
  expect(lastList()).toMatchObject({
    excludeIdentities: [awkward],
    search: "50% off",
  });
});

it("reads last used comparisons as one chip without a second operator", async () => {
  server.list.mockResolvedValue(page(["s1"]));
  server.facets.mockResolvedValue(facets());
  renderAt();
  await screen.findByText("s1");

  submitSearch("last_used > 7d");
  await waitFor(() =>
    expect(lastList()).toMatchObject({ lastUsedOlderThan: 604800 }),
  );
  expect(
    screen.getByRole("button", { name: "Edit filter Last used" }).textContent,
  ).toBe("Last used > 7d");
  expect(params().get("q")).toBeNull();

  pickField("Last used");
  expect(screen.queryByText("!=")).toBeNull();
  fireEvent.click(await screen.findByText("No recorded use"));
  await waitFor(() =>
    expect(lastList()).toMatchObject({ lastUsedNever: true }),
  );
  expect(lastList().lastUsedOlderThan).toBeUndefined();
  expect(screen.getByText("Last used: No recorded use")).toBeTruthy();
  expect(screen.queryByText("Last used > 7d")).toBeNull();

  const link = location();
  cleanup();
  renderAt(`/debug${link}`);
  await screen.findByText("s1");
  expect(screen.getByText("Last used: No recorded use")).toBeTruthy();
  expect(lastList()).toMatchObject({ lastUsedNever: true });
});

it("refuses filters that would not apply and ignores them in links", async () => {
  server.list.mockResolvedValue(page(["s1"]));
  server.facets.mockResolvedValue(facets());
  renderAt(
    "/debug?sf=last_used:eq:soon,color:eq:red,last_used:eq:%253E%25203651d,identity_type:eq:constructor,client:eq:dashboard",
  );
  await screen.findByText("s1");
  expect(
    (await screen.findByText(/Removed filters from this link/)).textContent,
  ).toBe(
    "Removed filters from this link that can't apply: Last used = soon; color = red; Last used = > 3651d; Identity type = constructor",
  );
  await waitFor(() => expect(params().get("sf")).toBe("client:eq:dashboard"));
  expect(
    screen.queryByRole("button", { name: "Edit filter Last used" }),
  ).toBeNull();
  expect(
    screen.queryByRole("button", { name: "Edit filter color" }),
  ).toBeNull();
  expect(lastList()).toMatchObject({ clients: ["dashboard"] });
  expect(lastList().lastUsedWithin).toBeUndefined();
  expect(lastList().lastUsedOlderThan).toBeUndefined();

  const requests = server.list.mock.calls.length;
  submitSearch("last_used > 3651d");
  expect(
    screen.getByText("Last used takes < 1h, > 7d (up to 3650d) or never"),
  ).toBeTruthy();
  expect(server.list.mock.calls.length).toBe(requests);
  expect(params().get("q")).toBeNull();

  submitSearch("identity_type = robot");
  expect(screen.getByText("Not a valid identity type value")).toBeTruthy();
  submitSearch("identity_type = toString");
  expect(screen.getByText("Not a valid identity type value")).toBeTruthy();
  expect(params().get("sf")).toBe("client:eq:dashboard");
  expect(lastList().identityTypes).toBeUndefined();
});

it("names readable agents found beyond the loaded rows, and only those", async () => {
  server.list.mockResolvedValue(page(["s1"]));
  server.agents = [{ id: "a-30", name: "Ops Bot" }];
  const crowd = Array.from({ length: 25 }, (_, i) =>
    value(`user:crowd-${i}`, 30 - i, `Crowd ${i}`),
  );
  server.facets.mockImplementation(async (request) =>
    request.fieldSearch === "ops"
      ? facets({
          identity: facet([value("agent:a-30"), value("agent:hidden-1")]),
        })
      : facets({ identity: facet(crowd, 40) }),
  );
  renderAt();
  await screen.findByText("s1");

  pickField("Identity");
  pick("=");
  fireEvent.change(screen.getByPlaceholderText("value"), {
    target: { value: "ops" },
  });
  expect(await screen.findByText("Ops Bot")).toBeTruthy();
  expect(screen.getByText("agent:hidden-1")).toBeTruthy();
  expect(server.agentsEnabled).toContain(true);
  expect(server.facets).toHaveBeenCalledWith(
    expect.objectContaining({
      field: "identity",
      fieldSearch: "ops",
      metaMcpServerId: "gateway-1",
    }),
  );
});

it("searches rail groups and the builder independently", async () => {
  server.list.mockResolvedValue(page(["s1"]));
  server.facets.mockImplementation(async (request) => {
    const many = (prefix: string) =>
      facet(
        Array.from({ length: 12 }, (_, i) => value(`${prefix}${i}`)),
        40,
      );
    if (request.field === "client" && request.fieldSearch === "zed")
      return facets({ client: facet([value(OTHER_CLIENT, 1, "Zed")]) });
    if (request.field === "mcp_server")
      return facets({ mcpServer: facet([value(ISSUER, 1, "linear-search")]) });
    if (request.field === "client" && request.fieldSearch === "cur")
      return facets({
        client: facet([value(THIRD_CLIENT, 1, "Cursor-builder")]),
      });
    return facets({ client: many("c-"), mcpServer: many("m-") });
  });
  renderAt();
  await screen.findByText("s1");
  openGroup("Client");
  openGroup("Sign-in server");
  await screen.findByLabelText("Filter Client");

  fireEvent.change(screen.getByLabelText("Filter Client"), {
    target: { value: "zed" },
  });
  fireEvent.change(screen.getByLabelText("Filter Sign-in server"), {
    target: { value: "lin" },
  });
  pickField("Client");
  pick("=");
  fireEvent.change(screen.getByPlaceholderText("value"), {
    target: { value: "cur" },
  });

  expect(await screen.findByText("Zed")).toBeTruthy();
  expect(await screen.findByText("linear-search")).toBeTruthy();
  expect(await screen.findByText("Cursor-builder")).toBeTruthy();
  expect(
    (screen.getByLabelText("Filter Client") as HTMLInputElement).value,
  ).toBe("zed");
  expect(
    (screen.getByLabelText("Filter Sign-in server") as HTMLInputElement).value,
  ).toBe("lin");
  expect((screen.getByPlaceholderText("value") as HTMLInputElement).value).toBe(
    "cur",
  );
});

it("says how many values a group leaves out using the server's matches", async () => {
  server.list.mockResolvedValue(page(["s1"]));
  server.facets.mockResolvedValue(
    facets({
      client: facet(
        [
          value(CLIENT, 3, "Cursor"),
          value("dashboard", 2, undefined, false),
          ...Array.from({ length: 10 }, (_, i) =>
            value(
              `0192f4c8-1a2b-7c3d-8e4f-5a6b7c8d9f${String(i).padStart(2, "0")}`,
            ),
          ),
        ],
        20,
      ),
    }),
  );
  renderAt("/debug?sf=client:eq:dashboard");
  expect(await screen.findByText("9 more — search to find them")).toBeTruthy();
});

it("calls the issuer the sign-in server and lists former ones", async () => {
  server.list.mockResolvedValue(page(["s1"]));
  server.facets.mockResolvedValue(
    facets({
      mcpServer: facet([
        value(ISSUER, 2, "current"),
        value(FORMER, 1, "former"),
      ]),
      unattributedCount: 3,
    }),
  );
  renderAt();
  await screen.findByText("s1");
  openGroup("Sign-in server");
  expect(await screen.findByText("former")).toBeTruthy();
  expect(screen.getByText("current")).toBeTruthy();
  expect(
    screen.getByText(
      "3 sessions on this gateway's sign-in server have no recorded origin.",
    ),
  ).toBeTruthy();

  const grouping = screen.getByRole("group", { name: "Group connections by" });
  fireEvent.click(within(grouping).getByText("Sign-in server"));
  expect(screen.getByTestId("grouping-header").textContent).toBe(
    "Sign-in server",
  );
});

it("hides the unattributed notice when there are none", async () => {
  server.list.mockResolvedValue(page(["s1"]));
  server.facets.mockResolvedValue(facets({ unattributedCount: 0 }));
  renderAt();
  await screen.findByText("s1");
  await waitFor(() => expect(server.facets).toHaveBeenCalled());
  expect(screen.queryByText(/no recorded origin/)).toBeNull();
});

it("keeps an identity's exact bytes through an edit and refuses an empty one", async () => {
  server.list.mockResolvedValue(page(["s1"]));
  server.facets.mockResolvedValue(facets());
  const padded = "workload:0192f4c8-1a2b-7c3d-8e4f-5a6b7c8d9e0f: repo:a/app ";
  const link = new URLSearchParams({
    sf: serializeFilters([
      { id: "x", path: "identity", op: Op.Eq, value: padded },
    ])!,
  });
  renderAt(`/debug?${link}`);
  await screen.findByText("s1");
  expect(lastList()).toMatchObject({ identities: [padded] });

  fireEvent.click(screen.getByRole("button", { name: "Edit filter Identity" }));
  fireEvent.click(await screen.findByRole("button", { name: "Save" }));
  await waitFor(() =>
    expect(lastList()).toMatchObject({ identities: [padded] }),
  );
  expect(params().get("sf")).toBe(link.get("sf"));

  cleanup();
  renderAt("/debug?sf=identity:eq:");
  await screen.findByText("s1");
  expect(
    await screen.findByText(/Removed filters from this link/),
  ).toBeTruthy();
  expect(lastList().identities).toBeUndefined();
});

const many = (n: number, prefix = "0192f4c8-1a2b-7c3d-8e4f-5a6b7c8d") =>
  Array.from(
    { length: n },
    (_, i) => `${prefix}${i.toString(16).padStart(4, "0")}`,
  );

it("keeps each list within the server's 50-value cap", async () => {
  server.list.mockResolvedValue(page(["s1"]));
  const fifty = many(50);
  server.facets.mockResolvedValue(
    facets({
      client: facet(
        [...fifty, OTHER_CLIENT].map((id) =>
          value(
            id,
            1,
            id === OTHER_CLIENT ? "Another client" : `Client ${id.slice(-4)}`,
          ),
        ),
        51,
      ),
    }),
  );
  const full = new URLSearchParams({
    sf: serializeFilters([
      { id: "a", path: "client", op: Op.In, value: encodeValues(fifty) },
    ])!,
  });
  renderAt(`/debug?${full}`);
  await screen.findByText("s1");
  expect(lastList().clients).toHaveLength(50);

  fireEvent.click(await screen.findByText("Another client"));
  expect(
    screen.getByText(
      "A field takes at most 50 values to include and 50 to exclude",
    ),
  ).toBeTruthy();
  expect(lastList().clients).toHaveLength(50);

  submitSearch(`client != ${OTHER_CLIENT}`);
  await waitFor(() =>
    expect(lastList()).toMatchObject({ excludeClients: [OTHER_CLIENT] }),
  );
  expect(
    screen.queryByText(
      "A field takes at most 50 values to include and 50 to exclude",
    ),
  ).toBeNull();
  cleanup();

  const over = new URLSearchParams({
    sf: serializeFilters([
      { id: "a", path: "client", op: Op.In, value: encodeValues(many(51)) },
      { id: "b", path: "client_registration", op: Op.Eq, value: "dcr" },
    ])!,
  });
  renderAt(`/debug?${over}`);
  await screen.findByText("s1");
  const notice = (await screen.findByText(/Removed filters from this link/))
    .textContent!;
  expect(notice).toMatch(/: Client in Client 0000, Client 0001, /);
  expect(notice).not.toContain("%2C");
  for (const [request] of [
    ...server.list.mock.calls,
    ...server.facets.mock.calls,
  ]) {
    expect(
      ((request.clients as string[] | undefined) ?? []).length,
    ).toBeLessThanOrEqual(50);
  }
  await waitFor(() =>
    expect(lastList()).toMatchObject({ clientRegistrations: ["dcr"] }),
  );
  submitSearch(`client != ${OTHER_CLIENT}`);
  await waitFor(() =>
    expect(screen.queryByText(/Removed filters from this link/)).toBeNull(),
  );
});

it("searches fixed fields by their shown labels without a request", async () => {
  server.list.mockResolvedValue(page(["s1"]));
  server.facets.mockResolvedValue(
    facets({
      identityType: facet([value("user", 2), value("agent", 1)]),
      clientRegistration: facet([value("dcr", 2), value("cimd", 1)]),
    }),
  );
  renderAt();
  await screen.findByText("s1");
  await waitFor(() => expect(server.facets).toHaveBeenCalled());

  pickField("Identity type");
  pick("=");
  fireEvent.change(screen.getByPlaceholderText("value"), {
    target: { value: "pers" },
  });
  expect(await screen.findByText("Person")).toBeTruthy();
  expect(
    screen.getAllByRole("option").map((option) => option.textContent),
  ).not.toContainEqual(expect.stringContaining("Agent"));
  fireEvent.keyDown(screen.getByPlaceholderText("value"), { key: "Escape" });

  pickField("Client registration");
  pick("=");
  fireEvent.change(screen.getByPlaceholderText("value"), {
    target: { value: "dynamic" },
  });
  expect(await screen.findByText("Dynamic registration (DCR)")).toBeTruthy();
  await new Promise<void>((resolve) => {
    setTimeout(resolve, 300);
  });
  expect(
    server.facets.mock.calls.filter(([request]) => request.fieldSearch),
  ).toEqual([]);
});

it("drops a builder search when the builder resets and refreshes each search once", async () => {
  server.list.mockResolvedValue(page(["s1"]));
  server.facets.mockImplementation(async (request) =>
    request.fieldSearch === "cur"
      ? Promise.reject(new Error("500"))
      : facets({
          client: facet(
            Array.from({ length: 12 }, (_, i) =>
              value(many(12)[i]!, 1, `Client ${i}`),
            ),
            40,
          ),
        }),
  );
  renderAt();
  await screen.findByText("s1");

  pickField("Client");
  pick("=");
  fireEvent.change(screen.getByPlaceholderText("value"), {
    target: { value: "cur" },
  });
  expect(await screen.findByText("Couldn't load filter values.")).toBeTruthy();
  fireEvent.keyDown(screen.getByPlaceholderText("value"), { key: "Escape" });
  await waitFor(() =>
    expect(screen.queryByText("Couldn't load filter values.")).toBeNull(),
  );

  openGroup("Client");
  fireEvent.change(await screen.findByLabelText("Filter Client"), {
    target: { value: "zed" },
  });
  pickField("Client");
  pick("=");
  fireEvent.change(screen.getByPlaceholderText("value"), {
    target: { value: "zed" },
  });
  await waitFor(() =>
    expect(
      server.facets.mock.calls.filter(([r]) => r.fieldSearch === "zed"),
    ).toHaveLength(1),
  );

  server.facets.mockClear();
  fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
  await waitFor(() => expect(server.facets).toHaveBeenCalledTimes(2));
  await new Promise<void>((resolve) => {
    setTimeout(resolve, 100);
  });
  expect(server.facets.mock.calls.map(([r]) => r.fieldSearch ?? null)).toEqual([
    null,
    "zed",
  ]);
});
