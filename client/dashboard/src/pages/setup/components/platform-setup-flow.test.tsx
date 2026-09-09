import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { PlatformSetupStep } from "../types";
import { PlatformSetupFlow } from "./platform-setup-flow";

const mocks = vi.hoisted(() => ({ ensure: vi.fn() }));

vi.mock("./platform-setup-values", () => ({
  usePlatformApiKeys: () => ({
    keys: {},
    pending: {},
    errors: {},
    ensure: mocks.ensure,
  }),
}));
vi.mock("./platform-setup-steps", () => ({
  PlatformSetupStepBody: ({
    step,
    eyebrow,
    onEligibilityAnswer,
  }: {
    step: PlatformSetupStep;
    eyebrow: string;
    onEligibilityAnswer: (eligible: boolean) => void;
  }) => (
    <div>
      <p>
        {eyebrow}: {step.title}
      </p>
      {step.eligibility ? (
        <>
          <button onClick={() => onEligibilityAnswer(true)}>Yes</button>
          <button onClick={() => onEligibilityAnswer(false)}>No</button>
        </>
      ) : null}
    </div>
  ),
}));

afterEach(cleanup);
beforeEach(() => mocks.ensure.mockReset());

describe("PlatformSetupFlow", () => {
  it("holds the instructions back until its prerequisite is met", () => {
    render(
      <PlatformSetupFlow
        platformId="cursor"
        status="not_started"
        onStatusChange={() => {}}
        heldBack="Publish the marketplace first."
      />,
    );

    expect(screen.getByText("Publish the marketplace first.")).toBeTruthy();
    expect(screen.queryByText(/Step 1:/)).toBeNull();
    expect(mocks.ensure).not.toHaveBeenCalled();
  });

  it("stacks every step and mints the API key once", () => {
    render(
      <PlatformSetupFlow
        platformId="cursor"
        status="not_started"
        onStatusChange={() => {}}
      />,
    );

    expect(
      screen.getByText("Step 1: Open your Cursor team dashboard"),
    ).toBeTruthy();
    expect(
      screen.getByText("Step 2: Import the Speakeasy marketplace"),
    ).toBeTruthy();
    expect(mocks.ensure).toHaveBeenCalledOnce();
  });

  it("shows nothing past the eligibility question until it is answered", () => {
    const onStatusChange = vi.fn();
    render(
      <PlatformSetupFlow
        platformId="claude"
        status="not_started"
        onStatusChange={(next) => void onStatusChange(next)}
      />,
    );

    expect(screen.getByText("Step 1: Plan check")).toBeTruthy();
    expect(screen.queryByText(/Step 2:/)).toBeNull();
    expect(
      screen.queryByRole("button", { name: /Mark Claude Code/ }),
    ).toBeNull();
    expect(mocks.ensure).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Yes" }));

    expect(screen.getByText(/Step 2:/)).toBeTruthy();
    expect(mocks.ensure).toHaveBeenCalledOnce();
  });

  it("explains why an ineligible org is stuck instead of listing steps", () => {
    const onStatusChange = vi.fn();
    render(
      <PlatformSetupFlow
        platformId="claude"
        status="not_started"
        onStatusChange={(next) => void onStatusChange(next)}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "No" }));

    expect(onStatusChange).toHaveBeenCalledWith("blocked");
    expect(screen.getByText("Per-user setup flow coming soon")).toBeTruthy();
    expect(screen.queryByText(/Step 2:/)).toBeNull();
    expect(
      screen.queryByRole("button", { name: /Mark Claude Code/ }),
    ).toBeNull();
  });

  it("names the platform by its label when the step covers more than one", () => {
    render(
      <PlatformSetupFlow
        platformId="claude-cowork"
        label="Claude Chat and Cowork"
        status="not_started"
        onStatusChange={() => {}}
      />,
    );

    expect(
      screen.getByRole("button", {
        name: "Mark Claude Chat and Cowork as connected",
      }),
    ).toBeTruthy();
  });

  it("marks the platform connected, and lets that be taken back", () => {
    const onStatusChange = vi.fn();
    const { rerender } = render(
      <PlatformSetupFlow
        platformId="cursor"
        status="not_started"
        onStatusChange={(next) => void onStatusChange(next)}
      />,
    );

    fireEvent.click(
      screen.getByRole("button", { name: "Mark Cursor as connected" }),
    );
    expect(onStatusChange).toHaveBeenCalledWith("complete");

    rerender(
      <PlatformSetupFlow
        platformId="cursor"
        status="complete"
        onStatusChange={(next) => void onStatusChange(next)}
      />,
    );

    expect(screen.getByText("Cursor is connected.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Not yet" }));
    expect(onStatusChange).toHaveBeenLastCalledWith("not_started");
  });
});
