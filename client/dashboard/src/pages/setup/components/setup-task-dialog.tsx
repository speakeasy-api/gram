import { Dialog } from "@/components/ui/Dialog";
import type { SetupTask } from "@gram/client/models/components/setuptask.js";
import { SetupTaskContent } from "./setup-task-content";

type SetupTaskDialogProps = {
  task: SetupTask | null;
  pending: boolean;
  onClose: () => void;
  onComplete: () => void;
};

export function SetupTaskDialog({
  task,
  pending,
  onClose,
  onComplete,
}: SetupTaskDialogProps): JSX.Element {
  return (
    <Dialog
      open={task !== null}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <Dialog.Content
        closeable={!pending}
        className="max-h-[90vh] w-[calc(100vw-2rem)] max-w-5xl overflow-y-auto bg-card"
      >
        <Dialog.Title className="sr-only">{task?.title}</Dialog.Title>
        <Dialog.Description className="sr-only">
          Complete the selected organization setup task.
        </Dialog.Description>
        {task ? (
          <SetupTaskContent
            taskKey={task.key}
            projectSlug="default"
            onComplete={onComplete}
            onSkip={onComplete}
            onBack={onClose}
          />
        ) : null}
      </Dialog.Content>
    </Dialog>
  );
}
