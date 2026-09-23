import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { JourneyStepsProvider } from "./journey-steps-provider";
import { StepContainer, StepSupportProvider } from "./step-container";
import { StepSection } from "./step-section";

afterEach(cleanup);

function Card({ onContinue }: { onContinue: () => void }): JSX.Element {
  return (
    <StepContainer
      title="Card"
      description="A card with two steps"
      onContinue={onContinue}
    >
      <StepSection index={1} slug="first" title="First">
        <span>first body</span>
      </StepSection>
      <StepSection index={2} slug="second" title="Second">
        <span>second body</span>
      </StepSection>
    </StepContainer>
  );
}

describe("StepContainer", () => {
  it("preserves visible keyboard focus on every footer action", () => {
    const onSupport = vi.fn();
    render(
      <MemoryRouter>
        <JourneyStepsProvider>
          <StepSupportProvider onSupport={() => void onSupport()}>
            <Card onContinue={() => {}} />
          </StepSupportProvider>
        </JourneyStepsProvider>
      </MemoryRouter>,
    );
    function expectFocus(name: string) {
      const button = screen.getByRole("button", { name });
      button.focus();
      expect(document.activeElement).toBe(button);
      for (const utility of [
        "focus-visible:ring-2",
        "focus-visible:ring-offset-3",
        "focus-visible:ring-[var(--border-focus)]",
        "focus-visible:ring-offset-[var(--bg-surface-primary-default)]",
      ]) {
        expect(button.classList.contains(utility)).toBe(true);
      }
      expect(button.classList.contains("focus-visible:ring-0")).toBe(false);
      return button;
    }
    fireEvent.click(expectFocus("Get support"));
    expect(onSupport).toHaveBeenCalledOnce();
    fireEvent.click(expectFocus("Next step"));
    expectFocus("Back");
    expectFocus("Mark done");
  });

  it("walks sub-steps with Next step, then offers Mark done, with no Back or Skip", () => {
    const onContinue = vi.fn();
    render(
      <MemoryRouter>
        <JourneyStepsProvider>
          <Card onContinue={() => void onContinue()} />
        </JourneyStepsProvider>
      </MemoryRouter>,
    );

    expect(screen.queryByRole("button", { name: "Back" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Skip" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Mark done" })).toBeNull();
    expect(screen.getByText("first body").closest("section")?.hidden).toBe(
      false,
    );
    expect(screen.getByText("second body").closest("section")?.hidden).toBe(
      true,
    );

    fireEvent.click(screen.getByRole("button", { name: "Next step" }));

    expect(screen.getByText("first body").closest("section")?.hidden).toBe(
      true,
    );
    expect(screen.getByText("second body").closest("section")?.hidden).toBe(
      false,
    );
    expect(screen.queryByRole("button", { name: "Next step" })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Mark done" }));
    expect(onContinue).toHaveBeenCalledOnce();
  });

  it("offers a Back control once past the first sub-step", () => {
    render(
      <MemoryRouter>
        <JourneyStepsProvider>
          <Card onContinue={() => {}} />
        </JourneyStepsProvider>
      </MemoryRouter>,
    );

    // Nothing to go back to on the first step.
    expect(screen.queryByRole("button", { name: "Back" })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Next step" }));

    // Below md the rail is hidden, so the footer carries the way back.
    const back = screen.getByRole("button", { name: "Back" });
    expect(back.className).toContain("md:hidden");

    fireEvent.click(back);

    expect(screen.getByText("first body").closest("section")?.hidden).toBe(
      false,
    );
    expect(screen.queryByRole("button", { name: "Back" })).toBeNull();
  });
});
