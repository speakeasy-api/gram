import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

const pathname = vi.hoisted(() => ({ current: "/acme" }));

vi.mock("@/contexts/Sdk", () => ({ useSlugs: () => ({ orgSlug: "acme" }) }));
vi.mock("react-router", () => ({
  useLocation: () => ({ pathname: pathname.current }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    setup: { Link: ({ children }: { children: ReactNode }) => <>{children}</> },
  }),
}));
vi.mock("@/hooks/useOnboardingCta", () => ({
  ONBOARDING_CTA_VT_CLASS: "",
  ONBOARDING_CTA_CONTENT_VT_CLASS: "",
  useOnboardingCta: () => ({
    eligible: true,
    dismissed: false,
    dismiss: vi.fn(),
  }),
}));

import { OnboardingBanner } from "./onboarding-banner.tsx";

afterEach(cleanup);

describe("OnboardingBanner", () => {
  it("stands down on org home, where the welcome banner offers setup", () => {
    pathname.current = "/acme";
    render(<OnboardingBanner />);
    expect(screen.queryByText("Finish setup")).toBeNull();
  });

  it("still shows on other org-level pages", () => {
    pathname.current = "/acme/settings";
    render(<OnboardingBanner />);
    expect(screen.getByText("Finish setup")).toBeTruthy();
  });
});
