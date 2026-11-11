import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { useGatewayDetailChrome } from "./useGatewayDetailChrome";

const state = vi.hoisted(() => ({
  rows: [] as { classification: string }[],
  isLoadingMembers: false,
  userSessionIssuerId: undefined as string | undefined,
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
  }),
}));
vi.mock("@/pages/mcp/gateway/GatewaySettingsTab", () => ({
  GATEWAY_AUTHENTICATION_SECTION_ID: "authentication",
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
  state.isLoadingMembers = false;
  state.userSessionIssuerId = undefined;
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

it("treats sign-in as optional, so an anonymous gateway can be ready", () => {
  renderReadiness();
  expect(row("Authentication").getAttribute("data-optional")).toBe("true");
});
