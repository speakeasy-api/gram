import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { GramCore } from "@gram/client/core.js";
import { HTTPClient } from "@gram/client/lib/http.js";
import { GramProvider } from "@gram/client/react-query/_context.js";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { ClientsAndSessionsTab } from "./ClientsAndSessionsTab";

vi.mock("@/components/identity-link", () => ({
  IdentityLink: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@/hooks/useReadableAgents", () => ({
  useReadableAgents: () => ({
    data: [{ id: "example-agent", name: "Example agent" }],
  }),
}));
vi.mock("@/lib/useIdentityHref", () => ({
  useIdentityHrefBuilder:
    () =>
    ({ userId }: { userId: string }) =>
      `/identities/${userId}/access`,
}));
vi.mock(
  "@/components/killswitch/KillswitchUserStatus",
  async (importOriginal) => ({
    ...(await importOriginal<
      typeof import("@/components/killswitch/KillswitchUserStatus")
    >()),
    useKillswitchUserBadges: () => ({
      canAccess: true,
      badges: new Map(),
      unavailableUserIds: new Set(),
      loader: null,
    }),
  }),
);
vi.mock("./ClientDetailSheet", () => ({
  ClientDetailSheet: ({
    clientId,
    client,
  }: {
    clientId: string;
    client?: { clientName: string };
  }) => (
    <div>
      Registration {clientId}
      {client && <span>Loaded {client.clientName}</span>}
    </div>
  ),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true, hasAnyScope: () => true }),
}));
vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project", slug: "default" }),
}));
// Reaches for route helpers this test does not mount; its own gating is
// covered in ViewOrgSessionsButton.test.tsx.
vi.mock("./ViewOrgSessionsButton", () => ({
  ViewOrgSessionsButton: () => <span>View all project sessions</span>,
}));
// Radix's dropdown does not open under happy-dom's pointer-event model, so
// the action menu is stubbed to plain buttons.
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
vi.mock("./RevokeSessionDialog", () => ({
  RevokeSessionDialog: ({
    open,
    onRevoked,
  }: {
    open: boolean;
    onRevoked: () => void;
  }) => (open ? <button onClick={onRevoked}>Confirm revoke</button> : null),
}));

const queryClients: QueryClient[] = [];
afterEach(() => {
  cleanup();
  queryClients.forEach((client) => client.clear());
  queryClients.length = 0;
});
function session(id: string, extra: Record<string, unknown> = {}) {
  return {
    id,
    user_session_issuer_id: "issuer",
    subject_urn: `user:${id}`,
    subject_display_name: `Person ${id}`,
    subject_type: "user",
    jti: id,
    issuer_slug: "workforce",
    refresh_expires_at: "2099-12-01T00:00:00Z",
    expires_at: "2026-01-01T00:00:00Z",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    upstreams: [],
    ...extra,
  };
}
const registration = {
  id: "loaded-registration",
  client_name: "Example desktop",
  client_id: "public-client",
  user_session_issuer_id: "issuer",
  active_session_count: 0,
  credential_kind: "public",
  redirect_uris: [],
  client_id_issued_at: "2026-01-01T00:00:00Z",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};
function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}
function isPath(request: Request, path: string) {
  return new URL(request.url).pathname.endsWith(path);
}
function setup(
  handle: (request: Request) => Response,
  {
    gatewayId,
    issuerId = gatewayId ? "issuer" : undefined,
    entry = "/",
    registrations = [],
  }: {
    gatewayId?: string;
    issuerId?: string;
    entry?: string;
    registrations?: unknown[];
  } = {},
) {
  const requests: Request[] = [];
  const client = new GramCore({
    serverURL: "https://api.example.test",
    retryConfig: { strategy: "none" },
    httpClient: new HTTPClient({
      fetcher: async (request) => {
        const req = request as Request;
        requests.push(req);
        if (isPath(req, "gatewaySessions.listFacets"))
          return json({
            servers: [{ value: "member", label: "Example member" }],
            providers: [{ value: "provider", label: "Example provider" }],
          });
        if (isPath(req, "userSessionClients.list"))
          return json({ items: registrations });
        return handle(req);
      },
    }),
  });
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  queryClients.push(queryClient);
  const view = render(
    <ClientsAndSessionsTab gateway={{ id: gatewayId }} issuerId={issuerId} />,
    {
      wrapper: ({ children }) => (
        <QueryClientProvider client={queryClient}>
          <GramProvider client={client}>
            <MemoryRouter initialEntries={[entry]}>
              <TooltipProvider>{children}</TooltipProvider>
            </MemoryRouter>
          </GramProvider>
        </QueryClientProvider>
      ),
    },
  );
  return { ...view, requests, queryClient, user: userEvent.setup() };
}

