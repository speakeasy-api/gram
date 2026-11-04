import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { BuiltinMcpSidebarNav } from "./builtin-mcp-sidebar-nav";

const state = vi.hoisted(() => ({ admin: true, credentials: vi.fn() }));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-one" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string, id: string) =>
      state.admin && scope === "org:admin" && id === "org-one",
    isLoading: false,
  }),
}));
vi.mock("@/components/mcp/useBuiltinMcpServers", () => ({
  useBuiltinMcpServers: () => ({
    servers: state.admin
      ? [
          {
            id: "builtin-one",
            key: "slack",
            name: "Slack",
            hidden: false,
            version: "1",
            url: "https://example.test/mcp/builtin/acme/project-one/slack",
            tools: [],
            environmentId: undefined,
          },
        ]
      : [],
  }),
}));
vi.mock("@/components/mcp/useBuiltinMcpCredentials", () => ({
  useBuiltinMcpCredentials: (...args: unknown[]) => {
    state.credentials(...args);
    return { isLoading: false, isError: false, ready: true };
  },
}));
vi.mock("@/components/mcp/useUpdateBuiltinMcpServer", () => ({
  useUpdateBuiltinMcpServer: () => ({ save: vi.fn(), isPending: false }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: {
      href: () => "/mcp",
      builtin: {
        overview: { href: (k: string) => `/mcp/builtin/${k}/overview` },
        tools: { href: (k: string) => `/mcp/builtin/${k}/tools` },
        authentication: {
          href: (k: string) => `/mcp/builtin/${k}/authentication`,
        },
      },
    },
  }),
}));
vi.mock("react-router", () => ({
  useLocation: () => ({ pathname: "/mcp/builtin/slack/overview" }),
  useParams: () => ({ builtinKey: "slack" }),
  Link: ({ to, children }: { to: string; children: ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}));
vi.mock("@/components/detail/detail-sidebar-nav", () => ({
  DetailSidebarNav: ({ cardContent }: { cardContent: ReactNode }) => (
    <div data-testid="rail">{cardContent}</div>
  ),
  DetailSidebarInfoLabel: ({ children }: { children: ReactNode }) => (
    <span>{children}</span>
  ),
}));
vi.mock("@/components/mcp-server-x-sidebar-nav", () => ({
  SidebarUrlRow: ({ url }: { url: string }) => <span>{url}</span>,
}));

afterEach(() => {
  cleanup();
});

beforeEach(() => {
  state.admin = true;
  state.credentials.mockClear();
});

it("renders the rail for an organization admin", () => {
  render(<BuiltinMcpSidebarNav />);
  expect(screen.getByTestId("rail")).toBeTruthy();
});

it("renders nothing for a non-admin who opens a built-in URL", () => {
  state.admin = false;
  const { container } = render(<BuiltinMcpSidebarNav />);
  // The page behind it is org:admin only; the rail must not appear alongside
  // its Unauthorized fallback.
  expect(container.innerHTML).toBe("");
  expect(screen.queryByTestId("rail")).toBeNull();
});
