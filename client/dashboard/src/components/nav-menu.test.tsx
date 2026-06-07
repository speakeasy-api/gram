import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { NAV_LOADING_DURATION_MS, NavButton } from "./nav-menu";

// Stub sidebar primitives to prevent real Radix/Tailwind sidebar from
// loading. NavButton doesn't render these but nav-menu.tsx imports them.
vi.mock("@/components/ui/sidebar", () => ({
  SidebarMenu: ({ children }: { children: React.ReactNode }) => (
    <ul>{children}</ul>
  ),
  SidebarMenuItem: ({ children }: { children: React.ReactNode }) => (
    <li>{children}</li>
  ),
}));

// Mock Link as a plain <a> so NavButton renders without a RouterProvider.
// The behavior under test is the click → nav-shimmer → auto-revert state machine,
// not routing; data-testid keeps existing click targets working.
vi.mock("react-router", () => ({
  Link: ({
    children,
    onClick,
    to: _to,
    target,
  }: {
    children: React.ReactNode;
    onClick?: React.MouseEventHandler<HTMLAnchorElement>;
    to: string;
    target?: string;
  }) => (
    <a data-testid="nav-button" onClick={onClick} target={target}>
      {children}
    </a>
  ),
}));

// Type renders as a plain span; className is forwarded so tests can observe
// loading state via the nav-shimmer class that NavButton applies.
vi.mock("./ui/type", () => ({
  Type: ({
    children,
    className,
  }: {
    children: React.ReactNode;
    className?: string;
  }) => (
    <span data-testid="nav-label" className={className}>
      {children}
    </span>
  ),
}));

vi.mock("./product-tier-badge", () => ({
  ProductTierBadge: () => null,
}));

vi.mock("./release-stage-badge", () => ({
  ReleaseStageBadge: () => null,
}));

vi.mock("./brand-gradient-rail", () => ({
  BrandGradientRail: ({ className }: { className?: string }) => (
    <div data-testid="nav-rail" className={className} />
  ),
}));

const TestIcon = ({ className }: { className?: string }) => (
  <svg data-testid="nav-icon" className={className} />
);

describe("NavButton active rail", () => {
  afterEach(cleanup);

  it("renders the gradient rail when active", () => {
    render(<NavButton title="Home" Icon={TestIcon} active />);
    expect(screen.getByTestId("nav-rail")).toBeTruthy();
  });

  it("does not render the rail when inactive", () => {
    render(<NavButton title="Home" Icon={TestIcon} />);
    expect(screen.queryByTestId("nav-rail")).toBeNull();
  });
});

describe("NavButton click loading state", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  it("renders the icon with no loading state by default", () => {
    render(<NavButton title="Home" Icon={TestIcon} />);
    screen.getByTestId("nav-icon");
    expect(screen.getByTestId("nav-label").className).not.toContain(
      "nav-shimmer",
    );
  });

  it("adds nav-shimmer on click and removes it after 600ms", () => {
    render(<NavButton title="Home" Icon={TestIcon} />);
    const label = screen.getByTestId("nav-label");

    fireEvent.click(screen.getByTestId("nav-button"));

    expect(label.className).toContain("nav-shimmer");

    act(() => {
      vi.advanceTimersByTime(NAV_LOADING_DURATION_MS);
    });

    expect(label.className).not.toContain("nav-shimmer");
  });

  it("skips nav-shimmer for external (target=_blank) links", () => {
    render(<NavButton title="Docs" Icon={TestIcon} target="_blank" />);
    const label = screen.getByTestId("nav-label");

    fireEvent.click(screen.getByTestId("nav-button"));

    // External link: clicked but never enters loading state.
    expect(label.className).not.toContain("nav-shimmer");
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
