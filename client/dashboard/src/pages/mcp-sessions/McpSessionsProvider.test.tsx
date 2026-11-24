import { TooltipProvider } from "@/components/ui/Tooltip";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import McpSessionsProvider from "./McpSessionsProvider";

const provider = (id: string, slug: string) => ({
  id,
  slug,
  projectId: "",
  sessionDurationHours: 24,
  clientIdMetadataAdmissionMode: "open",
});

const state = vi.hoisted(() => ({
  health: "on" as "pending" | "on" | "off",
  linkRows: [] as (boolean | undefined)[],
  providers: [] as unknown[],
}));

vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
}));

vi.mock("@/contexts/Auth", async (original) => ({
  ...(await original<object>()),
  useProject: () => ({ id: "project_1", slug: "project" }),
}));

// The frame's breadcrumbs and tabs need the app shell; the header is what is tested.
vi.mock("@/components/detail/detail-frame", async (original) => ({
  ...(await original<object>()),
  DetailFrame: ({
    header,
    tabsAside,
    children,
  }: {
    header?: React.ReactNode;
    tabsAside?: React.ReactNode;
    children: React.ReactNode;
  }) => (
    <>
      {header}
      {tabsAside}
      {children}
    </>
  ),
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: React.ReactNode }) => children,
}));

vi.mock("./connection-health/flag", async (original) => ({
  ...(await original<object>()),
  useConnectionHealthFlag: () => state.health,
}));

vi.mock("./providers", async (original) => ({
  ...(await original<object>()),
  useLastProvider: () => [null, vi.fn()],
  useProjectProviders: () => ({
    providers: state.providers,
    isLoading: false,
    isError: false,
  }),
}));

vi.mock("./connection-health/OverviewTab", () => ({ OverviewTab: () => null }));
vi.mock("./connection-health/ConnectionsViews", () => ({
  ConnectionsViews: () => null,
}));
vi.mock("./ConnectionsTab", () => ({ ConnectionsTab: () => null }));

vi.mock("@/pages/user-session-issuers/UserSessionIssuerDetail", () => ({
  ActionsSection: () => null,
  AdvancedSection: () => null,
  McpServersSection: ({ linkRows }: { linkRows?: boolean }) => {
    state.linkRows.push(linkRows);
    return <div data-testid="servers" />;
  },
}));

vi.mock("@gram/client/react-query/organizationUserSessionIssuer.js", () => ({
  useOrganizationUserSessionIssuer: () => ({
    data: { id: "provider_1" },
    isPending: false,
    isError: false,
  }),
}));

function Pathname() {
  return <output data-testid="pathname">{useLocation().pathname}</output>;
}

function renderProvider(tab: string) {
  render(
    <TooltipProvider>
      <MemoryRouter initialEntries={[`/mcp-sessions/provider_1/${tab}`]}>
        <Routes>
          <Route
            path="/mcp-sessions/:issuerId/*"
            element={<McpSessionsProvider />}
          />
          <Route path="*" element={null} />
        </Routes>
        <Pathname />
      </MemoryRouter>
    </TooltipProvider>,
  );
}

describe("McpSessionsProvider", () => {
  beforeEach(() => {
    state.providers = [provider("provider_1", "acme-sso")];
  });

  afterEach(() => {
    cleanup();
    state.health = "on";
    state.linkRows = [];
  });

  it("drops the header and shows no provider switch with one provider", () => {
    renderProvider("mcp-servers");
    expect(screen.queryByText("MCP Authentication Provider")).toBeNull();
    expect(screen.queryByRole("combobox")).toBeNull();
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe(
      "acme-sso",
    );
  });

  it("switches between two providers on the same tab", () => {
    state.providers = [
      provider("provider_1", "acme-sso"),
      provider("provider_2", "beta-sso"),
    ];
    renderProvider("mcp-servers");
    expect(
      screen.getByRole("button", {
        name: "What is an MCP Authentication Provider?",
      }),
    ).toBeTruthy();
    const trigger = screen.getByRole("combobox", {
      name: "MCP Authentication Provider",
    });
    expect(trigger.textContent).toContain("acme-sso");
    fireEvent.click(trigger);
    expect(screen.getByRole("option", { name: /acme-sso/ })).toBeTruthy();
    fireEvent.click(screen.getByRole("option", { name: /beta-sso/ }));
    expect(screen.getByTestId("pathname").textContent).toMatch(
      /\/mcp-sessions\/provider_2\/mcp-servers$/,
    );
  });

  it("links the MCP servers rows with connection health on", () => {
    renderProvider("mcp-servers");
    expect(screen.getByTestId("servers")).toBeTruthy();
    expect(state.linkRows.at(-1)).toBe(true);
  });

  it("renders the header and MCP servers as before with connection health off", () => {
    state.health = "off";
    renderProvider("mcp-servers");
    const eyebrow = screen.getByText("MCP Authentication Provider");
    expect(eyebrow.className).toBe("text-eyebrow");
    expect(eyebrow.children).toHaveLength(0);
    expect(screen.queryByRole("combobox")).toBeNull();
    expect(state.linkRows.at(-1)).toBe(false);
  });

  it("renders the MCP servers tab as before while the flag loads", () => {
    state.health = "pending";
    renderProvider("mcp-servers");
    const eyebrow = screen.getByText("MCP Authentication Provider");
    expect(eyebrow.children).toHaveLength(0);
    expect(screen.queryByRole("combobox")).toBeNull();
    expect(state.linkRows.at(-1)).toBe(false);
  });

  it("renders nothing on Overview while the flag loads", () => {
    state.health = "pending";
    renderProvider("overview");
    expect(screen.queryByText("MCP Authentication Provider")).toBeNull();
    expect(screen.queryByText("acme-sso")).toBeNull();
  });
});
