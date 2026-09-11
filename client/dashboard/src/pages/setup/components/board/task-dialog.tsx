import { StepSupportProvider } from "../step-container";
import { showPylonChat } from "@/lib/pylon";
import { Badge } from "@/components/ui/Badge";
import { Dialog } from "@/components/ui/Dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { cn } from "@/lib/utils";
import { AssigneePicker } from "./assignee-picker";
import type { Assignee, BoardTask } from "./board-store";
import { TaskStep } from "./task-step";
import {
  type OnboardingTaskId,
  TASK_STATUS_META,
  TASK_STATUSES,
  type TaskStatus,
} from "./tasks";

function StatusSelect({
  value,
  disabled,
  blocked,
  onChange,
}: {
  value: TaskStatus;
  disabled: boolean;
  blocked: boolean;
  onChange: (status: TaskStatus) => void;
}): JSX.Element {
  return (
    <Select
      value={value}
      onValueChange={(next) => onChange(next as TaskStatus)}
      disabled={disabled}
    >
      <SelectTrigger size="sm" aria-label="Status">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {TASK_STATUSES.map((status) => (
          <SelectItem
            key={status}
            value={status}
            disabled={blocked && status !== "todo"}
          >
            <span
              className={cn(
                "size-2 rounded-full",
                TASK_STATUS_META[status].dotClassName,
              )}
              aria-hidden="true"
            />
            {TASK_STATUS_META[status].label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

interface TaskDialogProps {
  task: BoardTask | null;
  projectSlug?: string;
  canAssign: boolean;
  canSetStatus: boolean;
  isPending: boolean;
  error: string | null;
  onClose: () => void;
  onOpenTask: (id: OnboardingTaskId) => void;
  onSetStatus: (id: OnboardingTaskId, status: TaskStatus) => Promise<boolean>;
  onAssign: (id: OnboardingTaskId, assignee: Assignee | undefined) => void;
}

/**
 * The task's detail view: its board metadata across the top and the original
 * setup step underneath, so completing the step is still done in place.
 */
export function TaskDialog({
  task,
  projectSlug,
  canAssign,
  canSetStatus,
  isPending,
  error,
  onClose,
  onOpenTask,
  onSetStatus,
  onAssign,
}: TaskDialogProps): JSX.Element {
  return (
    <Dialog
      open={task !== null}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      {task && (
        <Dialog.Content className="flex max-h-[90vh] w-[calc(100vw-2rem)] max-w-4xl flex-col gap-0 overflow-hidden p-0">
          <Dialog.Title className="sr-only">{task.title}</Dialog.Title>
          <Dialog.Description className="sr-only">
            {task.description}
          </Dialog.Description>

          <div className="border-border flex flex-wrap items-center gap-x-3 gap-y-2 border-b px-6 py-3 pr-14">
            <StatusSelect
              value={task.status}
              disabled={task.verified || !canSetStatus || isPending}
              blocked={task.blockedBy.length > 0}
              onChange={(status) => void onSetStatus(task.id, status)}
            />
            {task.verified && (
              <Badge variant="success" size="sm">
                Verified
              </Badge>
            )}
            <AssigneePicker
              assignee={task.assignee}
              onChange={(assignee) => onAssign(task.id, assignee)}
              size="sm"
              disabled={!canAssign || isPending}
            />
          </div>

          <div className="overflow-y-auto px-8 py-6">
            {error && <p role="alert">{error}</p>}
            {task.blockedBy.length > 0 && (
              <p>Blocked by: {task.blockedBy.join(", ")}</p>
            )}
            <StepSupportProvider
              onSupport={() => {
                void onSetStatus(task.id, "awaiting_support").then((saved) => {
                  if (saved) showPylonChat();
                });
              }}
            >
              <TaskStep
                key={task.id}
                taskId={task.id}
                projectSlug={projectSlug}
                onComplete={() => {
                  if (task.verified) return;
                  void onSetStatus(task.id, "done").then((saved) => {
                    if (saved) onClose();
                  });
                }}
                onClose={onClose}
                onOpenTask={onOpenTask}
              />
            </StepSupportProvider>
          </div>
        </Dialog.Content>
      )}
    </Dialog>
  );
}
