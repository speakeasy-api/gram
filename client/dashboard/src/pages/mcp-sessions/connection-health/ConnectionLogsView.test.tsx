import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ConnectionLogsView } from "./ConnectionLogsView";

vi.mock("./api", async (original) => ({
  ...(await original<object>()),
  useRangeClock: () => [new Date("2026-11-20T12:00:00Z"), vi.fn()],
  useAttemptCounts: () => ({
    data: { totalAttempts: 0, loggingEnabled: true, viewerScope: "all" },
    isPlaceholderData: false,
    isFetching: false,
    isError: false,
    refetch: vi.fn(),
  }),
  useConnectionAttempts: () => ({
    data: { pages: [{ attempts: [] }] },
    isPending: false,
    isError: false,
    isFetching: false,
    isPlaceholderData: false,
    hasNextPage: false,
    fetchNextPage: vi.fn(),
    refetch: vi.fn(),
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
  afterEach(cleanup);

  it("shows no server chip, reset or filter wording for the fixed server", () => {
    renderLogs("/?server=srv_other", "srv_1");
    expect(screen.queryByText(/srv_/)).toBeNull();
    expect(screen.queryByText(/^Server/)).toBeNull();
    expect(screen.queryByText("Reset filters")).toBeNull();
    expect(screen.getByText(/^No attempts in the/)).toBeTruthy();
    const open = screen.getByRole("link", {
      name: /Open in MCP Sign-in with this server/,
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
});
