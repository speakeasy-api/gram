import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

// Stub the Radix-backed primitive so we can assert TableRowContextMenu's
// mapping and selection behavior without driving a real right-click in
// happy-dom.
vi.mock("./ui/context-menu", () => ({
  ContextMenu: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  ContextMenuTrigger: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  ContextMenuContent: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  ContextMenuItem: ({
    children,
    onSelect,
    disabled,
    variant,
  }: {
    children: React.ReactNode;
    onSelect?: () => void;
    disabled?: boolean;
    variant?: string;
  }) => (
    <button disabled={disabled} data-variant={variant} onClick={() => onSelect?.()}>
      {children}
    </button>
  ),
}));
vi.mock("@speakeasy-api/moonshine", () => ({
  Icon: () => <span data-testid="icon" />,
}));

import { TableRowContextMenu } from "./table-row-context-menu";

afterEach(cleanup);

describe("TableRowContextMenu", () => {
  it("renders an item per action and invokes onClick on select", () => {
    const del = vi.fn();
    const edit = vi.fn();
    render(
      <TableRowContextMenu
        actions={[
          {
            label: "Edit",
            onClick: () => {
              edit();
            },
          },
          {
            label: "Delete",
            onClick: () => {
              del();
            },
            destructive: true,
          },
        ]}
      >
        <div>row</div>
      </TableRowContextMenu>,
    );

    expect(screen.getByText("Edit")).toBeTruthy();
    fireEvent.click(screen.getByText("Delete"));
    expect(del).toHaveBeenCalledTimes(1);
    expect(edit).not.toHaveBeenCalled();
  });

  it("marks destructive actions with the destructive variant", () => {
    render(
      <TableRowContextMenu
        actions={[
          { label: "Edit", onClick: () => {} },
          { label: "Delete", onClick: () => {}, destructive: true },
        ]}
      >
        <div>row</div>
      </TableRowContextMenu>,
    );

    expect(screen.getByText("Delete").getAttribute("data-variant")).toBe(
      "destructive",
    );
    expect(screen.getByText("Edit").getAttribute("data-variant")).toBe(
      "default",
    );
  });

  it("renders children unwrapped when there are no actions", () => {
    render(
      <TableRowContextMenu actions={[]}>
        <div data-testid="row">row</div>
      </TableRowContextMenu>,
    );

    expect(screen.getByTestId("row")).toBeTruthy();
    expect(screen.queryByText("Delete")).toBeNull();
  });
});
