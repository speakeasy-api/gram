import { cleanup, render as rtlRender, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("@/components/ui/Sidebar", () => ({
  SidebarTrigger: () => <button data-testid="sidebar-trigger" />,
}));
vi.mock("@/components/ui/Separator", () => ({
  Separator: () => <hr data-testid="separator" />,
}));
vi.mock("./onboarding-banner.tsx", () => ({
  OnboardingBanner: () => null,
}));
// The banner owns its own tier, scope and usage rules (see
// billing/billing-banners.test.tsx); what belongs here is that the header is
// the thing that mounts it, so it reaches every page that has a header.
vi.mock("./billing/billing-banners.tsx", () => ({
  PaygCapPausedBanner: () => <div data-testid="cap-paused-banner" />,
}));
// Stub context/hook modules imported at the top of page-header.tsx (used only
// in PageHeaderBreadcrumbs, not PageHeaderComponent, but they execute on import)
vi.mock("@/contexts/Sdk.tsx", () => ({ useSlugs: () => ({}) }));
vi.mock("@/contexts/Auth.tsx", () => ({
  useOrganization: () => ({}),
  useProject: () => ({}),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasAnyScope: () => false }),
}));
import { PageHeader } from "./page-header";

afterEach(cleanup);

// The route the header sits on is what decides whether the cap banner shows,
// so the real router resolves it — a stubbed `useLocation` would let a suffix
// check pass for a path the router would never match.
function render(
  ui: ReactNode,
  { at = "/placeholder-organization/projects" }: { at?: string } = {},
) {
  return rtlRender(<MemoryRouter initialEntries={[at]}>{ui}</MemoryRouter>);
}

describe("PageHeader.Actions", () => {
  it("renders action children in the toolbar", () => {
    render(
      <PageHeader>
        <PageHeader.Actions>
          <button data-testid="page-action">New</button>
        </PageHeader.Actions>
      </PageHeader>,
    );
    expect(screen.getByTestId("page-action")).toBeTruthy();
  });
});

describe("PageHeader", () => {
  // Chat going quiet is felt on whichever page the user was working on, so the
  // banner that explains it hangs off the header rather than the billing page.
  it("carries the chat spend cap banner on every page with a header", () => {
    render(
      <PageHeader>
        <span>crumbs</span>
      </PageHeader>,
    );

    expect(screen.getByTestId("cap-paused-banner")).toBeTruthy();
  });

  it("leaves billing-page banner ordering to the billing page", () => {
    render(
      <PageHeader>
        <span>crumbs</span>
      </PageHeader>,
      { at: "/placeholder-organization/billing" },
    );

    expect(screen.queryByTestId("cap-paused-banner")).toBeNull();
  });

  // Only the org billing route hands its banners to the page. A project path
  // that happens to end in the same segment is a different page, and dropping
  // the banner there would hide a paused organization from the work it was
  // interrupting.
  it.each([
    "/placeholder-organization/projects/billing",
    "/placeholder-organization/projects/placeholder-project/billing",
  ])("keeps the banner on %s", (at) => {
    render(
      <PageHeader>
        <span>crumbs</span>
      </PageHeader>,
      { at },
    );

    expect(screen.getByTestId("cap-paused-banner")).toBeTruthy();
  });
});
