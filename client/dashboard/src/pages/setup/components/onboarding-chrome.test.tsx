import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { OnboardingHeader } from "./onboarding-header";
import { OnboardingFooter } from "./onboarding-footer";
import { SETUP_CONTAINER } from "./setup-container";
import { StepContainer, StepSupportProvider } from "./step-container";
vi.mock("@/components/ui/ThemeSwitcher", () => ({ ThemeSwitcher: () => null }));
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

  it("keeps the setup frame full-width with responsive edge padding", () => {
    expect(SETUP_CONTAINER.split(" ")).toEqual([
      "mx-auto",
      "w-full",
      "px-4",
      "sm:px-6",
      "lg:px-8",
    ]);
  });

  it("keeps the header and footer aligned with the shared setup frame", () => {
    render(
      <>
        <OnboardingHeader />
        <OnboardingFooter />
      </>,
    );
    for (const role of ["banner", "contentinfo"] as const) {
      const element = screen.getByRole(role);
      expect(element.classList.contains("shrink-0")).toBe(true);
      for (const name of SETUP_CONTAINER.split(" ")) {
        expect(element.firstElementChild?.classList.contains(name)).toBe(true);
      }
    }
  });
});
