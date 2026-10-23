import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";

const state = vi.hoisted(() => ({
  admin: true,
  builtinKey: "slack",
  pathname: "/mcp/builtin/slack/overview",
  environments: [
    {
      id: "env-one",
      name: "Production",
      slug: "default",
      entries: [] as { name: string }[],
    },
  ],
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
          description: "Send messages in your Slack workspace.",
          hidden: true,
          version: "4",
          url: "https://api.example.test/mcp/builtin/project-one/slack",
          tools: [
            {
              name: "slack_send_message",
              description: "Post a message to a channel",
              inputSchema: JSON.stringify({
                type: "object",
                required: ["channel"],
                properties: {
                  channel: { type: "string", description: "Channel id" },
                  thread: { type: "string" },
                },
              }),
            },
            {
              name: "slack_read_messages",
              description: "Read messages",
              inputSchema: '{"type":"object"}',
            },
          ],
          credentialVariables: ["SLACK_BOT_TOKEN", "SLACK_USER_TOKEN"],
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
    data: { environments: state.environments },
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
  CopyButton: () => <button>Copy URL</button>,
}));
// The real block highlights asynchronously through shiki; the snippet text is
// what this page promises, so render it directly.
vi.mock("@/components/code", () => ({
  CodeBlock: ({ children }: { children: string }) => <pre>{children}</pre>,
}));
vi.mock("@/components/ui/Select", () => ({
  Select: ({
    children,
    value,
    onValueChange,
    disabled,
  }: {
    children: ReactNode;
    value: string;
    onValueChange: (value: string) => void;
    disabled: boolean;
  }) => (
    <select
      value={value}
      onChange={(event) => onValueChange(event.target.value)}
      disabled={disabled}
    >
      {children}
    </select>
  ),
  SelectTrigger: () => null,
  SelectValue: () => null,
  SelectContent: ({ children }: { children: ReactNode }) => children,
  SelectItem: ({ children, value }: { children: ReactNode; value: string }) => (
    <option value={value}>{children}</option>
  ),
}));
vi.mock("@/components/page-layout", () => {
  const Box = ({ children }: { children: ReactNode }) => <div>{children}</div>;
  const Toolbar = Object.assign(Box, {
    Search: ({
      value,
      onChange,
      placeholder,
    }: {
      value: string;
      onChange: (next: string) => void;
      placeholder: string;
    }) => (
      <input
        aria-label={placeholder}
        value={value}
        onChange={(event) => onChange(event.target.value)}
      />
    ),
    Actions: Box,
  });
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
      Toolbar,
      Section: Object.assign(Box, {
        // Carries `area` through so tests can see which eyebrow the page asks
        // for; the real component falls back to the sidebar area without it.
        Title: ({ area, children }: { area?: string; children: ReactNode }) => (
          <div>
            {area ? <span data-slot="page-eyebrow">{area}</span> : null}
            {children}
          </div>
        ),
        Description: Box,
        CTA: Box,
        Body: Box,
      }),
    }),
  };
});
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: {
      href: () => "/mcp",
      Link: ({ children }: { children: ReactNode }) => (
        <a href="/mcp">{children}</a>
      ),
      builtin: {
        overview: { href: (key: string) => `/mcp/builtin/${key}/overview` },
        tools: { href: (key: string) => `/mcp/builtin/${key}/tools` },
        authentication: {
          href: (key: string) => `/mcp/builtin/${key}/authentication`,
        },
      },
    },
    identities: { agents: { href: () => "/identities/agents" } },
    environments: {
      href: () => "/environments",
      environment: { href: (slug: string) => `/environments/${slug}` },
    },
  }),
}));
vi.mock("react-router", () => ({
  useParams: () => ({ builtinKey: state.builtinKey }),
  useLocation: () => ({ pathname: state.pathname }),
  Navigate: ({ to }: { to: string }) => <div>redirect:{to}</div>,
  Link: ({ to, children }: { to: string; children: ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}));

import BuiltinMcpServerPage from "./BuiltinMcpServer";

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  state.admin = true;
  state.builtinKey = "slack";
  state.pathname = "/mcp/builtin/slack/overview";
  state.environments = [
    { id: "env-one", name: "Production", slug: "default", entries: [] },
  ];
});

it("leads with connecting an agent and with what is missing before it can work", () => {
  render(<BuiltinMcpServerPage />);
  expect(
    screen.getByRole("navigation", { name: "Breadcrumb" }).textContent,
  ).toBe("Slack");
  expect(screen.getByText("Direct connection")).toBeTruthy();
  // The address is handed over ready to paste, not as a bare line to retype.
  expect(
    screen.getByText(
      /https:\/\/api\.example\.test\/mcp\/builtin\/project-one\/slack[\s\S]*Authorization: Bearer \$SPEAKEASY_AGENT_KEY/,
    ),
  ).toBeTruthy();
  // The one thing standing between the server and a working call, with a way
  // to fix it rather than a statement of fact.
  expect(
    screen.getByText("Add Slack credentials to use these tools."),
  ).toBeTruthy();
  expect(
    screen.getByRole("link", { name: "Add credentials" }).getAttribute("href"),
  ).toBe("/mcp/builtin/slack/authentication");
});

