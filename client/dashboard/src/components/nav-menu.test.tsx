import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { NavButton } from "./nav-menu";

// Stub the sidebar primitive so it doesn't pull in TooltipProvider /
// react-router context. The behavior under test is the click → spinner →
// auto-revert state machine inside NavButton, not the underlying button.
vi.mock("@/components/ui/sidebar", () => ({
  SidebarMenu: ({ children }: { children: React.ReactNode }) => (
    <ul>{children}</ul>
  ),
  SidebarMenuItem: ({ children }: { children: React.ReactNode }) => (
    <li>{children}</li>
  ),
  SidebarMenuButton: ({
    children,
    onClick,
  }: {
    children: React.ReactNode;
    onClick?: () => void;
    tooltip?: unknown;
    href?: string;
    target?: string;
    isActive?: boolean;
    className?: string;
  }) => (
    <button onClick={onClick} data-testid="nav-button">
      {children}
    </button>
  ),
}));

// Type renders as a plain span — the real component isn't relevant here.
vi.mock("./ui/type", () => ({
  Type: ({ children }: { children: React.ReactNode }) => (
    <span>{children}</span>
  ),
}));

vi.mock("./product-tier-badge", () => ({
  ProductTierBadge: () => null,
}));

const TestIcon = ({ className }: { className?: string }) => (
  <svg data-testid="nav-icon" className={className} />
);

describe("NavButton click loading state", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  it("renders the icon and not the spinner by default", () => {
    render(<NavButton title="Home" Icon={TestIcon} />);
    expect(screen.getByTestId("nav-icon")).toBeTruthy();
    expect(screen.queryByTestId("nav-spinner")).toBeNull();
  });

  it("swaps the icon for a spinner on click and reverts after 600ms", () => {
    render(<NavButton title="Home" Icon={TestIcon} />);

    fireEvent.click(screen.getByTestId("nav-button"));

    // After click: icon gone, spinner present (lucide Loader2 renders an
    // <svg class="lucide-loader-circle">; .animate-spin is a more reliable
    // marker than testid since we don't mock lucide).
    expect(screen.queryByTestId("nav-icon")).toBeNull();
    const spinner = document.querySelector(".animate-spin");
    expect(spinner).toBeTruthy();

    // Advance past the loading duration — icon returns.
    act(() => {
      vi.advanceTimersByTime(600);
    });

    expect(screen.getByTestId("nav-icon")).toBeTruthy();
    expect(document.querySelector(".animate-spin")).toBeNull();
  });

  it("skips the spinner for external (target=_blank) links", () => {
    render(<NavButton title="Docs" Icon={TestIcon} target="_blank" />);

    fireEvent.click(screen.getByTestId("nav-button"));

    // External link: clicked but never enters loading state.
    expect(screen.getByTestId("nav-icon")).toBeTruthy();
    expect(document.querySelector(".animate-spin")).toBeNull();
  });

  it("invokes the onClick callback the consumer passed in", () => {
    const onClick = vi.fn();
    render(<NavButton title="Home" Icon={TestIcon} onClick={onClick} />);

    fireEvent.click(screen.getByTestId("nav-button"));

    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it("clears the pending timer on unmount", () => {
    const { unmount } = render(<NavButton title="Home" Icon={TestIcon} />);

    fireEvent.click(screen.getByTestId("nav-button"));
    expect(vi.getTimerCount()).toBe(1);

    unmount();
    expect(vi.getTimerCount()).toBe(0);
  });
});
