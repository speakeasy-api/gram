import { TaskStep } from "./board/task-step";
import { isOnboardingTaskId } from "./board/tasks";
import { StepSupportProvider } from "./step-container";

export function SetupTaskContent({
  taskKey,
  projectSlug,
  onComplete,
  onSupport,
  onClose,
}: {
  taskKey: string;
  projectSlug: string;
  onComplete: () => void;
  onSupport: () => void;
  onClose: () => void;
}): JSX.Element | null {
  if (!isOnboardingTaskId(taskKey)) return null;

  return (
    <StepSupportProvider onSupport={onSupport}>
      <TaskStep
        taskId={taskKey}
        projectSlug={projectSlug}
        onComplete={onComplete}
        onClose={onClose}
      />
    </StepSupportProvider>
  );
}
