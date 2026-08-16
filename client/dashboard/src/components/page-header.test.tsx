import { cleanup, render, screen } from "@testing-library/react";
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
vi.mock("react-router", () => ({
  Link: ({ children }: { children: React.ReactNode }) => <a>{children}</a>,
  useLocation: () => ({ pathname: "/" }),
  useParams: () => ({}),
}));

import { PageHeader } from "./page-header";

afterEach(cleanup);

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
});
