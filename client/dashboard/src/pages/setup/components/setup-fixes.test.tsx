import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { SetupTask } from "@gram/client/models/components/setuptask.js";
import { OnboardingHeader } from "./onboarding-header";
import { SetupTaskAssignmentDialog } from "./setup-task-assignment-dialog";
import { SetupTaskDialog } from "./setup-task-dialog";
vi.mock("./setup-task-content", () => ({
  SetupTaskContent: ({
    onComplete,
    onSkip,
    onBack,
  }: {
    onComplete: () => void;
    onSkip: () => void;
    onBack: () => void;
  }) => (
    <>
      <button onClick={onComplete}>Complete</button>
      <button onClick={onSkip}>Skip</button>
      <button onClick={onBack}>Back</button>
    </>
  ),
}));

const task: SetupTask = {
  key: "connect-idp",
  title: "Connect identity provider",
  description: "Connect SSO",
  status: "todo",
  completedByFact: false,
  blockedBy: [],
  hidden: false,
};

afterEach(cleanup);

describe("setup interaction fixes", () => {
  it("exposes the compact dashboard action by name", () => {
    render(<OnboardingHeader onLeave={() => {}} />);

    expect(
      screen.getByRole("button", { name: "Go to dashboard" }),
    ).toBeTruthy();
  });

  it("keeps skip separate from task completion", () => {
    const onComplete = vi.fn();
    const onSkip = vi.fn();
    render(
      <SetupTaskDialog
        task={task}
        pending={false}
        onClose={() => {}}
        onComplete={() => void onComplete()}
        onSkip={() => void onSkip()}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Skip" }));

    expect(onSkip).toHaveBeenCalledOnce();
    expect(onComplete).not.toHaveBeenCalled();
  });

  it("prevents duplicate completion while the full handler is in flight", () => {
    let finishCompletion = () => {};
    const onComplete = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          finishCompletion = resolve;
        }),
    );
    render(
      <SetupTaskDialog
        task={task}
        pending={false}
        onClose={() => {}}
        onComplete={onComplete}
        onSkip={() => {}}
      />,
    );

    const complete = screen.getByRole("button", { name: "Complete" });
    fireEvent.click(complete);
    fireEvent.click(complete);

    expect(onComplete).toHaveBeenCalledOnce();
    finishCompletion();
  });

  it("does not dismiss or repeat task actions while pending", () => {
    const onClose = vi.fn();
    const onComplete = vi.fn();
    const onSkip = vi.fn();
    render(
      <SetupTaskDialog
        task={task}
        pending
        onClose={() => void onClose()}
        onComplete={() => void onComplete()}
        onSkip={() => void onSkip()}
      />,
    );

    fireEvent.keyDown(document, { key: "Escape" });
    fireEvent.pointerDown(document.body);
    fireEvent.click(screen.getByRole("button", { name: "Complete" }));
    fireEvent.click(screen.getByRole("button", { name: "Skip" }));
    fireEvent.click(screen.getByRole("button", { name: "Back" }));

    expect(onClose).not.toHaveBeenCalled();
    expect(onComplete).not.toHaveBeenCalled();
    expect(onSkip).not.toHaveBeenCalled();
  });

  it("does not dismiss the assignment dialog while pending", () => {
    const onClose = vi.fn();
    render(
      <SetupTaskAssignmentDialog
        task={task}
        members={[]}
        roles={[]}
        pending
        onClose={() => void onClose()}
        onAssignMember={() => {}}
        onAssignEmail={() => {}}
        onUnassign={() => {}}
      />,
    );

    fireEvent.keyDown(document, { key: "Escape" });
    fireEvent.pointerDown(document.body);

    expect(onClose).not.toHaveBeenCalled();
  });
});
