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

const productFeatures = vi.hoisted(() => ({
  current: {
    data: {
      logsEnabled: false,
      toolIoLogsEnabled: false,
      sessionCaptureEnabled: false,
    } as
      | {
          logsEnabled: boolean;
          toolIoLogsEnabled: boolean;
          sessionCaptureEnabled: boolean;
        }
      | undefined,
    isLoading: false,
  },
  query: vi.fn(),
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
vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: (...args: unknown[]) => {
    productFeatures.query(...args);
    return productFeatures.current;
  },
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org1", slug: "acme" }),
}));

vi.mock("@/components/ui/Skeleton", () => ({
  Skeleton: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));
vi.mock("./setup-shell", () => ({
  SetupShell: ({ children }: { children: React.ReactNode }) => <>{children}</>,
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
  productFeatures.current = {
    data: {
      logsEnabled: false,
      toolIoLogsEnabled: false,
      sessionCaptureEnabled: false,
    },
    isLoading: false,
  };
  productFeatures.query.mockReset();
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

  it("resumes at instrument-agents once the logging bundle is on", () => {
    publishStatus.current = { data: { connected: true }, isLoading: false };
    productFeatures.current = {
      data: {
        logsEnabled: true,
        toolIoLogsEnabled: true,
        sessionCaptureEnabled: true,
      },
      isLoading: false,
    };

    render(<SetupWizard />);

    expect(resumedStep()).toBe("instrument-agents");
  });

  it("stays on enable-logging when only some logging features are on", () => {
    publishStatus.current = { data: { connected: true }, isLoading: false };
    productFeatures.current = {
      data: {
        logsEnabled: true,
        toolIoLogsEnabled: false,
        sessionCaptureEnabled: false,
      },
      isLoading: false,
    };

    render(<SetupWizard />);

    expect(resumedStep()).toBe("enable-logging");
  });

  it("falls back to step 0 when the product features query fails", () => {
    publishStatus.current = { data: { connected: true }, isLoading: false };
    productFeatures.current = { data: undefined, isLoading: false };

    render(<SetupWizard />);

    expect(resumedStep()).toBe("connect-idp");
  });

  it("keeps the product features query from throwing to the error boundary", () => {
    render(<SetupWizard />);

    expect(productFeatures.query).toHaveBeenCalledWith(
      { organizationId: "org1" },
      undefined,
      { throwOnError: false },
    );
  });

  it("waits for product features before choosing a resume step", () => {
    publishStatus.current = { data: { connected: true }, isLoading: false };
    productFeatures.current = {
      data: {
        logsEnabled: false,
        toolIoLogsEnabled: false,
        sessionCaptureEnabled: false,
      },
      isLoading: true,
    };

    render(<SetupWizard />);

    expect(resumedStep()).toBeNull();
  });

  it("resumes at directory-sync when only SSO is configured", () => {
    onboardingStatus.current = {
      data: { ssoConfigured: true, dsyncConfigured: false },
      isLoading: false,
    };

    render(<SetupWizard />);

    expect(resumedStep()).toBe("directory-sync");
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
