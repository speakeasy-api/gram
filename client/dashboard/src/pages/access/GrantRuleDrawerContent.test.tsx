import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { GrantRuleDrawerContent } from "./GrantRuleDrawerContent";
import type { ServerGroup } from "./serverMerge";
import type { AssistantGroup } from "./useOrgAssistants";

const mocks = vi.hoisted(() => ({
  inventory: {
    groups: [] as ServerGroup[],
    settled: false,
    isError: false,
    refetch: vi.fn(),
  },
  assistants: {
    groups: [] as AssistantGroup[],
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

vi.mock("./useOrgAssistants", () => ({
  useOrgAssistants: (enabled: boolean) =>
    enabled
      ? mocks.assistants
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
  mocks.assistants = {
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
  it("does not claim the organization has no servers when an allow rule filters them all out", () => {
    // An exception picker only lists what its allow rule covers, and that
    // leaving nothing does not mean the organization has no servers.
    mocks.inventory = {
      ...mocks.inventory,
      settled: true,
      groups: serverGroups,
    };
    render(
      <GrantRuleDrawerContent
        resourceType="mcp"
        scope="mcp:connect"
        selectors={[]}
        onChangeSelectors={() => {}}
        isDeny
        allowSelectors={[{ resourceKind: "mcp", resourceId: "server_absent" }]}
      />,
    );
    expect(screen.queryByText("Server one")).toBeNull();
    expect(screen.queryByText("No servers found")).toBeNull();
    expect(screen.getByText("No matching servers")).toBeTruthy();
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

describe("grant rule assistant list", () => {
  it("names a chosen assistant by its own id", () => {
    mocks.assistants = {
      ...mocks.assistants,
      settled: true,
      groups: [
        {
          projectId: "project_one",
          projectName: "Project one",
          assistants: [{ id: "assistant_one", name: "Assistant one" }],
        },
      ],
    };
    const onChangeSelectors = vi.fn<(selectors: unknown) => void>();
    render(
      <GrantRuleDrawerContent
        resourceType="assistant"
        scope="assistant:read"
        selectors={[]}
        onChangeSelectors={onChangeSelectors}
      />,
    );
    fireEvent.click(screen.getByText("Assistant one"));
    expect(onChangeSelectors).toHaveBeenCalledWith([
      { resourceKind: "assistant", resourceId: "assistant_one" },
    ]);
  });
});

it("limits inference grants to projects rather than assistants or MCP servers", () => {
  const onChangeSelectors = vi.fn<() => void>();
  render(
    <GrantRuleDrawerContent
      resourceType="inference"
      scope="inference:execute"
      selectors={[]}
      onChangeSelectors={onChangeSelectors}
    />,
  );
  expect(screen.queryByText("Specific assistants")).toBeNull();
  expect(screen.queryByText("Specific servers")).toBeNull();
  fireEvent.click(screen.getByText("Project one"));
  expect(onChangeSelectors).toHaveBeenCalledWith([
    { resourceKind: "inference", resourceId: "*", projectId: "project_one" },
  ]);
});

it.each(["project", "inference"] as const)(
  "exposes %s project selection as one semantic checkbox per row",
  (resourceType) => {
    const onChangeSelectors = vi.fn();
    const props = {
      resourceType,
      scope:
        resourceType === "inference" ? "inference:execute" : "project:read",
      onChangeSelectors,
    } as const;
    const { container, rerender } = render(
      <GrantRuleDrawerContent {...props} selectors={[]} />,
    );
    const row = screen.getByRole("checkbox", { name: "Project one" });
    expect(row.tagName).toBe("BUTTON");
    expect(row.getAttribute("aria-checked")).toBe("false");
    expect(row.tabIndex).toBe(0);
    expect(container.querySelector("button button")).toBeNull();
    fireEvent.click(row);
    const selected = onChangeSelectors.mock.calls[0]![0];
    expect(selected).toEqual([
      resourceType === "inference"
        ? {
            resourceKind: "inference",
            resourceId: "*",
            projectId: "project_one",
          }
        : { resourceKind: "project", resourceId: "project_one" },
    ]);
    rerender(<GrantRuleDrawerContent {...props} selectors={selected} />);
    expect(row.getAttribute("aria-checked")).toBe("true");
    fireEvent.click(row);
    expect(onChangeSelectors).toHaveBeenLastCalledWith([]);
  },
);
