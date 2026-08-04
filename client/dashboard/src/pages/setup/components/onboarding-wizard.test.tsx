import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// ---------------------------------------------------------------------------
// Mocks. Set up before importing the component under test.
// ---------------------------------------------------------------------------

const state = vi.hoisted(() => ({
  ssoEnabled: false,
  scimEnabled: false,
}));

vi.mock("@gram/client/react-query/productFeatures", () => ({
  useProductFeatures: () => ({
    data: { ssoEnabled: state.ssoEnabled, scimEnabled: state.scimEnabled },
  }),
}));

vi.mock("@gram/client/react-query/onboardingStatus", () => ({
  useOnboardingStatus: () => ({ data: undefined, isLoading: false }),
}));

vi.mock("@gram/client/react-query/publishStatus", () => ({
  usePublishStatus: () => ({ data: undefined, isLoading: false }),
}));

vi.mock("./onboarding-header", () => ({
  OnboardingHeader: () => <div data-testid="header" />,
}));

vi.mock("./onboarding-footer", () => ({
  OnboardingFooter: () => <div data-testid="footer" />,
}));

// Stub each step so renderStep resolves to an identifiable node.
vi.mock("./steps", () => {
  const stub = (name: string) => () => <div data-testid={`step-${name}`} />;
  return {
    ConnectIdpStep: stub("connect-idp"),
    DirectorySyncStep: stub("directory-sync"),
    CreateMarketplaceStep: stub("create-marketplace"),
    DistributeServersStep: stub("distribute-servers"),
    InstrumentAgentsStep: stub("instrument-agents"),
    AdditionalAgentConfigStep: stub("additional-agent-config"),
    ConfirmTrafficStep: stub("confirm-traffic"),
    ConfigurePoliciesStep: stub("configure-policies"),
  };
});

import { SetupWizard } from "./onboarding-wizard";

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <SetupWizard />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  state.ssoEnabled = false;
  state.scimEnabled = false;
});

afterEach(cleanup);

describe("SetupWizard", () => {
  it("un-entitled: hides identity steps, resumes at create-marketplace, shows upsell", async () => {
    renderAt("/setup");

    // Identity steps absent from the stepper.
    await waitFor(() =>
      expect(screen.queryByText("Connect identity provider")).toBeNull(),
    );
    expect(screen.queryByText("Directory sync")).toBeNull();

    // The 6 non-identity steps are present.
    expect(screen.getByText("Create plugin marketplace")).toBeTruthy();
    expect(screen.getByText("Distribute MCP servers")).toBeTruthy();
    expect(screen.getByText("Configure policies")).toBeTruthy();

    // Resumes onto create-marketplace.
    expect(screen.getByTestId("step-create-marketplace")).toBeTruthy();

    // Persistent upsell banner.
    expect(screen.getByText(/available on an enterprise plan/i)).toBeTruthy();
  });

  it("un-entitled: a deep link to a hidden step clamps to create-marketplace", async () => {
    renderAt("/setup?step=connect-idp");

    await waitFor(() =>
      expect(screen.getByTestId("step-create-marketplace")).toBeTruthy(),
    );
    expect(screen.queryByTestId("step-connect-idp")).toBeNull();
  });

  it("fully entitled: shows all 8 steps and no upsell banner", async () => {
    state.ssoEnabled = true;
    state.scimEnabled = true;
    renderAt("/setup");

    await waitFor(() =>
      expect(screen.getByText("Connect identity provider")).toBeTruthy(),
    );
    expect(screen.getByText("Directory sync")).toBeTruthy();
    expect(screen.getByText("Configure policies")).toBeTruthy();

    expect(screen.queryByText(/available on an enterprise plan/i)).toBeNull();
  });
});
