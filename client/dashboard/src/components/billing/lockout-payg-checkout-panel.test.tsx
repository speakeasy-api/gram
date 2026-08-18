import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { emptySession } from "@/contexts/Auth";
import type { FeatureFlagResult } from "@/hooks/useFeatureFlag";
import type { Scope } from "@gram/client/models/components/rolegrant.js";

const mocks = vi.hoisted(() => ({
  flagResult: vi.fn(),
  hasScope: vi.fn(),
  sessionData: vi.fn(),
  mutate: vi.fn(),
  capture: vi.fn(),
}));

vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => mocks.flagResult() as FeatureFlagResult,
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: (scope: Scope) => mocks.hasScope(scope) }),
}));

// Only the session query is replaced: the gate has no session context of its
// own, and putting one back is exactly what the panel is being tested on.
vi.mock("@/contexts/Auth", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/contexts/Auth")>()),
  useSessionData: () => ({ session: mocks.sessionData() }),
}));

vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ capture: mocks.capture }),
}));

vi.mock("@gram/client/react-query/createStripeCheckout.js", () => ({
  useCreateStripeCheckoutMutation: () => ({
    mutate: mocks.mutate,
    isPending: false,
  }),
}));

import { LockoutPaygCheckoutPanel } from "./lockout-payg-checkout-panel";

const DAY = 24 * 60 * 60 * 1000;

const gatedOrg = (trial: { startedAt: Date; endsAt: Date } | null) => ({
  ...emptySession,
  trial,
  whitelisted: false,
  activeOrganizationId: "org-under-test",
});

const expiredTrial = {
  startedAt: new Date(Date.now() - 20 * DAY),
  endsAt: new Date(Date.now() - 6 * DAY),
};

const cta = () =>
  screen.queryByRole("button", { name: /start pay as you go/i });

describe("LockoutPaygCheckoutPanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.flagResult.mockReturnValue({ status: "enabled" });
    mocks.hasScope.mockReturnValue(true);
    mocks.sessionData.mockReturnValue(gatedOrg(null));
  });

  afterEach(cleanup);

  // Both lockout variants reach the same gate: the organization that never
  // trialed and the one whose enterprise trial ran out.
  it.each([
    ["never trialed", null],
    ["an expired trial", expiredTrial],
  ])("offers checkout to a gated admin with %s", (_name, trial) => {
    mocks.sessionData.mockReturnValue(gatedOrg(trial));

    render(<LockoutPaygCheckoutPanel />);

    expect(cta()).not.toBeNull();
    expect(mocks.hasScope).toHaveBeenCalledWith("org:admin");
  });

  it("renders nothing for a member without org:admin", () => {
    mocks.hasScope.mockReturnValue(false);

    const { container } = render(<LockoutPaygCheckoutPanel />);

    expect(container.firstChild).toBeNull();
  });

  // An organization that is not walled off only reaches this flow by choosing
  // to book a call, so the gate keeps its booking-only shape.
  it("renders nothing for a whitelisted organization", () => {
    mocks.sessionData.mockReturnValue({
      ...gatedOrg(null),
      whitelisted: true,
    });

    const { container } = render(<LockoutPaygCheckoutPanel />);

    expect(container.firstChild).toBeNull();
  });

  // An unresolved session would otherwise read as "not whitelisted".
  it("renders nothing until the session resolves", () => {
    mocks.sessionData.mockReturnValue(null);

    const { container } = render(<LockoutPaygCheckoutPanel />);

    expect(container.firstChild).toBeNull();
  });

  // Fail closed: an unresolved rollout flag can never put a payment flow in
  // front of a locked-out organization.
  it.each([
    ["disabled", { status: "disabled" }],
    ["loading", { status: "loading" }],
    ["missing", { status: "missing" }],
    ["error", { status: "error" }],
  ])("renders nothing when the flag is %s", (_name, result) => {
    mocks.flagResult.mockReturnValue(result);

    const { container } = render(<LockoutPaygCheckoutPanel />);

    expect(container.firstChild).toBeNull();
  });

  it("only calls the checkout endpoint on click", () => {
    render(<LockoutPaygCheckoutPanel />);

    expect(mocks.mutate).not.toHaveBeenCalled();
  });
});
