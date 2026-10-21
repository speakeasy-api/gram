import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const testState = vi.hoisted(() => ({
  failClosed: false as boolean | undefined,
  mutate: vi.fn(),
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-active" }),
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@/lib/errors", () => ({ handleAPIError: vi.fn() }));

vi.mock("@gram/client/react-query/featuresSet.js", () => ({
  useFeaturesSetMutation: () => ({
    isPending: false,
    mutate: testState.mutate,
  }),
}));

vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  invalidateAllProductFeatures: vi.fn(),
  useProductFeatures: () => ({
    data:
      testState.failClosed === undefined
        ? undefined
        : { riskEnforcementFailClosed: testState.failClosed },
    error: null,
    isFetching: false,
    refetch: vi.fn(),
  }),
}));

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));

import {
  JudgeFailureModeCard,
  useJudgeFailureMode,
} from "./judge-failure-mode";

function Harness(): JSX.Element {
  return <JudgeFailureModeCard mode={useJudgeFailureMode()} />;
}

const toggleName = "Block the message when the judge can't answer";

beforeEach(() => {
  testState.failClosed = false;
  testState.mutate.mockReset();
});

afterEach(cleanup);

describe("JudgeFailureModeCard", () => {
  it("shows the organization's failure mode and labels it organization-wide", () => {
    render(<Harness />);

    expect(screen.getByText("Allow the message")).toBeTruthy();
    expect(screen.getByText(/applies to every prompt policy/)).toBeTruthy();
    const toggle = screen.getByRole("switch", { name: toggleName });
    expect(toggle.getAttribute("aria-checked")).toBe("false");
  });

  it("asks before switching every prompt policy to block", () => {
    render(<Harness />);

    fireEvent.click(screen.getByRole("switch", { name: toggleName }));
    expect(testState.mutate).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Block" }));
    expect(testState.mutate).toHaveBeenCalledWith(
      {
        request: {
          setProductFeatureRequestBody: {
            organizationId: "org-active",
            featureName: "risk_enforcement_fail_closed",
            enabled: true,
          },
        },
      },
      expect.anything(),
    );
  });

  it("switches back to allowing without a confirmation", () => {
    testState.failClosed = true;
    render(<Harness />);

    expect(screen.getByText("Block the message")).toBeTruthy();
    fireEvent.click(screen.getByRole("switch", { name: toggleName }));
    expect(testState.mutate).toHaveBeenCalledWith(
      {
        request: {
          setProductFeatureRequestBody: {
            organizationId: "org-active",
            featureName: "risk_enforcement_fail_closed",
            enabled: false,
          },
        },
      },
      undefined,
    );
  });

  it("keeps the switch disabled until the setting loads", () => {
    testState.failClosed = undefined;
    render(<Harness />);

    const toggle = screen.getByRole("switch", { name: toggleName });
    expect(toggle.hasAttribute("disabled")).toBe(true);
  });
});
