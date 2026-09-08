import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { JourneyStepsProvider } from "./journey-steps-provider";
import { StepContainer } from "./step-container";
import { StepSection } from "./step-section";

afterEach(cleanup);

function Card({ onContinue }: { onContinue: () => void }): JSX.Element {
  return (
    <StepContainer
      icon={null}
      title="Card"
      description="A card with two steps"
      onContinue={onContinue}
    >
      <StepSection index={1} title="First">
        <span>first body</span>
      </StepSection>
      <StepSection index={2} title="Second">
        <span>second body</span>
      </StepSection>
    </StepContainer>
  );
}

describe("StepContainer", () => {
  it("walks sub-steps with Next step, then offers Mark done, with no Back or Skip", () => {
    const onContinue = vi.fn();
    render(
      <JourneyStepsProvider>
        <Card onContinue={() => void onContinue()} />
      </JourneyStepsProvider>,
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
});
