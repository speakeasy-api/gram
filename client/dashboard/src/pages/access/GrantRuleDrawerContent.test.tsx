import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { GrantRuleDrawerContent } from "./GrantRuleDrawerContent";
import type { ServerGroup } from "./serverMerge";

const mocks = vi.hoisted(() => ({
  inventory: {
    groups: [] as ServerGroup[],
    settled: false,
    isError: false,
    refetch: vi.fn(),
  },
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    id: "org_example",
    projects: [{ id: "project_one", name: "Project one", slug: "project-one" }],
  }),
}));
vi.mock("./useOrgMcpServers", () => ({
  useOrgMcpServers: (enabled: boolean) =>
    enabled
      ? mocks.inventory
      : { groups: [], settled: false, isError: false, refetch: vi.fn() },
}));

const serverGroups: ServerGroup[] = [
  {
    projectId: "project_one",
    projectName: "Project one",
    servers: [
      {
        id: "server_one",
        name: "Server one",
        slug: "server-one",
        tools: [{ id: "tool_one", name: "search", type: "http" }],
        dynamicTools: false,
        remoteBacked: false,
      },
    ],
  },
];

function setup(resourceType: "mcp" | "project" = "mcp") {
  return render(
    <GrantRuleDrawerContent
      resourceType={resourceType}
      scope={resourceType === "mcp" ? "mcp:connect" : "project:read"}
      selectors={[]}
      onChangeSelectors={() => {}}
    />,
  );
}

beforeEach(() => {
  mocks.inventory = {
    groups: [],
    settled: false,
    isError: false,
    refetch: vi.fn(),
  };
});
afterEach(cleanup);

describe("grant rule server list", () => {
  it("says the inventory is loading rather than empty while it is pending", () => {
    setup();
    expect(screen.getByText("Loading servers…")).toBeTruthy();
    expect(screen.queryByText("No servers found")).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
  });
  it("says the inventory is unavailable, with a retry, when a read fails", () => {
    mocks.inventory = { ...mocks.inventory, isError: true };
    setup();
    expect(screen.getByText("Servers unavailable")).toBeTruthy();
    expect(screen.queryByText("No servers found")).toBeNull();
    expect(screen.getByRole("alert").textContent).toMatch(
      /Could not load this organization/,
    );
    screen.getByRole("button", { name: "Retry" }).click();
    expect(mocks.inventory.refetch).toHaveBeenCalledTimes(1);
  });
  it("reports an empty inventory only after a settled, successful read", () => {
    mocks.inventory = { ...mocks.inventory, settled: true };
    setup();
    expect(screen.getByText("No servers found")).toBeTruthy();
    expect(screen.queryByText("Loading servers…")).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
  });
  it("renders the servers once the inventory settles with rows", () => {
    mocks.inventory = {
      ...mocks.inventory,
      settled: true,
      groups: serverGroups,
    };
    setup();
    expect(screen.getByText("Server one")).toBeTruthy();
    expect(screen.queryByText("Loading servers…")).toBeNull();
    expect(screen.queryByText("No servers found")).toBeNull();
  });
  it("never shows inventory state in a drawer that does not read it", () => {
    // A project-scoped drawer disables the hook, so a failed MCP read must not
    // reach it as a loading or unavailable state.
    mocks.inventory = { ...mocks.inventory, isError: true };
    setup("project");
    expect(screen.queryByText("Loading servers…")).toBeNull();
    expect(screen.queryByText("Servers unavailable")).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
  });
});
