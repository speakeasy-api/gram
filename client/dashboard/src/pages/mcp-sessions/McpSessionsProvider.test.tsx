import { TooltipProvider } from "@/components/ui/Tooltip";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import McpSessionsProvider from "./McpSessionsProvider";

const state = vi.hoisted(() => ({
  health: "on" as "pending" | "on" | "off",
  linkRows: [] as (boolean | undefined)[],
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
    children,
  }: {
    header: React.ReactNode;
    children: React.ReactNode;
  }) => (
    <>
      {header}
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
    providers: [
      {
        id: "provider_1",
        slug: "acme-sso",
        projectId: "",
        sessionDurationHours: 24,
        clientIdMetadataAdmissionMode: "open",
      },
    ],
    isLoading: false,
    isError: false,
  }),
}));

vi.mock("./ProviderPicker", () => ({
  ProviderPicker: ({ current }: { current: { slug: string } }) => (
    <h1>{current.slug}</h1>
  ),
}));
vi.mock("./connection-health/OverviewTab", () => ({ OverviewTab: () => null }));
vi.mock("./connection-health/ConnectionsViews", () => ({
  ConnectionsViews: () => null,
}));
vi.mock("./ConnectionsTab", () => ({ ConnectionsTab: () => null }));
vi.mock("./connection-health/SessionDurationControl", () => ({
  SessionDurationControl: () => null,
}));

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

function renderProvider(tab: string) {
  render(
    <TooltipProvider>
      <MemoryRouter initialEntries={[`/mcp-sessions/provider_1/${tab}`]}>
        <Routes>
          <Route
            path="/mcp-sessions/:issuerId/*"
            element={<McpSessionsProvider />}
          />
        </Routes>
      </MemoryRouter>
    </TooltipProvider>,
  );
}

describe("McpSessionsProvider", () => {
  afterEach(() => {
    cleanup();
    state.health = "on";
    state.linkRows = [];
  });

  it("explains the provider beside the eyebrow with connection health on", () => {
    renderProvider("mcp-servers");
    expect(screen.getByText("MCP Authentication Provider")).toBeTruthy();
    const info = screen.getByRole("button", {
      name: "What is an MCP Authentication Provider?",
    });
    expect(info.tagName).toBe("BUTTON");
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
    expect(
      screen.queryByRole("button", {
        name: "What is an MCP Authentication Provider?",
      }),
    ).toBeNull();
    expect(state.linkRows.at(-1)).toBe(false);
  });

  it("renders the MCP servers tab as before while the flag loads", () => {
    state.health = "pending";
    renderProvider("mcp-servers");
    const eyebrow = screen.getByText("MCP Authentication Provider");
    expect(eyebrow.children).toHaveLength(0);
    expect(
      screen.queryByRole("button", {
        name: "What is an MCP Authentication Provider?",
      }),
    ).toBeNull();
    expect(state.linkRows.at(-1)).toBe(false);
  });

  it("renders nothing on Overview while the flag loads", () => {
    state.health = "pending";
    renderProvider("overview");
    expect(screen.queryByText("MCP Authentication Provider")).toBeNull();
    expect(screen.queryByText("acme-sso")).toBeNull();
  });
});
