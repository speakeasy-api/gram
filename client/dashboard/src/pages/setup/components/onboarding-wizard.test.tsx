import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render } from "@testing-library/react";

import { SetupWizard } from "./onboarding-wizard";

const searchParamsState = vi.hoisted(() => ({
  current: new URLSearchParams(),
  setSearchParams: vi.fn(),
}));

const onboardingStatus = vi.hoisted(() => ({
  current: {
    data: { ssoConfigured: false, dsyncConfigured: false },
    isLoading: false,
  },
}));

const publishStatus = vi.hoisted(() => ({
  current: { data: { connected: false }, isLoading: false },
}));

vi.mock("react-router", () => ({
  useNavigate: () => vi.fn(),
  useParams: () => ({ orgSlug: "acme" }),
  useSearchParams: () => [
    searchParamsState.current,
    searchParamsState.setSearchParams,
  ],
}));
vi.mock("@gram/client/react-query/onboardingStatus", () => ({
  useOnboardingStatus: () => onboardingStatus.current,
}));
vi.mock("@gram/client/react-query/publishStatus", () => ({
  usePublishStatus: () => publishStatus.current,
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
  EnableLoggingStep: () => null,
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

beforeEach(() => {
  searchParamsState.current = new URLSearchParams();
  searchParamsState.setSearchParams.mockReset();
  onboardingStatus.current = {
    data: { ssoConfigured: false, dsyncConfigured: false },
    isLoading: false,
  };
  publishStatus.current = { data: { connected: false }, isLoading: false };
});

function resumedStep(): string | null {
  const updater = searchParamsState.setSearchParams.mock.calls[0]?.[0] as
    | ((prev: URLSearchParams) => URLSearchParams)
    | undefined;
  if (!updater) return null;
  return updater(new URLSearchParams()).get("step");
}

describe("SetupWizard", () => {
  it("records that the setup view was opened for the org", () => {
    render(<SetupWizard />);

    expect(localStorage.getItem("gram-org-welcome-rollout-started:acme")).toBe(
      "true",
    );
  });

  it("resumes at enable-logging after the marketplace is published", () => {
    publishStatus.current = { data: { connected: true }, isLoading: false };

    render(<SetupWizard />);

    expect(resumedStep()).toBe("enable-logging");
  });

  it("resumes at create-marketplace after directory sync is configured", () => {
    onboardingStatus.current = {
      data: { ssoConfigured: true, dsyncConfigured: true },
      isLoading: false,
    };

    render(<SetupWizard />);

    expect(resumedStep()).toBe("create-marketplace");
  });
});