it("lists organization sessions without a project or its actions, preserving loaded rows on a page error", async () => {
  let attempts = 0;
  const { user } = setup((request) => {
    const url = new URL(request.url);
    expect(url.searchParams.has("gateway_id")).toBe(false);
    expect(request.headers.has("Gram-Project")).toBe(false);
    if (!url.searchParams.has("cursor"))
      return json({
        items: [
          session("first", {
            user_session_client_id: "registration-id",
            client_name: "Example desktop",
          }),
        ],
        next_cursor: "cursor",
      });
    attempts++;
    return attempts === 1
      ? json({}, 503)
      : json({ items: [session("second")] });
  });
  await screen.findByText("Person first");
  expect(screen.queryByText(/Revoke/)).toBeNull();
  expect(screen.queryByText("Agents")).toBeNull();
  expect(screen.queryByText("View all project sessions")).toBeNull();
  await user.click(screen.getByText("Agent"));
  expect(screen.queryByText("View registration")).toBeNull();
  await user.click(screen.getByText("Identity"));
  await user.click(screen.getByRole("button", { name: "Load more" }));
  await user.click(await screen.findByRole("button", { name: "Retry" }));
  await screen.findByText("Person second");
  expect(screen.getByText("Person first")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Load more" })).toBeNull();
});

it("passes bookmarked filters to the API and resets pagination when identity search changes", async () => {
  const { user, requests } = setup(
    (request) => {
      const url = new URL(request.url);
      expect(url.searchParams.get("gateway_id")).toBe("gateway");
      expect(url.searchParams.get("server_id")).toBe("member");
      expect(url.searchParams.get("provider_id")).toBe("provider");
      expect(url.searchParams.get("status")).toBe("expired");
      expect(url.searchParams.get("sort")).toBe("last_used");
      if (url.searchParams.get("search") === "Ada") {
        expect(url.searchParams.has("cursor")).toBe(false);
        return json({ items: [session("Ada")] });
      }
      return json({
        items: [session(url.searchParams.has("cursor") ? "second" : "first")],
        next_cursor: "next",
      });
    },
    {
      gatewayId: "gateway",
      entry: "/?server=member&provider=provider&status=expired",
    },
  );
  await screen.findByText("Person first");
  expect(screen.getByText("Expired sessions")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Load more" }));
  await screen.findByText("Person second");
  await user.type(screen.getByPlaceholderText("Search identities…"), "Ada");
  await screen.findByText("Person Ada");
  expect(screen.queryByText("Person first")).toBeNull();
  expect(screen.queryByText("Person second")).toBeNull();
  await waitFor(() =>
    expect(
      requests.filter(
        (request) => new URL(request.url).searchParams.get("search") === "Ada",
      ),
    ).toHaveLength(1),
  );
});

it("says when filters match nothing rather than reading as an unused gateway", async () => {
  setup(() => json({ items: [] }), {
    gatewayId: "gateway",
    entry: "/?status=revoked",
  });
  expect(
    await screen.findByText("No connections match these filters"),
  ).toBeTruthy();
});

it("preserves registration and killswitch actions and invalidates gateway sessions and registrations after revocation", async () => {
  const { queryClient } = setup(
    () =>
      json({
        items: [
          session("example", {
            user_session_client_id: "registration-id",
            client_name: "Example desktop",
          }),
        ],
      }),
    { gatewayId: "gateway" },
  );
  const invalidate = vi.spyOn(queryClient, "invalidateQueries");
  await screen.findByText("Person example");
  expect(screen.getByText("View killswitches")).toBeTruthy();
  expect(screen.getByText("New killswitch…")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { expanded: false }));
  fireEvent.click(screen.getByText("Revoke connection"));
  fireEvent.click(screen.getByText("Confirm revoke"));
  expect(
    invalidate.mock.calls.some(([filters]) =>
      JSON.stringify(filters?.queryKey).includes("userSessionClients"),
    ),
  ).toBe(true);
  expect(
    invalidate.mock.calls.some(([filters]) =>
      JSON.stringify(filters?.queryKey).includes("gatewaySessions"),
    ),
  ).toBe(true);
  fireEvent.click(screen.getByText("Agent"));
  fireEvent.click(screen.getByText("View registration"));
  expect(screen.getByText("Registration registration-id")).toBeTruthy();
});

it("resolves authorized agent names", async () => {
  setup(() =>
    json({
      items: [
        session("agent", {
          subject_type: "agent",
          subject_urn: "agent:example-agent",
          subject_display_name: undefined,
        }),
      ],
    }),
  );
  expect(await screen.findByText("Example agent")).toBeTruthy();
});

it("opens a bookmarked registration without selecting a session first", async () => {
  setup(() => json({ items: [] }), {
    gatewayId: "gateway",
    entry: "/?registration=bookmarked&q=Ada",
  });
  expect(await screen.findByText("Registration bookmarked")).toBeTruthy();
  expect(screen.getByPlaceholderText("Search identities…")).toHaveProperty(
    "value",
    "Ada",
  );
});

it("hands a loaded registration to its bookmarked detail sheet", async () => {
  setup(() => json({ items: [] }), {
    gatewayId: "gateway",
    entry: "/?registration=loaded-registration",
    registrations: [registration],
  });
  expect(await screen.findByText("Loaded Example desktop")).toBeTruthy();
});

it("ignores a registration link on the organization-wide listing", async () => {
  setup(() => json({ items: [session("first")] }), {
    entry: "/?registration=bookmarked",
  });
  await screen.findByText("Person first");
  expect(screen.queryByText("Registration bookmarked")).toBeNull();
});

it("explains a gateway with no session issuer instead of listing", () => {
  const { requests } = setup(() => json({ items: [] }), {
    gatewayId: "gateway",
    issuerId: "",
  });
  expect(screen.getByText(/has no session issuer/i)).toBeTruthy();
  expect(requests).toHaveLength(0);
});

it("converts a bookmarked calendar selection to an exclusive next-day bound", async () => {
  const from = new Date(2026, 2, 7);
  const to = new Date(2026, 2, 8);
  const params = new URLSearchParams({
    from: from.toISOString(),
    to: to.toISOString(),
    calendarEnd: new Date(2026, 2, 9).toISOString(),
  });
  const { requests } = setup(() => json({ items: [session("calendar")] }), {
    gatewayId: "gateway",
    entry: `/?${params}`,
  });
  await screen.findByText("Person calendar");
  const request = requests.find((r) => isPath(r, "gatewaySessions.list"))!;
  expect(new URL(request.url).searchParams.get("created_before")).toBe(
    new Date(2026, 2, 9).toISOString(),
  );
});
