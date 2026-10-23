import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { Children, isValidElement, type ReactNode } from "react";

const state = vi.hoisted(() => ({
  admin: true,
  hidden: true,
  builtinKey: "slack",
  save: vi.fn(),
  catalog: vi.fn(),
}));
vi.mock("@/components/mcp/useBuiltinMcpServers", () => ({
  useBuiltinMcpServers: () => {
    state.catalog();
    return {
      isLoading: false,
      isError: false,
      servers: [
        {
          id: "builtin-one",
          key: "slack",
          name: "Slack",
          description: "Slack tools",
          hidden: state.hidden,
          version: "0",
          url: "https://api.example.test/mcp/builtin/project-one/slack",
          tools: [
            {
              name: "slack_read_messages",
              description: "Read messages",
              inputSchema: '{"type":"object"}',
            },
          ],
          credentialVariables: [
            "SLACK_BOT_TOKEN",
            "SLACK_USER_TOKEN",
            "SLACK_TOKEN",
          ],
        },
      ],
    };
  },
}));
vi.mock("@/components/mcp/useUpdateBuiltinMcpServer", () => ({
  useUpdateBuiltinMcpServer: () => ({ save: state.save, isPending: false }),
}));
vi.mock("@gram/client/react-query/listEnvironments.js", () => ({
  useListEnvironments: () => ({
    data: { environments: [] },
    isLoading: false,
    isError: false,
  }),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-one" }),
  useProject: () => ({ id: "project-one" }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "project-one",
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({
    children,
    scope,
    resourceId,
  }: {
    children: ReactNode;
    scope: string;
    resourceId: string;
  }) =>
    state.admin && scope === "org:admin" && resourceId === "org-one"
      ? children
      : null,
}));
vi.mock("@/components/ui/CopyButton", () => ({
  CopyButton: () => <button>Copy MCP URL</button>,
}));
vi.mock("@/components/page-layout", () => {
  const Box = ({ children }: { children: ReactNode }) => <div>{children}</div>;
  return {
    Page: Object.assign(Box, {
      Header: Object.assign(Box, {
        Breadcrumbs: ({
          substitutions,
        }: {
          substitutions: Record<string, string>;
        }) => <nav aria-label="Breadcrumb">{substitutions.slack}</nav>,
      }),
      Body: Box,
      Section: Object.assign(
        ({ children }: { children: ReactNode }) => (
          <div>
            {Children.toArray(children).filter(
              (child) => isValidElement(child) && child.type === Box,
            )}
          </div>
        ),
        { Title: Box, Description: Box, CTA: Box, Body: Box },
      ),
    }),
  };
});
vi.mock("@/routes", () => ({
  useRoutes: () => ({ settings: { href: () => "/settings" } }),
}));
vi.mock("react-router", () => ({
  useParams: () => ({ builtinKey: state.builtinKey }),
  Link: ({ to, children }: { to: string; children: ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}));

import BuiltinMcpServerPage from "./BuiltinMcpServer";
afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  state.admin = true;
  state.hidden = true;
  state.builtinKey = "slack";
});

it("keeps hidden built-ins addressable to admins with a read-only tool inventory", () => {
  render(<BuiltinMcpServerPage />);
  expect(screen.getByText("Hidden from discovery")).toBeTruthy();
  expect(
    screen.getByRole("navigation", { name: "Breadcrumb" }).textContent,
  ).toBe("Slack");
  expect(screen.getAllByText("Slack")).toHaveLength(2);
  expect(screen.getByText("Built-in")).toBeTruthy();
  expect(
    screen.getByText("https://api.example.test/mcp/builtin/project-one/slack"),
  ).toBeTruthy();
  expect(screen.getByText("slack_read_messages")).toBeTruthy();
  expect(screen.getByText(/human connections are blocked/i)).toBeTruthy();
  expect(
    screen.queryByRole("button", { name: /connect|test|delete/i }),
  ).toBeNull();
  expect(
    screen
      .getByRole("link", { name: "Manage visibility in Project Settings" })
      .getAttribute("href"),
  ).toBe("/settings#builtin-mcp-servers");
});
it("does not mount details or query the catalog for non-admins", () => {
  state.admin = false;
  render(<BuiltinMcpServerPage />);
  expect(state.catalog).not.toHaveBeenCalled();
  expect(screen.queryByText("Slack")).toBeNull();
});
it("does not reinterpret unknown keys as Slack", () => {
  state.builtinKey = "unknown";
  render(<BuiltinMcpServerPage />);
  expect(screen.getByText("Built-in server not found.")).toBeTruthy();
});
