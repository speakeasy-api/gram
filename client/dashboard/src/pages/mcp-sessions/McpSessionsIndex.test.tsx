import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import McpSessionsIndex from "./McpSessionsIndex";

const { health } = vi.hoisted(() => ({ health: vi.fn() }));

vi.mock("./connection-health/flag", async (original) => ({
  ...(await original<typeof import("./connection-health/flag")>()),
  useConnectionHealthFlag: () => health(),
}));

vi.mock("./providers", () => ({
  initialProvider: () => ({ id: "provider_1" }),
  useLastProvider: () => [null],
  useProjectProviders: () => ({
    providers: [{ id: "provider_1" }],
    isLoading: false,
    isError: false,
  }),
}));

vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project_example" }),
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcpSessions: {
      provider: {
        overview: { href: (id: string) => `/p/${id}/overview` },
        connections: { href: (id: string) => `/p/${id}/connections` },
      },
    },
  }),
}));

function Landed() {
  const { pathname, search } = useLocation();
  return <span>{`${pathname}${search}`}</span>;
}

function landing(url: string): string {
  render(
    <MemoryRouter initialEntries={[url]}>
      <Routes>
        <Route path="/mcp-sessions" element={<McpSessionsIndex />} />
        <Route path="/p/*" element={<Landed />} />
      </Routes>
    </MemoryRouter>,
  );
  return screen.getByText(/^\/p\//).textContent ?? "";
}

describe("McpSessionsIndex", () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("opens Overview for a bare visit with connection health on", () => {
    health.mockReturnValue("on");
    expect(landing("/mcp-sessions")).toBe("/p/provider_1/overview");
  });

  it("opens Connections for ?view=sessions and drops the param", () => {
    health.mockReturnValue("on");
    expect(landing("/mcp-sessions?view=sessions")).toBe(
      "/p/provider_1/connections",
    );
  });

  it("opens Connections with connection health off", () => {
    health.mockReturnValue("off");
    expect(landing("/mcp-sessions")).toBe("/p/provider_1/connections");
  });

  it("drops ?view=sessions with connection health off too", () => {
    health.mockReturnValue("off");
    expect(landing("/mcp-sessions?view=sessions")).toBe(
      "/p/provider_1/connections",
    );
  });
});
