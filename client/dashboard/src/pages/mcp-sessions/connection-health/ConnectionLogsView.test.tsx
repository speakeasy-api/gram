import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ConnectionLogsView } from "./ConnectionLogsView";

const enabled = vi.hoisted(() => ({
  calls: [] as (boolean | undefined)[],
  sent: [] as { people: number; enabled: boolean | undefined }[],
  refetched: [] as number[],
  logging: true,
}));

vi.mock("./api", async (original) => ({
  ...(await original<object>()),
  useRangeClock: () => [new Date("2026-11-20T12:00:00Z"), vi.fn()],
  useAttemptCounts: (
    _scope: unknown,
    _window: unknown,
    filters: { people?: string[] },
    options?: { enabled?: boolean },
  ) => {
    const people = filters.people?.length ?? 0;
    enabled.calls.push(options?.enabled);
    enabled.sent.push({ people, enabled: options?.enabled });
    return {
      data:
        options?.enabled === false
          ? undefined
          : {
              totalAttempts: 0,
              loggingEnabled: enabled.logging,
              viewerScope: "all",
            },
      isPlaceholderData: false,
      isFetching: false,
      isError: false,
      refetch: () => enabled.refetched.push(people),
    };
  },
  useConnectionAttempts: (
    _scope: unknown,
    _window: unknown,
    filters: { people?: string[] },
    options?: { enabled?: boolean },
  ) => ({
    _: enabled.calls.push(options?.enabled),
    __: enabled.sent.push({
      people: filters.people?.length ?? 0,
      enabled: options?.enabled,
    }),
    data:
      options?.enabled === false ? undefined : { pages: [{ attempts: [] }] },
    isPending: false,
    isError: false,
    isFetching: false,
    isPlaceholderData: false,
    hasNextPage: false,
    fetchNextPage: vi.fn(),
    refetch: () => enabled.refetched.push(filters.people?.length ?? 0),
  }),
  useAttemptHere: () => ({
    query: { data: undefined, isFetching: false, refetch: vi.fn() },
    attempt: undefined,
    failed: false,
    isPending: false,
    retry: vi.fn(),
  }),
}));

vi.mock("../providers", () => ({
  useProjectProviders: () => ({ providers: [] }),
}));

vi.mock("./AttemptDetailSheet", () => ({ AttemptDetailSheet: () => null }));

vi.mock("@/components/require-scope", () => ({
  RequireScope: () => null,
}));

vi.mock("@gram/client/react-query/featuresSet.js", () => ({
  useFeaturesSetMutation: () => ({ mutate: vi.fn(), isPending: false }),
}));

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));

vi.mock("@/contexts/Auth", async (original) => ({
  ...(await original<object>()),
  useOrganization: () => ({ id: "org_1", name: "Example" }),
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcpSessions: {
      provider: {
        connectionLogs: { href: (id: string) => `/p/${id}/connection-logs` },
      },
    },
  }),
}));

function renderLogs(url: string, serverId?: string) {
  render(
    <MemoryRouter initialEntries={[url]}>
      <ConnectionLogsView providerId="provider_1" serverId={serverId} />
    </MemoryRouter>,
  );
}

describe("ConnectionLogsView on a server's tab", () => {
  afterEach(() => {
    cleanup();
    enabled.logging = true;
  });

  it("shows no server chip, reset or filter wording for the fixed server", () => {
    renderLogs("/?server=srv_other", "srv_1");
    expect(screen.queryByText(/srv_/)).toBeNull();
    expect(screen.queryByText(/^Server/)).toBeNull();
    expect(screen.queryByText("Reset filters")).toBeNull();
    expect(screen.getByText(/^No attempts in the/)).toBeTruthy();
    const open = screen.getByRole("link", {
      name: /Open in MCP Sessions with this server/,
    });
    expect(open.getAttribute("href")).toBe(
      "/p/provider_1/connection-logs?server=srv_1",
    );
  });

  it("still shows a server filter on the provider's own logs", () => {
    renderLogs("/?server=srv_other");
    expect(screen.getAllByText(/srv_other/).length).toBeGreaterThan(0);
    expect(screen.getByText("No attempts match these filters")).toBeTruthy();
  });

  it("does not send a link with more than 50 people", () => {
    enabled.sent = [];
    const people = Array.from({ length: 51 }, (_, i) => `user:u${i}`).join(",");
    renderLogs(`/?person=${encodeURIComponent(people)}`);
    expect(
      screen.getByText("Too many servers or people in this link"),
    ).toBeTruthy();
    const filtered = enabled.sent.filter((c) => c.people > 0);
    expect(filtered.length).toBeGreaterThan(0);
    expect(filtered.every((c) => c.enabled === false)).toBe(true);
    // Unfiltered options still load so each pick can be removed.
    expect(enabled.sent.some((c) => c.people === 0 && c.enabled)).toBe(true);
  });

  it("shows the logging gate for a link over the limit when logging is off", () => {
    enabled.logging = false;
    const people = Array.from({ length: 51 }, (_, i) => `user:u${i}`).join(",");
    renderLogs(`/?person=${encodeURIComponent(people)}`);
    expect(screen.getByText("Connection Logs needs logging")).toBeTruthy();
    expect(
      screen.queryByText("Too many servers or people in this link"),
    ).toBeNull();
  });

  it("sends a link with exactly 50 people", () => {
    enabled.sent = [];
    const people = Array.from({ length: 50 }, (_, i) => `user:u${i}`).join(",");
    renderLogs(`/?person=${encodeURIComponent(people)}`);
    expect(
      screen.queryByText("Too many servers or people in this link"),
    ).toBeNull();
    const filtered = enabled.sent.filter((c) => c.people === 50);
    expect(filtered.length).toBeGreaterThan(0);
    expect(filtered.some((c) => c.enabled === true)).toBe(true);
  });

  it("does not refetch a link with more than 50 people on Refresh", () => {
    enabled.refetched = [];
    const people = Array.from({ length: 51 }, (_, i) => `user:u${i}`).join(",");
    renderLogs(
      `/?from=2026-11-20T10:00:00.000Z&to=2026-11-20T11:00:00.000Z&person=${encodeURIComponent(people)}`,
    );
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    expect(enabled.refetched.every((n) => n === 0)).toBe(true);
  });

  it("ignores the server filter on a server's own tab, where it is not applied", () => {
    const servers = Array.from({ length: 51 }, (_, i) => `srv_${i}`).join(",");
    renderLogs(`/?server=${encodeURIComponent(servers)}`, "srv_1");
    expect(
      screen.queryByText("Too many servers or people in this link"),
    ).toBeNull();
  });
});
