import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { OnboardingHeader } from "./onboarding-header";
import { StepContainer, StepSupportProvider } from "./step-container";

afterEach(cleanup);

describe("setup interaction fixes", () => {
  it("exposes the compact dashboard action by name", () => {
    render(<OnboardingHeader onLeave={() => {}} />);

    expect(
      screen.getByRole("button", { name: "Go to dashboard" }),
    ).toBeTruthy();
  });

  it("places the shared support action directly before the primary action", () => {
    const onSupport = vi.fn();
    render(
      <StepSupportProvider onSupport={() => void onSupport()}>
        <StepContainer
          title="Task"
          description="Description"
          onContinue={() => {}}
        >
          Content
        </StepContainer>
      </StepSupportProvider>,
    );

    const support = screen.getByRole("button", { name: "Get support" });
    const primary = screen.getByRole("button", { name: "Mark done" });
    expect(support.nextElementSibling).toBe(primary);
    fireEvent.click(support);
    expect(onSupport).toHaveBeenCalledOnce();
  });
});