it("stays quiet about credentials once one is stored", () => {
  state.environments = [
    {
      id: "env-one",
      name: "Production",
      slug: "default",
      entries: [{ name: "slack_bot_token" }],
    },
  ];
  render(<BuiltinMcpServerPage />);
  expect(screen.queryByText(/Add Slack credentials/i)).toBeNull();
});

it("browses tools by name with their parameters, never a raw schema", () => {
  state.pathname = "/mcp/builtin/slack/tools";
  render(<BuiltinMcpServerPage />);
  expect(screen.getByText("2 of 2 tools")).toBeTruthy();

  fireEvent.change(screen.getByLabelText("Search tools"), {
    target: { value: "send" },
  });
  expect(screen.getByText("1 of 2 tools")).toBeTruthy();
  expect(screen.queryByText("slack_read_messages")).toBeNull();

  fireEvent.click(screen.getByRole("button", { name: /slack_send_message/ }));
  expect(screen.getByText("Parameters")).toBeTruthy();
  expect(screen.getByText("channel")).toBeTruthy();
  expect(screen.getByText("Required")).toBeTruthy();
  expect(screen.queryByText(/"type":\s*"object"/)).toBeNull();
});

it("reports each upstream credential and where it is stored", () => {
  state.pathname = "/mcp/builtin/slack/authentication";
  state.environments = [
    {
      id: "env-one",
      name: "Production",
      slug: "default",
      entries: [{ name: "SLACK_BOT_TOKEN" }],
    },
  ];
  render(<BuiltinMcpServerPage />);
  expect(screen.getByText("Slack bot token")).toBeTruthy();
  expect(screen.getByText("Set")).toBeTruthy();
  expect(screen.getByText("Slack user token")).toBeTruthy();
  expect(screen.getByText("Not set")).toBeTruthy();
  expect(
    screen.getByRole("link", { name: "Production" }).getAttribute("href"),
  ).toBe("/environments/default");
});

it("keeps the authentication tab about upstream credentials, not inbound policy", () => {
  state.pathname = "/mcp/builtin/slack/authentication";
  render(<BuiltinMcpServerPage />);

  expect(screen.getByText("Upstream authentication")).toBeTruthy();

  // How a client authenticates to reach this server is not settled here, so
  // the tab never rules a way in or out.
  expect(screen.queryByText(/OAuth/i)).toBeNull();
  expect(screen.queryByText(/sign-?in/i)).toBeNull();
  expect(screen.queryByText(/only agent keys/i)).toBeNull();
  expect(screen.queryByText(/unsupported|not supported/i)).toBeNull();
  // Access is gated before this page renders; restating it is not content.
  expect(screen.queryByText(/organization admins/i)).toBeNull();

  // Token choice is per tool, so the page must not promise that whichever
  // credential is stored first is the one every tool gets.
  expect(screen.queryByText(/first credential/i)).toBeNull();
  expect(screen.getByText(/some require the user token/i)).toBeTruthy();
});

it.each(["overview", "tools", "authentication"])(
  "names the %s area as MCP Servers rather than inheriting the sidebar's",
  (tab) => {
    state.pathname = `/mcp/builtin/slack/${tab}`;
    render(<BuiltinMcpServerPage />);
    const eyebrows = document.querySelectorAll('[data-slot="page-eyebrow"]');
    expect(eyebrows.length).toBeGreaterThan(0);
    for (const eyebrow of eyebrows) {
      expect(eyebrow.textContent).toBe("MCP Servers");
    }
  },
);

it("scopes the connect recipe to a direct connection and claims no stable address", () => {
  render(<BuiltinMcpServerPage />);
  expect(screen.getByText("Direct connection")).toBeTruthy();

  // The address carries a project id, so it is neither shared across projects
  // nor permanent.
  expect(screen.queryByText(/every project/i)).toBeNull();
  expect(screen.queryByText(/never changes/i)).toBeNull();
});

it("saves the credential environment against the version it read", () => {
  state.pathname = "/mcp/builtin/slack/authentication";
  render(<BuiltinMcpServerPage />);
  fireEvent.change(screen.getByRole("combobox"), {
    target: { value: "env-one" },
  });
  fireEvent.click(screen.getByRole("button", { name: /save/i }));
  expect(state.save).toHaveBeenCalledWith({
    key: "slack",
    expectedVersion: "4",
    hidden: true,
    environmentId: "env-one",
  });
});

it("does not mount details or query the catalog for non-admins", () => {
  state.admin = false;
  render(<BuiltinMcpServerPage />);
  expect(state.catalog).not.toHaveBeenCalled();
  expect(screen.queryByText("Direct connection")).toBeNull();
});

it("does not reinterpret unknown keys as a built-in that exists", () => {
  state.builtinKey = "unknown";
  state.pathname = "/mcp/builtin/unknown/overview";
  render(<BuiltinMcpServerPage />);
  expect(screen.getByText("Built-in server not found")).toBeTruthy();
});

it("sends a bare built-in address to its overview", () => {
  state.pathname = "/mcp/builtin/slack";
  render(<BuiltinMcpServerPage />);
  expect(screen.getByText("redirect:/mcp/builtin/slack/overview")).toBeTruthy();
});
