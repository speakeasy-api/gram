import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render } from "@testing-library/react";

import { SetupWizard } from "./onboarding-wizard";

vi.mock("react-router", () => ({
  useNavigate: () => vi.fn(),
  useParams: () => ({ orgSlug: "acme" }),
  useSearchParams: () => [new URLSearchParams(), vi.fn()],
}));
vi.mock("@gram/client/react-query/onboardingStatus", () => ({
  useOnboardingStatus: () => ({
    data: { ssoConfigured: false, dsyncConfigured: false },
    isLoading: false,
  }),
}));
vi.mock("@gram/client/react-query/publishStatus", () => ({
  usePublishStatus: () => ({ data: { connected: false }, isLoading: false }),
}));

vi.mock("@/components/ui/Skeleton", () => ({
  Skeleton: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));
vi.mock("./onboarding-header", () => ({
  OnboardingHeader: () => null,
}));
vi.mock("./onboarding-footer", () => ({
  OnboardingFooter: () => null,
}));
vi.mock("./onboarding-stepper", () => ({
  OnboardingStepper: () => null,
}));
vi.mock("./steps", () => ({
  ConnectIdpStep: () => null,
  DirectorySyncStep: () => null,
  CreateMarketplaceStep: () => null,
  DistributeServersStep: () => null,
  InstrumentAgentsStep: () => null,
  AdditionalAgentConfigStep: () => null,
  ConfirmTrafficStep: () => null,
  ConfigurePoliciesStep: () => null,
  PlatformMCPSetupStep: () => null,
}));

afterEach(() => {
  cleanup();
  localStorage.clear();
});

describe("SetupWizard", () => {
  it("records that the setup view was opened for the org", () => {
    render(<SetupWizard />);

    expect(localStorage.getItem("gram-org-welcome-rollout-started:acme")).toBe(
      "true",
    );
  });
});
