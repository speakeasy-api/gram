import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("@/components/ui/sidebar", () => ({
  SidebarTrigger: () => <button data-testid="sidebar-trigger" />,
}));
vi.mock("@/components/ui/separator", () => ({
  Separator: () => <hr data-testid="separator" />,
}));
vi.mock("./onboarding-banner.tsx", () => ({
  OnboardingBanner: () => null,
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
