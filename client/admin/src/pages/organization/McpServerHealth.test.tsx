import {
  act,
  cleanup,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { routeTree } from "@/routeTree.gen";
import { anOrganization, aProject } from "@/test/fixtures";
import { renderRouteTree } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  getSession: vi.fn(),
  getOrganization: vi.fn(),
  listOrganizationProjects: vi.fn(),
  healthFetch: vi.fn(),
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    getSession: mocks.getSession,
    getOrganization: mocks.getOrganization,
    listOrganizationProjects: mocks.listOrganizationProjects,
  };
});

const ORG = anOrganization();
const PROJECT = aProject({ id: "proj_1", name: "default", slug: "default" });
const SERVER_ID = "srv_crm";

// The wire shape, snake_case, as the admin endpoint answers. Invented values.
const CLIENT = {
  id: "rsc_1",
  registration: "static",
  token_endpoint_auth_method: "client_secret_post",
  scope: ["api", "refresh_token"],
  grant_types: ["authorization_code", "refresh_token"],
  has_identity_provider_connection: false,
  attachment_scope: "project",
  issuer: {
    id: "rsi_1",
    slug: "crm-upstream",
    name: "Example CRM",
    issuer: "https://login.example.test",
    attachment_scope: "global",
    networking: "public",
    oidc: true,
    passthrough: false,
    pkce: "supported",
    cimd_supported: false,
    metadata_fetched_at: "2026-09-29T07:40:00Z",
  },
  sessions: {
    linked_subjects: 5,
    reauthorizations: 7,
    first_linked_at: "2026-08-14T10:00:00Z",
    validation_status_counts: { valid: 4, rejected_by_member: 1 },
  },
};

const ISSUER = {
  id: "usi_1",
  slug: "crm-login",
  classification: "custom",
  authn_challenge_mode: "interactive",
  session_duration_hours: 720,
  attachment_scope: "project",
  use_authentication_host: false,
  other_servers_using_issuer: [],
  created_at: "2026-08-12T00:00:00Z",
  sessions: {
    distinct_subjects_ever: 12,
    distinct_subjects_in_window: 4,
    first_issued_at: "2026-08-14T10:00:00Z",
    last_issued_at: "2026-09-29T09:12:00Z",
    live: 3,
  },
  remote_session_clients: [CLIENT],
};

const SERVER = {
  id: SERVER_ID,
  name: "crm",
  source: "remote",
  visibility: "private",
  created_at: "2026-08-12T00:00:00Z",
};

function series(windowDays: number) {
  const weekly = windowDays === 90;
  const step = weekly ? 7 : 1;
  const count = weekly ? 13 : windowDays;
  return {
    bucket_seconds: step * 86_400,
    daily: Array.from({ length: count }, (_, i) => ({
      bucket_start: new Date(Date.UTC(2026, 8, 1 + i * step)).toISOString(),
      total: 30,
      failed: i === 3 ? 6 : 0,
    })),
  };
}

function enabled(windowDays: number) {
  return {
    type: "logging:enabled",
    window_days: windowDays,
    watermark: "2026-09-29T09:10:00Z",
    outcomes: {
      success: 400,
      unauthorized: 3,
      client_error: 5,
      server_error: 6,
      blocked: 1,
      failed: 3,
      unknown: 2,
    },
    ...series(windowDays),
  };
}

type Body = Record<string, unknown>;
let respond: (windowDays: number) => Body;

function withIssuer(windowDays: number): Body {
  return {
    server: SERVER,
    correlation: { url_slug: "crm-9c1e", mcp_server_id: SERVER_ID },
    user_session_issuer: ISSUER,
    tool_calls: enabled(windowDays),
  };
}

function requestUrl(input: RequestInfo | URL): URL {
  if (input instanceof Request) return new URL(input.url);
  return new URL(String(input), window.location.origin);
}

const writeText = vi.fn(() => Promise.resolve());

