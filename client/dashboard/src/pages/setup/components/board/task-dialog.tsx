import { useOrgRoutes } from "@/routes";
import { SETUP_TASK_SLUGS } from "../../task-slugs";
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
import { RemindButton } from "./remind-button";
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
  onChange,
}: {
  value: TaskStatus;
  disabled: boolean;
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
          <SelectItem key={status} value={status}>
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
  isReminding: boolean;
  onClose: () => void;
  onOpenTask: (id: OnboardingTaskId) => void;
  onSetStatus: (id: OnboardingTaskId, status: TaskStatus) => void;
  onAssign: (id: OnboardingTaskId, assignee: Assignee | undefined) => void;
  onRemind: (id: OnboardingTaskId) => void;
}

/**
 * The task's detail view: its board metadata across the top and the original
 * setup step underneath, so completing the step is still done in place.
 */
export function TaskDialog({
  task,
  projectSlug,
  isReminding,
  onClose,
  onOpenTask,
  onSetStatus,
  onAssign,
  onRemind,
}: TaskDialogProps): JSX.Element {
  const orgRoutes = useOrgRoutes();
  // Only mapped tasks have content in the standalone guided flow.
  const guidedSlug = task ? SETUP_TASK_SLUGS[task.id] : undefined;

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
              disabled={task.verified}
              onChange={(status) => onSetStatus(task.id, status)}
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
            />
            <RemindButton
              task={task}
              isReminding={isReminding}
              onRemind={() => onRemind(task.id)}
              size="sm"
            />
            {guidedSlug && (
              <orgRoutes.setupTask.Link
                params={[guidedSlug]}
                queryParams={projectSlug ? { projectSlug } : undefined}
                className="text-primary text-sm font-medium"
              >
                Open guided setup
              </orgRoutes.setupTask.Link>
            )}
          </div>

          <div className="overflow-y-auto px-8 py-6">
            <StepSupportProvider
              onSupport={() => {
                onSetStatus(task.id, "awaiting_support");
                showPylonChat();
              }}
            >
              <TaskStep
                key={task.id}
                taskId={task.id}
                projectSlug={projectSlug}
                onComplete={() => {
                  onSetStatus(task.id, "done");
                  onClose();
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
