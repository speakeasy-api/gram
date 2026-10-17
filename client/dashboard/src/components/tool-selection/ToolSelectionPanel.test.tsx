import { TooltipProvider } from "@/components/ui/Tooltip";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ToolSelectionPanel,
  type ToolSelectionServer,
} from "./ToolSelectionPanel";

const servers: ToolSelectionServer[] = [
  {
    id: "deployed",
    name: "Inventory",
    tools: [{ name: "search_inventory", annotations: [] }],
    status: "ready",
  },
  // Tools load from stored metadata only once the row is expanded.
  {
    id: "lazy",
    name: "Device fleet",
    tools: [],
    status: "loading",
    collapsedLabel: "Expand to load tools",
  },
  // Loaded, but nothing has been recorded for it yet.
  {
    id: "empty",
    name: "Badge readers",
    tools: [],
    status: "ready",
    collapsedLabel: "Expand to load tools",
    emptyContent: <p>Record this server&apos;s tools first.</p>,
  },
];

function renderPanel() {
  const onExpandedServersChange = vi.fn<(serverIds: string[]) => void>();
  render(
    <TooltipProvider>
      <ToolSelectionPanel
        servers={servers}
        mode="tools"
        selectedAnnotations={[]}
        selectedTools={[]}
        onSelectionChange={() => {}}
        onExpandedServersChange={onExpandedServersChange}
      />
    </TooltipProvider>,
  );
  return { onExpandedServersChange };
}

function search(query: string): void {
  fireEvent.change(screen.getByPlaceholderText("Search tools and servers…"), {
    target: { value: query },
  });
}

afterEach(cleanup);

describe("ToolSelectionPanel search", () => {
  it("keeps a server found by name before its tools have loaded", () => {
    const { onExpandedServersChange } = renderPanel();

    search("fleet");

    const row = screen.getByRole("button", { name: /Device fleet/ });
    expect(screen.queryByRole("button", { name: /Inventory/ })).toBeNull();
    // The search alone doesn't load it; expanding it does.
    expect(row.getAttribute("aria-expanded")).toBe("false");
    expect(onExpandedServersChange).toHaveBeenLastCalledWith([]);

    fireEvent.click(row);

    expect(row.getAttribute("aria-expanded")).toBe("true");
    expect(onExpandedServersChange).toHaveBeenLastCalledWith(["lazy"]);
    expect(screen.getByText("Loading tools…")).toBeTruthy();
  });

  it("drops a server with no loaded tools when the search names a tool", () => {
    renderPanel();

    search("search_inv");

    expect(screen.getByRole("button", { name: /Inventory/ })).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Device fleet/ })).toBeNull();
  });

  it("opens a loaded server with no tools so its guidance shows", () => {
    renderPanel();

    search("badge");

    const row = screen.getByRole("button", { name: /Badge readers/ });
    expect(row.getAttribute("aria-expanded")).toBe("true");
    expect(screen.getByText("Record this server's tools first.")).toBeTruthy();
  });
});
