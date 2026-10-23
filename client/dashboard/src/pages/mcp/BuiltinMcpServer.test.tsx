import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";

const state = vi.hoisted(() => ({
  admin: true,
  issuers: [] as { id: string; slug: string; projectId: string }[],
  issuerError: false,
  issuerLoading: false,
  userSessionIssuerId: undefined as string | undefined,
  userSessionIssuerBound: false,
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
          userSessionIssuerId: state.userSessionIssuerId,
          userSessionIssuerBound: state.userSessionIssuerBound,
          id: "builtin-one",
          key: "slack",
          name: "Slack",
          description: "Send messages in your Slack workspace.",
          hidden: true,
          version: "4",
          url: "https://api.example.test/mcp/builtin/acme/project-one/slack",
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
vi.mock("@/hooks/useEffectiveUserSessionIssuers", () => ({
  useEffectiveUserSessionIssuers: () => ({
    issuers: state.issuers,
    isLoading: state.issuerLoading,
    isError: state.issuerError,
  }),
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
// The credentials hook asks the same question the page's guard does, so the
// two have to agree in tests as they do in the app.
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string, resourceId: string) =>
      state.admin && scope === "org:admin" && resourceId === "org-one",
    hasAnyScope: () => state.admin,
    hasAllScopes: () => state.admin,
    isLoading: false,
  }),
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
  state.issuers = [];
  state.issuerError = false;
  state.issuerLoading = false;
  state.userSessionIssuerId = undefined;
  state.userSessionIssuerBound = false;
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
      /https:\/\/api\.example\.test\/mcp\/builtin\/acme\/project-one\/slack[\s\S]*Authorization: Bearer \$SPEAKEASY_AGENT_KEY/,
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

it("separates upstream credentials from inbound OAuth policy", () => {
  state.pathname = "/mcp/builtin/slack/authentication";
  render(<BuiltinMcpServerPage />);

  expect(screen.getByText("Upstream authentication")).toBeTruthy();

  // Inbound OAuth is configured separately from upstream credentials.
  expect(screen.getByText("Inbound OAuth")).toBeTruthy();
  expect(screen.queryByText(/sign-?in/i)).toBeNull();
  expect(screen.queryByText(/only agent keys/i)).toBeNull();
  expect(screen.queryByText(/unsupported|not supported/i)).toBeNull();
  // Access is gated before this page renders; restating it is not content.
  expect(screen.queryByText(/organization admins/i)).toBeNull();

  // Token choice is per tool, so the page must not promise that whichever
  // credential is stored first is the one every tool gets.
  expect(screen.queryByText(/first credential/i)).toBeNull();
  expect(screen.getByText(/some require the user token/i)).toBeTruthy();

  // Machines connect and people do not; neither fact is an OAuth claim.
  expect(
    screen.getByText(/people cannot connect to this server/i),
  ).toBeTruthy();
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

  // The address is organization- and project-addressed, so it is neither
  // shared across projects nor permanent.
  expect(screen.queryByText(/every project/i)).toBeNull();
  expect(screen.queryByText(/never changes/i)).toBeNull();
});

it("serves the endpoint the API returned rather than composing one", () => {
  render(<BuiltinMcpServerPage />);
  const snippet = screen.getByText(
    /https:\/\/api\.example\.test\/mcp\/builtin\/acme\/project-one\/slack/,
  );
  expect(snippet).toBeTruthy();
  // The synthetic id is internal; it never reaches a connection recipe.
  expect(screen.queryByText(/[0-9a-f]{8}-[0-9a-f]{4}-/i)).toBeNull();
});

it("offers the agent key as one credential, not as the only mechanism", () => {
  render(<BuiltinMcpServerPage />);
  expect(
    screen.getByText(/one of the credentials this endpoint accepts/i),
  ).toBeTruthy();
  expect(screen.queryByText(/only agent keys/i)).toBeNull();
  expect(screen.queryByText(/OAuth/i)).toBeNull();
});

it("previews the selected environment's credentials before saving", () => {
  state.pathname = "/mcp/builtin/slack/authentication";
  state.environments = [
    {
      id: "env-one",
      name: "Production",
      slug: "default",
      entries: [{ name: "SLACK_BOT_TOKEN" }],
    },
    {
      id: "env-two",
      name: "Staging",
      slug: "staging",
      entries: [],
    },
  ];
  render(<BuiltinMcpServerPage />);
  expect(screen.getByRole("link", { name: "Production" })).toBeTruthy();

  fireEvent.change(screen.getAllByRole("combobox")[0]!, {
    target: { value: "env-two" },
  });

  // The selection on screen is what the list describes; the saved one is gone.
  expect(screen.getByRole("link", { name: "Staging" })).toBeTruthy();
  expect(screen.queryByRole("link", { name: "Production" })).toBeNull();
  expect(screen.queryByText("Set")).toBeNull();
});

it("opens the tool drawer as a disclosure, not as a toggle", () => {
  state.pathname = "/mcp/builtin/slack/tools";
  render(<BuiltinMcpServerPage />);
  const row = screen.getByRole("button", { name: /slack_send_message/ });

  expect(row.getAttribute("aria-pressed")).toBeNull();
  expect(row.getAttribute("aria-haspopup")).toBe("dialog");
  expect(row.getAttribute("aria-expanded")).toBe("false");

  // The open sheet hides the rest of the page from the a11y tree, so the row
  // is read from the handle taken before it opened.
  fireEvent.click(row);
  expect(row.getAttribute("aria-expanded")).toBe("true");
});

it("saves the credential environment against the version it read", () => {
  state.pathname = "/mcp/builtin/slack/authentication";
  render(<BuiltinMcpServerPage />);
  fireEvent.change(screen.getAllByRole("combobox")[0]!, {
    target: { value: "env-one" },
  });
  fireEvent.click(screen.getAllByRole("button", { name: /save/i })[0]!);
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

it("binds an eligible organization issuer explicitly when OAuth is ambiguous", () => {
  state.pathname = "/mcp/builtin/slack/authentication";
  state.issuers = [
    { id: "issuer-one", slug: "project-login", projectId: "project-one" },
    { id: "issuer-two", slug: "org-login", projectId: "" },
    {
      id: "issuer-three",
      slug: "other-project-login",
      projectId: "project-one",
    },
  ];
  render(<BuiltinMcpServerPage />);
  expect(
    screen.getByText(/OAuth is unavailable because multiple/),
  ).toBeTruthy();
  expect(screen.getByText(/keys remain available independently/)).toBeTruthy();
  fireEvent.change(screen.getAllByRole("combobox")[1]!, {
    target: { value: "issuer-two" },
  });
  fireEvent.click(screen.getAllByRole("button", { name: /save/i })[1]!);
  expect(state.save).toHaveBeenCalledWith({
    key: "slack",
    hidden: true,
    expectedVersion: "4",
    environmentId: undefined,
    userSessionIssuerId: "issuer-two",
  });
});

it("shows an automatic default before it has been persisted", () => {
  state.pathname = "/mcp/builtin/slack/authentication";
  state.issuers = [
    { id: "issuer-one", slug: "project-login", projectId: "project-one" },
  ];
  render(<BuiltinMcpServerPage />);
  expect(
    screen.getByText("Automatic authorization server: project-login"),
  ).toBeTruthy();
});

it("does not fall back from an invalid persisted issuer binding", () => {
  state.pathname = "/mcp/builtin/slack/authentication";
  state.userSessionIssuerBound = true;
  state.userSessionIssuerId = "removed-issuer";
  state.issuers = [
    { id: "issuer-one", slug: "project-login", projectId: "project-one" },
  ];
  render(<BuiltinMcpServerPage />);
  expect(
    screen.getByText(/bound authorization server is no longer eligible/),
  ).toBeTruthy();
  expect(screen.queryByText(/Automatic authorization server:/)).toBeNull();
});

it("limits no-issuer unavailability to OAuth", () => {
  state.pathname = "/mcp/builtin/slack/authentication";
  render(<BuiltinMcpServerPage />);
  expect(
    screen.getByText(/OAuth is unavailable because no authorization server/),
  ).toBeTruthy();
  expect(screen.getByText(/keys remain available independently/)).toBeTruthy();
});

it("does not report OAuth availability from a failed issuer listing", () => {
  state.pathname = "/mcp/builtin/slack/authentication";
  state.issuerError = true;
  render(<BuiltinMcpServerPage />);
  expect(screen.getByText(/Could not load authorization servers/)).toBeTruthy();
  expect(screen.queryByText(/OAuth is unavailable because no/)).toBeNull();
  expect(
    (screen.getAllByRole("button", { name: /save/i })[1] as HTMLButtonElement)
      .disabled,
  ).toBe(true);
});

it("shows the persisted binding regardless of how it was selected", () => {
  state.pathname = "/mcp/builtin/slack/authentication";
  state.userSessionIssuerBound = true;
  state.userSessionIssuerId = "issuer-two";
  state.issuers = [
    { id: "issuer-one", slug: "project-login", projectId: "project-one" },
    { id: "issuer-two", slug: "org-login", projectId: "" },
  ];
  render(<BuiltinMcpServerPage />);
  expect(
    screen.getByText("Bound authorization server: org-login"),
  ).toBeTruthy();
  expect(screen.queryByText(/OAuth is unavailable/)).toBeNull();
  expect(
    (screen.getAllByRole("button", { name: /save/i })[1] as HTMLButtonElement)
      .disabled,
  ).toBe(true);
});

it("prefers one project issuer over multiple organization issuers", () => {
  state.pathname = "/mcp/builtin/slack/authentication";
  state.issuers = [
    { id: "issuer-one", slug: "project-login", projectId: "project-one" },
    { id: "issuer-two", slug: "org-login", projectId: "" },
    { id: "issuer-three", slug: "other-org-login", projectId: "" },
  ];
  render(<BuiltinMcpServerPage />);
  expect(
    screen.getByText("Automatic authorization server: project-login"),
  ).toBeTruthy();
  expect(screen.queryByText(/OAuth is unavailable/)).toBeNull();
});

it("does not bypass ambiguous project issuers with a unique organization issuer", () => {
  state.pathname = "/mcp/builtin/slack/authentication";
  state.issuers = [
    { id: "issuer-one", slug: "project-login", projectId: "project-one" },
    { id: "issuer-two", slug: "other-project-login", projectId: "project-one" },
    { id: "issuer-three", slug: "org-login", projectId: "" },
  ];
  render(<BuiltinMcpServerPage />);
  expect(
    screen.getByText(/OAuth is unavailable because multiple/),
  ).toBeTruthy();
  expect(screen.queryByText(/Automatic authorization server:/)).toBeNull();
});

it("uses a unique organization issuer when no project issuer exists", () => {
  state.pathname = "/mcp/builtin/slack/authentication";
  state.issuers = [{ id: "issuer-one", slug: "org-login", projectId: "" }];
  render(<BuiltinMcpServerPage />);
  expect(
    screen.getByText("Automatic authorization server: org-login"),
  ).toBeTruthy();
});
