import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AppRoute } from "@/routes";
import {
  CollapsibleNavGroup,
  CollapsibleNavItem,
  NAV_LOADING_DURATION_MS,
  NavButton,
  NavGroupProvider,
} from "./nav-menu";

// Stub sidebar primitives to prevent real Radix/Tailwind sidebar from
// loading. NavButton doesn't render these but nav-menu.tsx imports them.
vi.mock("@/components/ui/Sidebar", () => ({
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

// Text renders as a plain span; className is forwarded so tests can observe
// loading state via the nav-shimmer class that NavButton applies.
vi.mock("@/components/ui/Text", () => ({
  Text: ({
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
    render(
      <NavButton title="Home" Icon={TestIcon} onClick={() => void onClick()} />,
    );

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

// ---------------------------------------------------------------------------
// Group expansion state
// ---------------------------------------------------------------------------

function makeRoute(title: string): AppRoute {
  return {
    title,
    url: title.toLowerCase(),
    href: () => `/${title.toLowerCase()}`,
    active: false,
  } as unknown as AppRoute;
}

function Groups({
  activeGroup,
  defaultOpenGroups,
}: {
  activeGroup?: string;
  defaultOpenGroups?: string[];
}) {
  return (
    <NavGroupProvider
      activeGroup={activeGroup}
      defaultOpenGroups={defaultOpenGroups}
    >
      <CollapsibleNavGroup label="Observe" Icon={TestIcon}>
        <CollapsibleNavItem item={makeRoute("Costs")} />
      </CollapsibleNavGroup>
      <CollapsibleNavGroup label="Connect" Icon={TestIcon}>
        <CollapsibleNavItem item={makeRoute("Sources")} />
      </CollapsibleNavGroup>
    </NavGroupProvider>
  );
}

describe("NavGroupProvider group expansion", () => {
  afterEach(() => {
    cleanup();
  });

  it("renders all groups collapsed when nothing is active and no defaults", () => {
    render(<Groups />);

    expect(screen.queryByText("Costs")).toBeNull();
    expect(screen.queryByText("Sources")).toBeNull();
    screen.getByRole("button", { name: "Expand Observe" });
    screen.getByRole("button", { name: "Expand Connect" });
  });

  it("expands only the active group", () => {
    render(<Groups activeGroup="Observe" />);

    screen.getByText("Costs");
    expect(screen.queryByText("Sources")).toBeNull();
  });

  it("collapses the previously active group when navigating to another group", () => {
    const { rerender } = render(<Groups activeGroup="Observe" />);
    screen.getByText("Costs");

    rerender(<Groups activeGroup="Connect" />);

    expect(screen.queryByText("Costs")).toBeNull();
    screen.getByText("Sources");
  });

  it("keeps explicitly expanded groups open across navigation", () => {
    const { rerender } = render(<Groups activeGroup="Observe" />);

    fireEvent.click(screen.getByRole("button", { name: "Expand Connect" }));
    screen.getByText("Sources");

    // Navigate to a top-level page: the active-only group collapses, the
    // explicitly expanded one stays open.
    rerender(<Groups />);

    expect(screen.queryByText("Costs")).toBeNull();
    screen.getByText("Sources");
  });

  it("collapses an open group via its chevron without navigating", () => {
    render(<Groups activeGroup="Observe" />);
    screen.getByText("Costs");

    fireEvent.click(screen.getByRole("button", { name: "Collapse Observe" }));

    expect(screen.queryByText("Costs")).toBeNull();
  });

  it("collapses a chevron-expanded group again on second chevron click", () => {
    const { rerender } = render(<Groups />);

    fireEvent.click(screen.getByRole("button", { name: "Expand Connect" }));
    screen.getByText("Sources");

    fireEvent.click(screen.getByRole("button", { name: "Collapse Connect" }));
    expect(screen.queryByText("Sources")).toBeNull();

    // A group collapsed by hand is no longer "explicit": navigating into a
    // different group on the same provider must not resurrect it.
    rerender(<Groups activeGroup="Observe" />);
    screen.getByText("Costs");
    expect(screen.queryByText("Sources")).toBeNull();
  });

  it("opens defaultOpenGroups initially (org sidebar behavior)", () => {
    render(<Groups defaultOpenGroups={["Observe", "Connect"]} />);

    screen.getByText("Costs");
    screen.getByText("Sources");
  });
});
