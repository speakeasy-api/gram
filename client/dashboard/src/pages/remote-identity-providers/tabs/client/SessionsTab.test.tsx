import { MemoryRouter } from "react-router";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { GramCore } from "@gram/client/core.js";
import { HTTPClient } from "@gram/client/lib/http.js";
import { GramProvider } from "@gram/client/react-query/_context.js";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { SessionsTab } from "./SessionsTab";

vi.mock("@/components/identity-link", () => ({
  IdentityLink: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasAnyScope: () => true }),
}));
vi.mock("@/hooks/useReadableAgents", () => ({
  useReadableAgents: () => ({ data: [], isError: false }),
}));
vi.mock("../../clientDialogs", () => ({
  RevokeAllSessionsDialog: () => null,
}));

const queryClients: QueryClient[] = [];
afterEach(() => {
  cleanup();
  queryClients.forEach((client) => client.clear());
  queryClients.length = 0;
});

function session(id: string, clientId = "client-a") {
  return {
    id,
    remote_session_client_id: clientId,
    user_session_issuer_id: "issuer-a",
    subject_urn: `anonymous:${id}`,
    subject_display_name: `Session ${id}`,
    scopes: [],
    has_refresh_token: false,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    access_expires_at: "2026-12-01T00:00:00Z",
  };
}

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function setup(handle: (request: Request) => Response | Promise<Response>) {
  const requests: Request[] = [];
  const client = new GramCore({
    serverURL: "https://api.example.test",
    retryConfig: { strategy: "none" },
    httpClient: new HTTPClient({
      fetcher: async (request) => {
        const req = request as Request;
        requests.push(req);
        return handle(req);
      },
    }),
  });
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  queryClients.push(queryClient);
  const view = render(<SessionsTab clientId="client-a" />, {
    wrapper: ({ children }) => (
      <QueryClientProvider client={queryClient}>
        <GramProvider client={client}>
          <MemoryRouter>{children}</MemoryRouter>
        </GramProvider>
      </QueryClientProvider>
    ),
  });
  return { ...view, requests, user: userEvent.setup() };
}

it("loads later pages on demand, keeps rows during failure, and retries the same cursor", async () => {
  const firstPage = Array.from({ length: 2 }, (_, i) => session(`first-${i}`));
  let finishNextPage: (response: Response) => void = () => {};
  let nextAttempts = 0;
  const { user, requests } = setup((request) => {
    const url = new URL(request.url);
    expect(url.pathname).toBe("/rpc/organizationRemoteSessions.list");
    expect(url.searchParams.get("client_id")).toBe("client-a");
    expect(request.headers.has("Gram-Project")).toBe(false);
    if (!url.searchParams.has("cursor")) {
      return json({ items: firstPage, next_cursor: "after-first" });
    }
    expect(url.searchParams.get("cursor")).toBe("after-first");
    nextAttempts++;
    if (nextAttempts === 1) {
      return new Promise<Response>((resolve) => {
        finishNextPage = resolve;
      });
    }
    return json({ items: [session("later")] });
  });

  await screen.findByText("Session first-1");
  expect(requests).toHaveLength(1);
  await user.click(screen.getByRole("button", { name: "Load more" }));
  const loading = await screen.findByRole("button", { name: "Loading…" });
  expect((loading as HTMLButtonElement).disabled).toBe(true);
  expect(screen.getByText("Session first-0")).toBeTruthy();
  finishNextPage(json({ message: "Temporary failure" }, 503));
  await screen.findByRole("alert");
  expect(screen.getByText("Session first-1")).toBeTruthy();
  expect(nextAttempts).toBe(1);

  await user.click(screen.getByRole("button", { name: "Retry" }));
  await screen.findByText("Session later");
  expect(screen.getByText("Session first-0")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Load more" })).toBeNull();
  expect(screen.queryByRole("alert")).toBeNull();
  expect(requests).toHaveLength(3);
});

it("resets pagination when switching clients and handles an empty last page", async () => {
  const { user, requests, rerender } = setup((request) => {
    const url = new URL(request.url);
    if (url.searchParams.get("client_id") === "client-b") {
      expect(url.searchParams.has("cursor")).toBe(false);
      return json({ items: [session("other-client", "client-b")] });
    }
    if (url.searchParams.has("cursor")) return json({ items: [] });
    return json({ items: [session("first")], next_cursor: "after-first" });
  });
  await screen.findByText("Session first");
  await user.click(screen.getByRole("button", { name: "Load more" }));
  await waitFor(() =>
    expect(screen.queryByRole("button", { name: "Loading…" })).toBeNull(),
  );
  expect(screen.getByText("Session first")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Load more" })).toBeNull();

  rerender(<SessionsTab clientId="client-b" />);
  await screen.findByText("Session other-client");
  expect(screen.queryByText("Session first")).toBeNull();
  expect(requests).toHaveLength(3);
});

it("retries an initial error and shows the empty state only after success", async () => {
  let attempts = 0;
  const { user } = setup(() => {
    attempts++;
    return attempts === 1 ? json({}, 503) : json({ items: [] });
  });
  await screen.findByText("Failed to load sessions");
  expect(screen.queryByText("No sessions match these filters.")).toBeNull();
  await user.click(screen.getByRole("button", { name: "Retry" }));
  await screen.findByText("No sessions match these filters.");
  expect(screen.queryByRole("button", { name: "Load more" })).toBeNull();
});

it("revoking a session from a later page refreshes all loaded pages", async () => {
  let revoked = false;
  const { user, requests } = setup((request) => {
    const url = new URL(request.url);
    if (url.pathname.endsWith(".revoke")) {
      expect(url.searchParams.get("id")).toBe("later");
      revoked = true;
      return new Response(null, { status: 200 });
    }
    if (url.searchParams.has("cursor")) {
      return json({ items: revoked ? [] : [session("later")] });
    }
    return json({ items: [session("first")], next_cursor: "after-first" });
  });
  await screen.findByText("Session first");
  await user.click(screen.getByRole("button", { name: "Load more" }));
  await screen.findByText("Session later");
  await user.click(
    screen.getAllByRole("button", { name: "Session actions" })[1]!,
  );
  await user.click(
    await screen.findByRole("menuitem", { name: "Revoke session" }),
  );
  await waitFor(() => expect(screen.queryByText("Session later")).toBeNull());
  expect(screen.getByText("Session first")).toBeTruthy();
  expect(
    requests.filter((request) =>
      new URL(request.url).pathname.endsWith(".list"),
    ),
  ).toHaveLength(4);
});

it("searches on the server and starts a new cursor sequence after filtering", async () => {
  const { user } = setup((request) => {
    const url = new URL(request.url);
    expect(url.searchParams.get("sort")).toBe("newest");
    if (url.searchParams.get("search") === "Ada") {
      expect(url.searchParams.has("cursor")).toBe(false);
      return json({ items: [session("Ada")] });
    }
    return json({
      items: [session(url.searchParams.has("cursor") ? "later" : "first")],
      next_cursor: "next",
    });
  });
  await screen.findByText("Session first");
  await user.click(screen.getByRole("button", { name: "Load more" }));
  await screen.findByText("Session later");
  await user.type(screen.getByPlaceholderText("Search identities…"), "Ada");
  await screen.findByText("Session Ada");
  expect(screen.queryByText("Session first")).toBeNull();
  expect(screen.queryByText("Session later")).toBeNull();
});
