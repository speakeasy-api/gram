import { StepSupportProvider } from "../step-container";
import { JourneyStepsProvider } from "../journey-steps-provider";
import { useJourneyView } from "../journey-steps";
import { Button } from "@/components/ui/Button";
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

function JourneyNavigation(): JSX.Element | null {
  const { steps, activeIndex, setActiveIndex } = useJourneyView();
  if (steps.length < 2) return null;
  return (
    <nav aria-label="Task steps" className="mb-4 flex flex-wrap gap-2">
      {steps.map((step) => (
        <Button
          key={step.index}
          size="sm"
          variant={step.index === activeIndex ? "secondary" : "tertiary"}
          aria-current={step.index === activeIndex ? "step" : undefined}
          onClick={() => setActiveIndex(step.index)}
        >
          <Button.Text>
            {step.index}. {step.title}
          </Button.Text>
        </Button>
      ))}
    </nav>
  );
}

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
  onSetStatus,
  onAssign,
}: TaskDialogProps): JSX.Element {
  return (
    <Dialog
      open={task !== null}
      onOpenChange={(open) => {
        if (!open && !isPending) onClose();
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

          <div className="min-h-0 overflow-y-auto px-4 py-6 sm:px-8">
            {error && <p role="alert">{error}</p>}
            {task.blockedBy.length > 0 && (
              <p>Blocked by: {task.blockedBy.join(", ")}</p>
            )}
            <StepSupportProvider
              onSupport={() => {
                if (task.verified) return showPylonChat();
                void onSetStatus(task.id, "awaiting_support").then((saved) => {
                  if (saved) showPylonChat();
                });
              }}
            >
              <JourneyStepsProvider key={task.id}>
                <fieldset disabled={isPending} className="min-w-0">
                  <JourneyNavigation />
                  <TaskStep
                    taskId={task.id}
                    projectSlug={projectSlug}
                    onComplete={() => {
                      if (task.verified) return onClose();
                      void onSetStatus(task.id, "done").then((saved) => {
                        if (saved) onClose();
                      });
                    }}
                    onClose={() => {
                      if (!isPending) onClose();
                    }}
                  />
                </fieldset>
              </JourneyStepsProvider>
            </StepSupportProvider>
          </div>
        </Dialog.Content>
      )}
    </Dialog>
  );
}
