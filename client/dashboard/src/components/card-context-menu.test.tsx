import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

// Stub the Radix-backed primitive so we can assert CardContextMenu's mapping and
// selection behavior without driving a real right-click in happy-dom.
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
  }: {
    children: React.ReactNode;
    onSelect?: () => void;
    disabled?: boolean;
  }) => (
    <button disabled={disabled} onClick={() => onSelect?.()}>
      {children}
    </button>
  ),
}));
vi.mock("@speakeasy-api/moonshine", () => ({
  Icon: () => <span data-testid="icon" />,
}));

import { CardContextMenu } from "./card-context-menu";

afterEach(cleanup);

describe("CardContextMenu", () => {
  it("renders an item per action and invokes onClick on select", () => {
    const del = vi.fn();
    const edit = vi.fn();
    render(
      <CardContextMenu
        actions={[
          { label: "Edit", onClick: edit },
          { label: "Delete", onClick: del, destructive: true },
        ]}
      >
        <div>card</div>
      </CardContextMenu>,
    );

    expect(screen.getByText("Edit")).toBeTruthy();
    fireEvent.click(screen.getByText("Delete"));
    expect(del).toHaveBeenCalledTimes(1);
    expect(edit).not.toHaveBeenCalled();
  });

  it("renders children unwrapped when there are no actions", () => {
    render(
      <CardContextMenu actions={[]}>
        <div data-testid="card">card</div>
      </CardContextMenu>,
    );

    expect(screen.getByTestId("card")).toBeTruthy();
    expect(screen.queryByText("Delete")).toBeNull();
  });
});