beforeEach(() => {
  for (const mock of Object.values(mocks)) mock.mockReset();
  mocks.getSession.mockResolvedValue({ email: "ops@example.test", name: "" });
  mocks.getOrganization.mockResolvedValue(ORG);
  mocks.listOrganizationProjects.mockResolvedValue({ projects: [PROJECT] });
  respond = withIssuer;
  mocks.healthFetch.mockImplementation((input: RequestInfo | URL) => {
    const url = requestUrl(input);
    if (url.pathname !== "/admin/project.mcpServerHealth") {
      return Promise.resolve(new Response("not found", { status: 404 }));
    }
    const windowDays = Number(url.searchParams.get("window_days"));
    return Promise.resolve(
      new Response(JSON.stringify(respond(windowDays)), {
        headers: { "Content-Type": "application/json" },
      }),
    );
  });
  vi.stubGlobal("fetch", mocks.healthFetch);
  writeText.mockClear();
  Object.defineProperty(navigator, "clipboard", {
    value: { writeText },
    configurable: true,
    writable: true,
  });
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function open(search = `project=${PROJECT.id}`) {
  return renderRouteTree(routeTree, {
    initialPath: `/organizations/${ORG.slug}/mcp-servers/${SERVER_ID}?${search}`,
  });
}

function healthRequests(): URL[] {
  return mocks.healthFetch.mock.calls
    .map(([input]) => requestUrl(input))
    .filter((url) => url.pathname === "/admin/project.mcpServerHealth");
}

function card(name: string): HTMLElement {
  return screen.getByRole("group", { name });
}

describe("McpServerHealth", () => {
  it("asks for the server in its project over the default window", async () => {
    await open();
    await screen.findByRole("heading", { name: "crm" });

    const [request] = healthRequests();
    expect(Object.fromEntries(request!.searchParams)).toEqual({
      organization_id: ORG.id,
      project_id: PROJECT.id,
      mcp_server_id: SERVER_ID,
      window_days: "14",
    });
  });

  it("draws a server with an issuer and a remote session client", async () => {
    await open();
    await screen.findByRole("heading", { name: "crm" });

    // The server's header replaces the organization's.
    expect(screen.queryByRole("button", { name: /Open in Dashboard/ })).toBe(
      null,
    );
    expect(card("User session issuer").textContent).toContain("Configured");
    expect(card("User session issuer").textContent).toContain(
      "30 days sessions · 1 upstream client",
    );
    expect(card("People signed in").textContent).toContain("12 ever");
    expect(card("People signed in").textContent).toContain(
      "4 in window · 3 live sessions",
    );
    expect(card("Upstream accounts linked").textContent).toContain("1 invalid");
    expect(card("Tool calls").textContent).toContain("420");
    expect(card("Tool calls").textContent).toContain("4.3% failed");
    expect(card("Tool calls").textContent).toContain(
      "18 failed · 3 unauthorized",
    );

    expect(
      screen.getByRole("heading", { name: "Tool calls per day" }),
    ).toBeTruthy();
    expect(screen.getByText(/Failed means status 400 or above/)).toBeTruthy();

    const clients = screen.getByRole("region", {
      name: "Remote session clients",
    });
    expect(within(clients).getByText("Example CRM")).toBeTruthy();
    expect(
      within(clients).getByText("(shared by every organization)"),
    ).toBeTruthy();
    expect(within(clients).getByText("4 valid")).toBeTruthy();
    expect(within(clients).getByText("1 rejected by member")).toBeTruthy();
  });

  it("links both logs to Datadog, filtered to the server", async () => {
    await open();
    await screen.findByRole("heading", { name: "crm" });

    const tail = new URL(
      screen
        .getByRole("link", { name: /Tool call tail/ })
        .getAttribute("href")!,
    );
    expect(tail.origin + tail.pathname).toBe(
      "https://app.datadoghq.com/logs/livetail",
    );
    expect(tail.searchParams.get("query")).toContain('"/mcp/crm-9c1e"');

    const login = new URL(
      screen
        .getByRole("link", { name: /Login challenge logs/ })
        .getAttribute("href")!,
    );
    expect(login.searchParams.get("query")).toBe(
      '@gram.toolset.mcp_slug:crm-9c1e OR @gram.oauth.issuer:"https://login.example.test"',
    );
    expect(login.searchParams.get("from_ts")).toBeTruthy();
  });

  it("copies the Platform MCP prompt", async () => {
    await open();
    const copy = await screen.findByRole("button", { name: "Copy prompt" });
    await act(async () => {
      fireEvent.click(copy);
      await Promise.resolve();
    });

    expect(writeText).toHaveBeenCalledTimes(1);
    const [prompt] = writeText.mock.calls[0] as unknown as [string];
    expect(prompt).toContain(`"crm" (mcp_id ${SERVER_ID})`);
    expect(prompt).toContain("in the default project");
  });

  it("names a legacy auth mode when there is no issuer", async () => {
    respond = (windowDays) => ({
      server: { ...SERVER, source: "toolset_only", visibility: "public" },
      correlation: { url_slug: "docs" },
      legacy_auth: "external_oauth",
      tool_calls: enabled(windowDays),
    });
    await open();
    await screen.findByRole("heading", { name: "crm" });

    expect(card("User session issuer").textContent).toContain(
      "legacy: external OAuth",
    );
    expect(card("People signed in").textContent).toContain(
      "No issuer, so no sessions to count",
    );
    expect(
      screen.queryByRole("region", { name: "Remote session clients" }),
    ).toBe(null);
  });

  it("says logging is off rather than showing zeros", async () => {
    respond = () => ({
      server: SERVER,
      correlation: { url_slug: "crm-9c1e" },
      legacy_auth: "oauth_proxy",
      tool_calls: { type: "logging:disabled" },
    });
    await open();
    await screen.findByText("Logging is off for this organization");

    expect(card("Tool calls").textContent).toContain("Unknown");
    expect(screen.queryByRole("heading", { name: /Tool calls per/ })).toBe(
      null,
    );
    expect(
      screen
        .getByRole("link", { name: "Review features" })
        .getAttribute("href"),
    ).toBe(`/organizations/${ORG.slug}/features`);
    // The tail works from ingress logs, so it stays with logging off.
    expect(screen.getByRole("link", { name: /Tool call tail/ })).toBeTruthy();
  });

  it("re-queries from the window picker and switches to weekly buckets at 90 days", async () => {
    const { router } = await open();
    await screen.findByRole("heading", { name: "Tool calls per day" });

    fireEvent.keyDown(screen.getByRole("combobox", { name: "Window" }), {
      key: "ArrowDown",
    });
    fireEvent.click(
      await screen.findByRole("option", { name: "Last 90 days" }),
    );

    await screen.findByRole("heading", { name: "Tool calls per week" });
    expect(router.state.location.search).toEqual({
      project: PROJECT.id,
      window: 90,
    });
    expect(healthRequests().at(-1)!.searchParams.get("window_days")).toBe("90");
  });

  it("keeps the project on the MCP Servers crumb and leaves the others bare", async () => {
    await open("project=proj_1&window=30");
    await screen.findByRole("heading", { name: "crm" });

    const nav = screen.getByRole("navigation", { name: "breadcrumb" });
    await waitFor(() => expect(within(nav).getByText("crm")).toBeTruthy());
    expect(
      within(nav)
        .getByRole("link", { name: "MCP Servers" })
        .getAttribute("href"),
    ).toBe(`/organizations/${ORG.slug}/mcp-servers?project=${PROJECT.id}`);
    expect(
      within(nav).getByRole("link", { name: ORG.name }).getAttribute("href"),
    ).toBe(`/organizations/${ORG.slug}`);
    expect(
      within(nav)
        .getByRole("link", { name: "Organizations" })
        .getAttribute("href"),
    ).toBe("/organizations");
  });

  it("sends an address with no project back to the list", async () => {
    const { router } = await open("");
    await waitFor(() =>
      expect(router.state.location.pathname).toBe(
        `/organizations/${ORG.slug}/mcp-servers`,
      ),
    );
    expect(healthRequests()).toEqual([]);
  });
});
