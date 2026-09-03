import { useRef } from "react";
import { Dialog } from "@/components/ui/Dialog";
import type { SetupTask } from "@gram/client/models/components/setuptask.js";
import { SetupTaskContent } from "./setup-task-content";

type SetupTaskDialogProps = {
  task: SetupTask | null;
  pending: boolean;
  onClose: () => void;
  onComplete: () => void | Promise<void>;
  onSkip: () => void;
};

export function SetupTaskDialog({
  task,
  pending,
  onClose,
  onComplete,
  onSkip,
}: SetupTaskDialogProps): JSX.Element {
  const completionInFlight = useRef(false);

  const handleComplete = async () => {
    if (pending || completionInFlight.current) return;
    completionInFlight.current = true;
    try {
      await onComplete();
    } finally {
      completionInFlight.current = false;
    }
  };

  return (
    <Dialog
      open={task !== null}
      onOpenChange={(open) => {
        if (!open && !pending) onClose();
      }}
    >
      <Dialog.Content
        closeable={!pending}
        onEscapeKeyDown={(event) => {
          if (pending) event.preventDefault();
        }}
        onInteractOutside={(event) => {
          if (pending) event.preventDefault();
        }}
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
            onComplete={() => void handleComplete()}
            onSkip={() => {
              if (!pending) onSkip();
            }}
            onBack={() => {
              if (!pending) onClose();
            }}
          />
        ) : null}
      </Dialog.Content>
    </Dialog>
  );
}
