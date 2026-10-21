import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { unrecognized } from "@gram/client/types/unrecognized.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ToolFilteringSection } from "./ToolFilteringSection";

const mocks = vi.hoisted(() => ({
  groups: [] as { id: string; name: string }[],
  create: vi.fn(),
  update: vi.fn(),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));
vi.mock("@gram/client/react-query/toolVariationGroups.js", () => ({
  useToolVariationGroups: () => ({
    data: { groups: mocks.groups },
    isLoading: false,
  }),
  invalidateAllToolVariationGroups: vi.fn(),
}));
vi.mock("@gram/client/react-query/createGlobalToolVariationGroup.js", () => ({
  useCreateGlobalToolVariationGroupMutation: () => ({
    mutate: mocks.create,
    isPending: false,
  }),
}));
vi.mock("@gram/client/react-query/updateMcpServer.js", () => ({
  useUpdateMcpServerMutation: () => ({
    mutate: mocks.update,
    isPending: false,
  }),
}));
const server = {
  id: "server-1",
  visibility: "private",
  name: "Example server",
} as McpServer;
afterEach(() => {
  cleanup();
  mocks.groups = [];
  vi.clearAllMocks();
});

describe("unsupported tool filtering configuration", () => {
  it("prevents creating a group when visibility is unknown", () => {
    render(
      <ToolFilteringSection
        mcpServer={{ ...server, visibility: unrecognized("future_visibility") }}
      />,
    );
    const enable = screen.getByRole("button", { name: "Enable" });
    expect(enable).toHaveProperty("disabled", true);
    fireEvent.click(enable);
    expect(mocks.create).not.toHaveBeenCalled();
    expect(mocks.update).not.toHaveBeenCalled();
    expect(
      screen.getByText(/Refresh the page before changing tool filtering/),
    ).toBeTruthy();
  });

  it("blocks selecting and saving a group when a dirty draft becomes unsupported", () => {
    mocks.groups = [{ id: "group-1", name: "Example filter" }];
    const { rerender } = render(<ToolFilteringSection mcpServer={server} />);
    fireEvent.click(screen.getByRole("combobox", { name: "Tool filtering" }));
    fireEvent.click(screen.getByRole("option", { name: "Example filter" }));
    expect(screen.getByRole("button", { name: "Save" })).toHaveProperty(
      "disabled",
      false,
    );
    rerender(
      <ToolFilteringSection
        mcpServer={{ ...server, visibility: unrecognized("future_visibility") }}
      />,
    );
    expect(
      screen.getByRole("combobox", { name: "Tool filtering" }),
    ).toHaveProperty("disabled", true);
    const save = screen.getByRole("button", { name: "Save" });
    expect(save).toHaveProperty("disabled", true);
    fireEvent.click(save);
    expect(mocks.create).not.toHaveBeenCalled();
    expect(mocks.update).not.toHaveBeenCalled();
  });

  it("still allows enabling filtering with a known visibility", () => {
    render(<ToolFilteringSection mcpServer={server} />);
    fireEvent.click(screen.getByRole("button", { name: "Enable" }));
    expect(mocks.create).toHaveBeenCalledExactlyOnceWith({});
  });
});
