import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { useGatewayDetailChrome } from "./useGatewayDetailChrome";

const state = vi.hoisted(() => ({
  rows: [] as {
    classification: string;
    server?: { id?: string; slug?: string; name?: string };
    member?: { id: string; mcpServerSlug?: string };
  }[],
  memberStates: {} as Record<
    string,
    | {
        kind: string;
        why?: string;
        fromServer?: boolean;
        needsCredential?: boolean;
      }
    | undefined
  >,
  isLoadingMembers: false,
  membersFailed: false,
  userSessionIssuerId: undefined as string | undefined,
  discoveryMode: undefined as string | undefined,
  codeModeEnabled: false,
  setup: {
    gaps: [] as {
      serverId: string;
      serverSlug: string | undefined;
      name: string;
      missingEnvVars: number;
    }[],
    loading: false,
    failed: false,
  },
}));
vi.mock("@/pages/mcp/gateway/useGatewayMemberStates", () => ({
  useGatewayMemberStates: () => ({
    stateFor: (row: { server?: { slug?: string } }) =>
      state.memberStates[row.server?.slug ?? ""],
    connect: () => undefined,
    signIn: {},
  }),
}));
vi.mock("./useGatewayMemberSetup", () => ({
  useGatewayMemberSetup: () => state.setup,
}));
vi.mock("@/contexts/Auth", () => ({ useProject: () => ({ id: "project" }) }));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "example", projectSlug: "default" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true }),
}));
vi.mock("@/pages/skills/use-skills-over-mcp", () => ({
  useSkillsOverMcpEnabled: () => false,
}));
vi.mock("@/hooks/useToolsetUrl", () => ({
  useResolvedMcpServerUrl: () => ({ mcpUrl: "https://example.test/mcp" }),
  gatewayInstallPageUrl: () => undefined,
}));
vi.mock("@/pages/mcp/gateway/useGatewayMemberRows", () => ({
  useGatewayMemberRows: () => ({
    rows: state.rows,
    isLoading: state.isLoadingMembers,
    isError: state.membersFailed,
  }),
}));
vi.mock("@/pages/mcp/gateway/GatewaySettingsTab", () => ({
  GATEWAY_AUTHENTICATION_SECTION_ID: "authentication",
  GATEWAY_DISCOVERY_SECTION_ID: "discovery",
}));
vi.mock("@/pages/mcp/x/tabs/settings/sections/ServerUrlSection", () => ({
  MCP_SERVER_URL_SECTION_ID: "server-url",
}));
vi.mock("@gram/client/react-query/getMetaMcpServer.js", () => ({
  useGetMetaMcpServer: () => ({
    data: {
      id: "gw",
      name: "Gateway",
      userSessionIssuerId: state.userSessionIssuerId,
      discoveryMode: state.discoveryMode,
      codeModeEnabled: state.codeModeEnabled,
    },
  }),
}));
vi.mock("@gram/client/react-query/mcpEndpoints.js", () => ({
  useMcpEndpoints: () => ({ data: { mcpEndpoints: [] }, isLoading: false }),
}));

function Readiness() {
  const chrome = useGatewayDetailChrome();
  return (
    <ul>
      {chrome?.readinessChecks.map((check) => (
        <li
          key={check.key}
          data-ready={String(check.ready)}
          data-optional={String(!!check.optional)}
          data-broken={String(!!check.broken)}
          data-href={check.href ?? ""}
        >
          {check.label}: {check.description}
        </li>
      ))}
    </ul>
  );
}
function renderReadiness() {
  render(
    <MemoryRouter
      initialEntries={["/example/projects/default/mcp/gateway/gw/overview"]}
    >
      <Routes>
        <Route
          path="/example/projects/default/mcp/gateway/:gatewayId/:tab"
          element={<Readiness />}
        />
      </Routes>
    </MemoryRouter>,
  );
}
const row = (label: string) => screen.getByText(new RegExp(`^${label}:`));

afterEach(cleanup);
beforeEach(() => {
  state.rows = [];
  state.memberStates = {};
  state.isLoadingMembers = false;
  state.membersFailed = false;
  state.userSessionIssuerId = undefined;
  state.discoveryMode = undefined;
  state.codeModeEnabled = false;
  state.setup = { gaps: [], loading: false, failed: false };
});

it("counts only members the gateway can serve", () => {
  state.rows = [
    { classification: "unproxied" },
    { classification: "slugless" },
  ];
  renderReadiness();
  expect(row("Members").getAttribute("data-ready")).toBe("false");

  cleanup();
  state.rows = [{ classification: "hosted" }, { classification: "unproxied" }];
  renderReadiness();
  expect(row("Members").getAttribute("data-ready")).toBe("true");
  expect(row("Members").textContent).toContain("Fronting 1 MCP server.");
});

it("can't confirm members while they load", () => {
  state.isLoadingMembers = true;
  renderReadiness();
  expect(row("Members").getAttribute("data-ready")).toBe("null");
});

