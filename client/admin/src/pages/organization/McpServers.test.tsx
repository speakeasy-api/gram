import {
  act,
  cleanup,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { AdminMcpServer } from "@/lib/gramAdminApi";
import { routeTree } from "@/routeTree.gen";
import { anOrganization, aProject } from "@/test/fixtures";
import { renderRouteTree } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  getSession: vi.fn(),
  getOrganization: vi.fn(),
  listOrganizationProjects: vi.fn(),
  listProjectMcpServers: vi.fn(),
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    getSession: mocks.getSession,
    getOrganization: mocks.getOrganization,
    listOrganizationProjects: mocks.listOrganizationProjects,
    listProjectMcpServers: mocks.listProjectMcpServers,
  };
});

const ORG = anOrganization();
// Listed newest first, the way the endpoint promises no order: the page has to
// sort to land on the oldest.
const NEWER = aProject({
  id: "proj_newer",
  name: "Newer",
  slug: "newer",
  mcp_server_count: 0,
  created_at: "2026-03-01T00:00:00Z",
});
const OLDEST = aProject({
  id: "proj_oldest",
  name: "Oldest",
  slug: "oldest",
  mcp_server_count: 2,
  created_at: "2026-01-01T00:00:00Z",
});

const LINEAR: AdminMcpServer = {
  id: "srv_linear",
  name: "Linear",
  url: "https://gram.example.test/mcp/linear",
  visibility: "public",
  source: "remote",
  created_at: "2026-02-03T12:00:00Z",
};
const LEGACY: AdminMcpServer = {
  id: "ts_legacy",
  name: "Legacy",
  visibility: "private",
  source: "toolset_only",
  created_at: "2026-02-04T12:00:00Z",
};

const SERVERS: Record<string, AdminMcpServer[]> = {
  [OLDEST.id]: [LINEAR, LEGACY],
  [NEWER.id]: [],
};

const writeText = vi.fn(() => Promise.resolve());

function shortDate(iso: string): string {
  return new Date(iso).toLocaleDateString(undefined, { timeZone: "UTC" });
}

beforeEach(() => {
  mocks.getSession.mockReset();
  mocks.getSession.mockResolvedValue({ email: "ops@example.test", name: "" });
  mocks.getOrganization.mockReset();
  mocks.getOrganization.mockResolvedValue(ORG);
  mocks.listOrganizationProjects.mockReset();
  mocks.listOrganizationProjects.mockImplementation((organizationID: string) =>
    organizationID === ORG.id
      ? Promise.resolve({ projects: [NEWER, OLDEST] })
      : Promise.reject(new Error(`no organization ${organizationID}`)),
  );
  mocks.listProjectMcpServers.mockReset();
  mocks.listProjectMcpServers.mockImplementation(
    (organizationID: string, projectID: string) =>
      organizationID === ORG.id && SERVERS[projectID]
        ? Promise.resolve({ mcp_servers: SERVERS[projectID] })
        : Promise.reject(new Error(`no project ${projectID}`)),
  );
  writeText.mockClear();
  Object.defineProperty(navigator, "clipboard", {
    value: { writeText },
    configurable: true,
    writable: true,
  });
});

afterEach(cleanup);

describe("McpServers", () => {
  it("starts on the oldest project and writes its id into the address", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/mcp-servers`,
    });

    await screen.findByRole("cell", { name: /Linear/ });
    expect(router.state.location.search).toEqual({ project: OLDEST.id });
    // Replaced, not pushed: the bare address is not left behind in history.
    expect(router.history.length).toBe(1);
    expect(
      screen.getByRole("combobox", { name: "Project" }).textContent,
    ).toContain(OLDEST.name);
  });

  it("falls back to the oldest project when the address names an unknown one", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/mcp-servers?project=proj_elsewhere`,
    });

    await screen.findByRole("cell", { name: /Linear/ });
    expect(router.state.location.search).toEqual({ project: OLDEST.id });
    expect(mocks.listProjectMcpServers).not.toHaveBeenCalledWith(
      ORG.id,
      "proj_elsewhere",
    );
  });

  it("renders each server's cells", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/mcp-servers?project=${OLDEST.id}`,
    });

    await screen.findByRole("cell", { name: /Linear/ });
    expect(
      screen.getAllByRole("columnheader").map((header) => header.textContent),
    ).toEqual(["Name", "Server URL", "Visibility", "Source", "Created"]);

    const [, linear, legacy] = screen.getAllByRole("row");
    expect(
      within(linear!)
        .getAllByRole("cell")
        .map((cell) => cell.textContent),
    ).toEqual([
      "Linear",
      LINEAR.url,
      "Public",
      "Remote",
      shortDate(LINEAR.created_at),
    ]);
    // No address is a dash, not an empty copy button.
    expect(
      within(legacy!)
        .getAllByRole("cell")
        .map((cell) => cell.textContent),
    ).toEqual([
      "Legacy",
      "-",
      "Private",
      "Legacy toolset",
      shortDate(LEGACY.created_at),
    ]);
    expect(screen.getByText("2 MCP servers")).toBeTruthy();
  });

  it("copies a server's URL", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/mcp-servers?project=${OLDEST.id}`,
    });

    const copy = await screen.findByRole("button", {
      name: "Copy Linear server URL",
    });
    await act(async () => {
      fireEvent.click(copy);
      await Promise.resolve();
    });

    expect(writeText).toHaveBeenCalledWith(LINEAR.url);
    expect(
      screen.getByRole("button", { name: "Linear server URL copied" }),
    ).toBeTruthy();
  });

  it("switches project from the picker and replaces the address", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/mcp-servers?project=${OLDEST.id}`,
    });

    const picker = await screen.findByRole("combobox", { name: "Project" });
    await screen.findByRole("cell", { name: /Linear/ });
    fireEvent.keyDown(picker, { key: "ArrowDown" });

    const options = await screen.findAllByRole("option");
    // Oldest first, each with its count.
    expect(options.map((option) => option.textContent)).toEqual([
      "Oldest2",
      "Newer0",
    ]);
    fireEvent.click(options[1]!);

    await screen.findByText("No MCP servers in this project");
    await waitFor(() =>
      expect(router.state.location.search).toEqual({ project: NEWER.id }),
    );
    expect(router.history.length).toBe(1);
  });

  it("lights the MCP Servers item in the record's sidebar", async () => {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/mcp-servers?project=${OLDEST.id}`,
    });

    const link = await screen.findByRole("link", { name: "MCP Servers" });
    expect(link.getAttribute("aria-current")).toBe("page");
  });
});
