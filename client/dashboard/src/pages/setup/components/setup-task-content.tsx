import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { TaskStep } from "./board/task-step";
import { isOnboardingTaskId } from "../onboarding-tasks";
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
}): JSX.Element {
  // A task added on the server before this dashboard learned to render it.
  if (!isOnboardingTaskId(taskKey)) {
    return (
      <Alert variant="warning">
        <div>
          <AlertTitle>This task needs a newer dashboard</AlertTitle>
          <AlertDescription>
            This version of the dashboard cannot open the “{taskKey}” setup
            task. Reload the page to update, or contact support.
          </AlertDescription>
        </div>
      </Alert>
    );
  }

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
