import { TooltipProvider } from "@/components/ui/Tooltip";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ToolMetadataDriftPanel } from "./ToolMetadataDriftPanel";
import type { ToolDrift } from "./toolMetadataSync";
import type { ToolMetadataActions } from "./useSyncToolMetadata";

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({
    children,
  }: {
    children: ReactNode | ((state: { disabled: boolean }) => ReactNode);
  }) =>
    typeof children === "function" ? children({ disabled: false }) : children,
}));

const drift: ToolDrift[] = [{ kind: "removed", toolName: "wipe_device" }];

function actions(): ToolMetadataActions {
  return {
    record: vi.fn<(toolName: string) => void>(),
    apply: vi.fn<(toolName: string) => void>(),
    remove: vi.fn<(toolName: string) => void>(),
    pendingTool: undefined,
  };
}

function panel(mcpServerId: string, toolActions: ToolMetadataActions) {
  return (
    <TooltipProvider>
      <ToolMetadataDriftPanel
        drift={drift}
        mcpServerId={mcpServerId}
        onSync={undefined}
        isSyncing={false}
        toolActions={toolActions}
      />
    </TooltipProvider>
  );
}

afterEach(cleanup);

describe("ToolMetadataDriftPanel per-tool removal", () => {
  it("marks a tool missing from the viewer's listing neutrally", () => {
    render(panel("srv-1", actions()));

    expect(screen.getByLabelText("Not in your listing")).toBeTruthy();
    expect(screen.queryByLabelText("Removed")).toBeNull();
  });

  it("removes only after confirmation, for the server it was opened on", () => {
    const first = actions();
    render(panel("srv-1", first));

    fireEvent.click(screen.getByRole("button", { name: /Remove wipe_device/ }));
    expect(first.remove).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Remove metadata" }));
    expect(first.remove).toHaveBeenCalledWith("wipe_device");
  });

  it("drops an open confirmation when the panel moves to another server", () => {
    const first = actions();
    const second = actions();
    const { rerender } = render(panel("srv-1", first));

    fireEvent.click(screen.getByRole("button", { name: /Remove wipe_device/ }));
    expect(
      screen.getByRole("button", { name: "Remove metadata" }),
    ).toBeTruthy();

    // Same tool name drifting on another server, with that server's actions.
    rerender(panel("srv-2", second));

    expect(
      screen.queryByRole("button", { name: "Remove metadata" }),
    ).toBeNull();
    expect(first.remove).not.toHaveBeenCalled();
    expect(second.remove).not.toHaveBeenCalled();
  });
});
