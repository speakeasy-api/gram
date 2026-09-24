import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { SetupTaskContent } from "./setup-task-content";
import { StepContainer } from "./step-container";
import type { TaskStepProps } from "./board/task-step";

vi.mock("./board/task-step", () => ({
  TaskStep: ({ taskId, onComplete, onClose }: TaskStepProps) => (
    <StepContainer
      title={taskId}
      description="Setup task"
      onContinue={onComplete}
      markDoneLabel="Complete"
    >
      <button onClick={onClose}>Skip</button>
    </StepContainer>
  ),
}));

afterEach(cleanup);

it.each([
  "enable-logging",
  "connect-idp",
  "directory-sync",
  "create-marketplace",
  "confirm-traffic",
  "identity-provider",
  "anthropic-observability",
])("shares the board renderer and callbacks for %s", (taskKey) => {
  const onComplete = vi.fn<() => void>();
  const onClose = vi.fn<() => void>();
  const onSupport = vi.fn<() => void>();
  render(
    <SetupTaskContent
      taskKey={taskKey}
      projectSlug="default"
      onComplete={onComplete}
      onClose={onClose}
      onSupport={onSupport}
    />,
  );

  expect(screen.getByText(taskKey)).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Complete" }));
  fireEvent.click(screen.getByRole("button", { name: "Skip" }));
  fireEvent.click(screen.getByRole("button", { name: "Get support" }));
  expect(onComplete).toHaveBeenCalledOnce();
  expect(onClose).toHaveBeenCalledOnce();
  expect(onSupport).toHaveBeenCalledOnce();
});
