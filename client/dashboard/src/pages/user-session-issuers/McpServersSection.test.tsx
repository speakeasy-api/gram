import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import type { UserSessionIssuerMcpServer } from "@gram/client/models/components/usersessionissuermcpserver.js";
import { cleanup, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { McpServersSection } from "./UserSessionIssuerDetail";

const servers = vi.hoisted(() => ({
  items: [] as UserSessionIssuerMcpServer[],
}));

vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "current" }),
}));

vi.mock("@/contexts/Auth", async (original) => ({
  ...(await original<object>()),
  useOrganization: () => ({
    projects: [
      { id: "project_a", slug: "alpha" },
      { id: "project_b", slug: "beta" },
    ],
  }),
}));

vi.mock("./ClientDriftWarning", () => ({ ClientDriftWarning: () => null }));

vi.mock(
  "@gram/client/react-query/organizationUserSessionIssuerMcpServers.js",
  () => ({
    useOrganizationUserSessionIssuerMcpServersInfinite: () => ({
      isPending: false,
      isError: false,
      hasNextPage: false,
      data: { pages: [{ result: { items: servers.items } }] },
    }),
  }),
);

const issuer = { id: "issuer_1" } as UserSessionIssuer;

function renderSection(linkRows?: boolean) {
  render(
    <MemoryRouter>
      <McpServersSection issuer={issuer} titled={false} linkRows={linkRows} />
    </MemoryRouter>,
  );
}

describe("McpServersSection", () => {
  afterEach(() => {
    cleanup();
    servers.items = [];
  });

  it("lists servers without links by default, as before", () => {
    servers.items = [
      {
        id: "srv_1",
        kind: "mcp_server",
        name: "Linear",
        slug: "linear",
        projectId: "project_a",
        projectName: "Alpha",
      },
    ];
    renderSection();
    expect(screen.getByText("Linear")).toBeTruthy();
    expect(screen.queryAllByRole("link")).toHaveLength(0);
  });

  it("links each row to its own Clients and Sessions tab in its project", () => {
    servers.items = [
      {
        id: "srv_1",
        kind: "mcp_server",
        name: "Linear",
        slug: "linear",
        projectId: "project_a",
        projectName: "Alpha",
      },
      {
        id: "11111111-1111-4111-8111-111111111111",
        kind: "mcp_server",
        name: "Reserved",
        slug: "catalog",
        projectId: "project_a",
        projectName: "Alpha",
      },
      {
        id: "gw_1",
        kind: "meta_mcp_server",
        name: "Team gateway",
        projectId: "project_b",
        projectName: "Beta",
      },
    ];
    renderSection(true);
    expect(
      screen
        .getByRole("link", { name: "Open MCP server Linear" })
        .getAttribute("href"),
    ).toBe("/org/projects/alpha/mcp/linear/sessions");
    expect(
      screen
        .getByRole("link", { name: "Open MCP server Reserved" })
        .getAttribute("href"),
    ).toBe(
      "/org/projects/alpha/mcp/11111111-1111-4111-8111-111111111111/sessions",
    );
    expect(
      screen
        .getByRole("link", { name: "Open gateway Team gateway" })
        .getAttribute("href"),
    ).toBe("/org/projects/beta/mcp/gateway/gw_1/sessions");
  });

  it("leaves a row unlinked when its project is not one the viewer has", () => {
    servers.items = [
      {
        id: "srv_2",
        kind: "mcp_server",
        name: "Notion",
        slug: "notion",
        projectId: "project_unknown",
        projectName: "Elsewhere",
      },
    ];
    renderSection(true);
    const row = screen.getByText("Notion").closest("tr");
    expect(row).toBeTruthy();
    expect(within(row!).queryAllByRole("link")).toHaveLength(0);
  });
});
