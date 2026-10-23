import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { GramCore } from "@gram/client/core.js";
import { HTTPClient } from "@gram/client/lib/http.js";
import { GramProvider } from "@gram/client/react-query/_context.js";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { GatewayConnectionsTab } from "./GatewayConnectionsTab";

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
vi.mock("@/components/sessions/ClientDetailSheet", () => ({
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
  useRBAC: () => ({ hasScope: () => true }),
}));
vi.mock("@/components/sessions/RevokeSessionDialog", () => ({
  RevokeSessionDialog: ({ onRevoked }: { onRevoked: () => void }) => (
    <button onClick={onRevoked}>Confirm revoke</button>
  ),
}));

const clients: QueryClient[] = [];
afterEach(() => {
  cleanup();
  clients.forEach((client) => client.clear());
  clients.length = 0;
});
function session(id: string) {
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
  };
}
function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}
function setup(
  handle: (request: Request) => Response,
  gatewayId?: string,
  entry = "/",
) {
  const requests: Request[] = [];
  const client = new GramCore({
    serverURL: "https://api.example.test",
    retryConfig: { strategy: "none" },
    httpClient: new HTTPClient({
      fetcher: async (request) => {
        const req = request as Request;
        requests.push(req);
        if (new URL(req.url).pathname.endsWith(".listFacets"))
          return json({
            servers: [{ value: "member", label: "Example member" }],
            providers: [{ value: "provider", label: "Example provider" }],
          });
        return handle(req);
      },
    }),
  });
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  clients.push(queryClient);
  const view = render(
    <GatewayConnectionsTab
      issuerId={gatewayId ? "issuer" : undefined}
      gatewayId={gatewayId}
      projectId={gatewayId ? "project" : undefined}
    />,
    {
      wrapper: ({ children }) => (
        <QueryClientProvider client={queryClient}>
          <GramProvider client={client}>
            <MemoryRouter initialEntries={[entry]}>{children}</MemoryRouter>
          </GramProvider>
        </QueryClientProvider>
      ),
    },
  );
  return { ...view, requests, queryClient, user: userEvent.setup() };
}
it("lists organization sessions without a project, preserving loaded rows on a page error", async () => {
  let attempts = 0;
  const { user } = setup((request) => {
    const url = new URL(request.url);
    expect(url.searchParams.has("gateway_id")).toBe(false);
    expect(request.headers.has("Gram-Project")).toBe(false);
    if (!url.searchParams.has("cursor"))
      return json({ items: [session("first")], next_cursor: "cursor" });
    attempts++;
    return attempts === 1
      ? json({}, 503)
      : json({ items: [session("second")] });
  });
  await screen.findByText("Person first");
  await user.click(screen.getByRole("button", { name: "Open menu" }));
  expect(screen.queryByText("Revoke session")).toBeNull();
  expect(screen.queryByText("View registration")).toBeNull();
  await user.keyboard("{Escape}");
  await user.click(screen.getByRole("button", { name: "Load more" }));
  await user.click(await screen.findByRole("button", { name: "Retry" }));
  await screen.findByText("Person second");
  expect(screen.getByText("Person first")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Load more" })).toBeNull();
});
it("passes bookmarked filters and sorting to the API and resets pagination when identity search changes", async () => {
  const { user, requests } = setup(
    (request) => {
      const url = new URL(request.url);
      expect(url.searchParams.get("gateway_id")).toBe("gateway");
      expect(url.searchParams.get("server_id")).toBe("member");
      expect(url.searchParams.get("provider_id")).toBe("provider");
      expect(url.searchParams.get("status")).toBe("expired");
      expect(url.searchParams.get("sort")).toBe("oldest");
      if (url.searchParams.get("search") === "Ada") {
        expect(url.searchParams.has("cursor")).toBe(false);
        return json({ items: [session("Ada")] });
      }
      return json({
        items: [session(url.searchParams.has("cursor") ? "second" : "first")],
        next_cursor: "next",
      });
    },
    "gateway",
    "/?server=member&provider=provider&status=expired&sort=oldest",
  );
  await screen.findByText("Person first");
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

it("preserves registration and killswitch actions and invalidates registration counts after revocation", async () => {
  const { user, queryClient } = setup(
    () =>
      json({
        items: [
          { ...session("example"), user_session_client_id: "registration-id" },
        ],
      }),
    "gateway",
  );
  const invalidate = vi.spyOn(queryClient, "invalidateQueries");
  await screen.findByText("Person example");
  await user.click(screen.getByRole("button", { name: "Open menu" }));
  expect(screen.getByText("View killswitches")).toBeTruthy();
  expect(screen.getByText("New killswitch…")).toBeTruthy();
  await user.click(screen.getByText("View registration"));
  expect(screen.getByText("Registration registration-id")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Open menu" }));
  await user.click(screen.getByText("Revoke session"));
  await user.click(screen.getByText("Confirm revoke"));
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
});
it("resolves authorized agent names", async () => {
  setup(() =>
    json({
      items: [
        {
          ...session("agent"),
          subject_type: "agent",
          subject_urn: "agent:example-agent",
          subject_display_name: undefined,
        },
      ],
    }),
  );
  expect(await screen.findByText("Example agent")).toBeTruthy();
});

it("opens a bookmarked registration without selecting a session first", async () => {
  setup(
    () => json({ items: [] }),
    "gateway",
    "/?registration=bookmarked&q=Ada&sort=expiry",
  );
  expect(await screen.findByText("Registration bookmarked")).toBeTruthy();
  expect(screen.getByPlaceholderText("Search identities…")).toHaveProperty(
    "value",
    "Ada",
  );
});

it("converts a bookmarked calendar selection to an exclusive next-day bound", async () => {
  const from = new Date(2026, 2, 7);
  const to = new Date(2026, 2, 8);
  const params = new URLSearchParams({
    from: from.toISOString(),
    to: to.toISOString(),
    calendarEnd: new Date(2026, 2, 9).toISOString(),
  });
  const { requests } = setup(
    () => json({ items: [session("calendar")] }),
    "gateway",
    `/?${params}`,
  );
  await screen.findByText("Person calendar");
  const request = requests.find((r) =>
    new URL(r.url).pathname.endsWith(".list"),
  )!;
  expect(new URL(request.url).searchParams.get("created_before")).toBe(
    new Date(2026, 2, 9).toISOString(),
  );
});

it("keeps the loaded registration row when opening its bookmarked detail sheet", async () => {
  const { user } = setup((request) => {
    if (new URL(request.url).pathname.includes("userSessionClients"))
      return json({
        items: [
          {
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
          },
        ],
      });
    return json({ items: [] });
  }, "gateway");
  await user.click(
    screen.getByRole("button", { name: "Show registered clients" }),
  );
  await screen.findByText("Example desktop");
  await user.click(screen.getByRole("button", { name: "Open menu" }));
  await user.click(screen.getByText("View registration"));
  expect(await screen.findByText("Loaded Example desktop")).toBeTruthy();
});