it("stays neutral when members fail to load", () => {
  state.membersFailed = true;
  renderReadiness();
  expect(row("Members").getAttribute("data-ready")).toBe("null");
  expect(row("Members").textContent).toContain("could not be loaded");
});

it("treats sign-in as optional, so an anonymous gateway can be ready", () => {
  renderReadiness();
  expect(row("Authentication").getAttribute("data-optional")).toBe("true");
});

it("flags a hosted member missing environment variables as broken", () => {
  state.rows = [{ classification: "hosted" }];
  state.setup = {
    gaps: [
      {
        serverId: "srv-1",
        serverSlug: "acme-support",
        name: "Acme Support Tools",
        missingEnvVars: 1,
      },
    ],
    loading: false,
    failed: false,
  };
  renderReadiness();
  const check = row("Member setup");
  expect(check.getAttribute("data-ready")).toBe("false");
  expect(check.getAttribute("data-broken")).toBe("true");
  expect(check.textContent).toContain(
    "Acme Support Tools needs 1 environment variable set before its tools can run.",
  );
  // Linked by id, since two servers can share a slug, to the block that
  // sets the variables.
  expect(check.getAttribute("data-href")).toContain(
    "/mcp/srv-1/settings#settings-environment-variables",
  );
});

it("holds member setup undetermined until its reads settle", () => {
  state.rows = [{ classification: "hosted" }];
  state.setup = { gaps: [], loading: true, failed: false };
  renderReadiness();
  expect(row("Member setup").getAttribute("data-ready")).toBe("null");
});

it("passes member setup once every hosted member is configured", () => {
  state.rows = [{ classification: "hosted" }];
  renderReadiness();
  expect(row("Member setup").getAttribute("data-ready")).toBe("true");
});

it("leaves member setup out without hosted members", () => {
  state.rows = [{ classification: "proxied" }];
  renderReadiness();
  expect(screen.queryByText(/^Member setup:/)).toBeNull();
});

it("flags Code Mode as broken when the gateway chose it but cannot run it", () => {
  state.discoveryMode = "code_mode";
  renderReadiness();
  expect(row("Code Mode").getAttribute("data-ready")).toBe("false");
  expect(row("Code Mode").getAttribute("data-broken")).toBe("true");

  cleanup();
  state.codeModeEnabled = true;
  renderReadiness();
  expect(row("Code Mode").getAttribute("data-ready")).toBe("true");

  cleanup();
  state.discoveryMode = "progressive";
  renderReadiness();
  expect(screen.queryByText(/^Code Mode:/)).toBeNull();
});

const remote = (slug: string, name: string) => ({
  classification: "proxied",
  server: { slug, name },
  member: { id: `member-${slug}`, mcpServerSlug: slug },
});

it("leaves the viewer's own sign-in out of the gateway's readiness", () => {
  state.rows = [remote("linear", "Linear")];
  state.memberStates = { linear: { kind: "sign_in" } };
  renderReadiness();
  expect(screen.queryByText(/Linear/)).toBeNull();
});

it("flags a member whose tools couldn't be read as broken", () => {
  state.rows = [remote("slack", "Slack")];
  state.memberStates = {
    slack: { kind: "unavailable", why: "Slack didn't answer." },
  };
  renderReadiness();
  const check = row("Slack's tools couldn't be read");
  expect(check.getAttribute("data-ready")).toBe("false");
  expect(check.getAttribute("data-broken")).toBe("true");
  expect(check.textContent).toContain("Slack didn't answer.");
});

it("puts a missing shared credential in plain words, linked to the member's sign-in settings", () => {
  state.rows = [
    {
      ...remote("slack", "Slack"),
      server: { id: "srv-slack", slug: "slack", name: "Slack" },
    },
  ];
  const raw =
    'server "slack" requires authentication and has no credential for this caller. Ask your administrator to connect it';
  state.memberStates = {
    slack: {
      kind: "unavailable",
      why: raw,
      fromServer: true,
      needsCredential: true,
    },
  };
  renderReadiness();
  const check = row("Slack's tools couldn't be read");
  expect(check.textContent).toContain(
    "Slack needs a shared credential before the gateway can list its tools.",
  );
  // The server's text never leads.
  expect(check.textContent).not.toContain(raw);
  expect(check.getAttribute("data-href")).toContain(
    "/mcp/srv-slack/settings#settings-sign-in",
  );
});

it("links an empty gateway's Members step to the Add servers sheet", () => {
  renderReadiness();
  expect(row("Members").getAttribute("data-href")).toContain(
    "/overview#add-servers",
  );
});

it("holds the header back while member access is checked, and stays quiet when healthy", () => {
  state.rows = [remote("linear", "Linear")];
  state.memberStates = { linear: { kind: "checking" } };
  renderReadiness();
  expect(row("Member access").getAttribute("data-ready")).toBe("null");

  cleanup();
  state.memberStates = { linear: { kind: "available" } };
  renderReadiness();
  expect(screen.queryByText(/^Member access:/)).toBeNull();
  expect(screen.queryByText(/^Sign in to/)).toBeNull();
});
