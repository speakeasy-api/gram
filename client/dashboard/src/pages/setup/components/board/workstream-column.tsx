import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/Tooltip";
import { assigneeIdentity, type Assignee, type BoardTask } from "./board-store";
import type { OnboardingWorkstreamDefinition } from "./tasks";
import { AssigneePicker } from "./assignee-picker";
import { countTasksBelow } from "./count-tasks-below";

interface WorkstreamColumnProps {
  workstream: OnboardingWorkstreamDefinition;
  tasks: BoardTask[];
  children: ReactNode;
  allTasks: BoardTask[];
  canAssign: boolean;
  isPending: boolean;
  onAssign: (owner: Assignee | undefined) => void;
}

export function WorkstreamColumn({
  workstream,
  tasks,
  children,
  allTasks,
  canAssign,
  isPending,
  onAssign,
}: WorkstreamColumnProps): JSX.Element {
  // Workstream assignment writes the same owner to every task. Do not promote
  // partial or mixed legacy card assignments to a workstream owner.
  const candidate = allTasks[0]?.assignee;
  const assignee =
    candidate &&
    allTasks.every(
      ({ assignee: owner }) =>
        owner?.kind === candidate.kind &&
        assigneeIdentity(owner) === assigneeIdentity(candidate),
    )
      ? candidate
      : undefined;
  // Progress describes the workstream, not the current assignment filter.
  const requiredTasks = allTasks.filter((task) => !task.hidden && !task.badge);
  const completedTasks = requiredTasks.filter(
    (task) => task.status === "done",
  ).length;
  const progress =
    requiredTasks.length > 0
      ? (completedTasks / requiredTasks.length) * 100
      : 0;
  const headingId = `onboarding-workstream-${workstream.id}`;
  const descriptionRef = useRef<HTMLParagraphElement>(null);
  const [descriptionTruncated, setDescriptionTruncated] = useState(false);
  const [descriptionOpen, setDescriptionOpen] = useState(false);
  useEffect(() => {
    const description = descriptionRef.current;
    if (!description) return;

    const measure = () => {
      // Line clamping can overflow vertically; unbroken text can overflow horizontally.
      const truncated =
        description.scrollHeight > description.clientHeight ||
        description.scrollWidth > description.clientWidth;
      setDescriptionTruncated(truncated);
      if (!truncated) setDescriptionOpen(false);
    };
    measure();
    const observer =
      typeof ResizeObserver === "undefined"
        ? null
        : new ResizeObserver(measure);
    observer?.observe(description);
    window.addEventListener("resize", measure);
    return () => {
      observer?.disconnect();
      window.removeEventListener("resize", measure);
    };
  }, [workstream.description]);

  const taskListRef = useRef<HTMLDivElement>(null);
  const [tasksBelow, setTasksBelow] = useState(0);
  const updateTasksBelow = useCallback(() => {
    if (taskListRef.current) {
      setTasksBelow(countTasksBelow(taskListRef.current));
    }
  }, []);

  const taskIds = tasks.map((task) => task.id).join(",");
  useEffect(() => {
    const taskList = taskListRef.current;
    if (!taskList) return;

    updateTasksBelow();
    if (typeof ResizeObserver === "undefined") return;

    const observer = new ResizeObserver(updateTasksBelow);
    observer.observe(taskList);
    for (const card of taskList.children) observer.observe(card);
    return () => observer.disconnect();
  }, [taskIds, updateTasksBelow]);

  return (
    <section
      aria-labelledby={headingId}
      className="bg-card flex min-h-64 max-h-full flex-col overflow-hidden border md:min-h-0"
    >
      <header className="bg-surface-secondary-default min-h-16 shrink-0 space-y-2 border-b px-3 py-2">
        <div className="min-w-0">
          <h2 id={headingId} className="text-foreground font-medium">
            {workstream.title}
          </h2>
          <TooltipProvider>
            <Tooltip
              open={descriptionTruncated && descriptionOpen}
              onOpenChange={(open) =>
                setDescriptionOpen(descriptionTruncated && open)
              }
            >
              <TooltipTrigger asChild>
                <p
                  ref={descriptionRef}
                  tabIndex={descriptionTruncated ? 0 : undefined}
                  className="text-muted-foreground mt-0.5 line-clamp-1 text-sm focus-visible:outline focus-visible:outline-2 focus-visible:outline-ring"
                >
                  {workstream.description}
                </p>
              </TooltipTrigger>
              {descriptionTruncated && (
                <TooltipContent>{workstream.description}</TooltipContent>
              )}
            </Tooltip>
          </TooltipProvider>
          <AssigneePicker
            assignee={assignee}
            placeholder={workstream.suggestedOwner}
            disabled={!canAssign || isPending}
            onChange={onAssign}
          />
        </div>
        <p className="text-muted-foreground text-xs font-medium tabular-nums">
          {requiredTasks.length === 0
            ? "No required tasks"
            : `${completedTasks} / ${requiredTasks.length} required tasks complete`}
        </p>
        {requiredTasks.length > 0 ? (
          <div
            role="progressbar"
            aria-label={`${workstream.title} progress`}
            aria-valuemin={0}
            aria-valuemax={requiredTasks.length}
            aria-valuenow={completedTasks}
            aria-valuetext={`${completedTasks} of ${requiredTasks.length} required tasks complete`}
            className="bg-muted h-1 overflow-hidden"
          >
            <div
              className="bg-success-default h-full"
              style={{ width: `${progress}%` }}
            />
          </div>
        ) : null}
      </header>
      <div
        ref={taskListRef}
        className="grid min-h-0 flex-1 content-start gap-3 overflow-y-auto overscroll-contain p-3"
        onScroll={updateTasksBelow}
      >
        {children}
      </div>
      {tasksBelow > 0 ? (
        <div className="border-border bg-surface-secondary-default border-t px-3 py-1.5 text-center text-xs font-medium text-muted-foreground">
          ↓ {tasksBelow} more {tasksBelow === 1 ? "task" : "tasks"} below
        </div>
      ) : null}
    </section>
  );
}
