import type { Tool, Toolset } from "@/lib/toolTypes";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MCPPerformanceTab } from "./MCPPerformanceTab";

const { mutate, hasScope } = vi.hoisted(() => ({
  mutate: vi.fn(),
  hasScope: vi.fn(() => true),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope }),
}));

vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ capture: vi.fn() }),
}));

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({}),
}));

vi.mock("@gram/client/react-query/updateToolset.js", () => ({
  useUpdateToolsetMutation: () => ({
    mutate,
    isPending: false,
  }),
}));

vi.mock("@gram/client/react-query/toolset.js", () => ({
  invalidateAllToolset: vi.fn(),
}));

const searchDocs: Tool = {
  type: "http",
  assetId: "asset_id",
  id: "tool_search_docs",
  projectId: "project_id",
  deploymentId: "deployment_id",
  name: "search_documentation",
  canonicalName: "search_documentation",
  description: "Search product documentation",
  summary: "Search product documentation",
  toolUrn: "tools:http:docs:search_documentation",
  httpMethod: "GET",
  path: "/search",
  schema: "{}",
  createdAt: new Date("2026-01-01T00:00:00Z"),
  updatedAt: new Date("2026-01-01T00:00:00Z"),
  openapiv3DocumentId: "doc_id",
  openapiv3Operation: "GET /search",
  tags: [],
};

const otherTool: Tool = {
  ...searchDocs,
  id: "tool_other",
  name: "list_pets",
  canonicalName: "list_pets",
  description: "List pets",
  toolUrn: "tools:http:pets:list_pets",
  path: "/pets",
  openapiv3Operation: "GET /pets",
};

function toolset(overrides: Partial<Toolset> = {}): Toolset {
  return {
    accountType: "free",
    createdAt: new Date("2026-01-01T00:00:00Z"),
    id: "toolset_id",
    name: "Docs tools",
    oauthEnablementMetadata: { oauth2SecurityCount: 0 },
    organizationId: "organization_id",
    projectId: "project_id",
    promptTemplates: [],
    rawTools: [],
    resourceUrns: [],
    resources: [],
    slug: "docs-tools",
    toolSelectionMode: "dynamic",
    topLevelToolUrns: [],
    toolUrns: [searchDocs.toolUrn, otherTool.toolUrn],
    tools: [searchDocs, otherTool],
    toolsetVersion: 1,
    updatedAt: new Date("2026-01-01T00:00:00Z"),
    ...overrides,
  };
}

afterEach(() => {
  cleanup();
  mutate.mockClear();
  hasScope.mockReturnValue(true);
});

describe("MCPPerformanceTab", () => {
  it("hides always-available tools unless the server is dynamic", () => {
    render(
      <MCPPerformanceTab toolset={toolset({ toolSelectionMode: "static" })} />,
    );

    expect(screen.getByText("Tool Selection Mode")).toBeTruthy();
    expect(screen.queryByText("Always-available tools")).toBeNull();
  });

  it("lists toolset tools for pinning in dynamic mode", () => {
    render(
      <MCPPerformanceTab
        toolset={toolset({ topLevelToolUrns: [searchDocs.toolUrn] })}
      />,
    );

    expect(screen.getByText("Always-available tools")).toBeTruthy();
    const search = screen.getByRole("checkbox", {
      name: /search_documentation/i,
    });
    const pets = screen.getByRole("checkbox", { name: /list_pets/i });
    expect(search.getAttribute("aria-checked")).toBe("true");
    expect(pets.getAttribute("aria-checked")).toBe("false");
  });

  it("pins a tool via toolsets.update", () => {
    render(<MCPPerformanceTab toolset={toolset()} />);

    fireEvent.click(
      screen.getByRole("checkbox", { name: /search_documentation/i }),
    );

    expect(mutate).toHaveBeenCalledWith({
      request: {
        slug: "docs-tools",
        updateToolsetRequestBody: {
          topLevelToolUrns: [searchDocs.toolUrn],
        },
      },
    });
  });

  it("does not pin tools without mcp:write", () => {
    hasScope.mockReturnValue(false);
    render(<MCPPerformanceTab toolset={toolset()} />);

    fireEvent.click(
      screen.getByRole("checkbox", { name: /search_documentation/i }),
    );
    expect(mutate).not.toHaveBeenCalled();
  });
});
