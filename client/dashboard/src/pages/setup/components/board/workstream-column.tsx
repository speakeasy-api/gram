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
import {
  assigneeIdentity,
  assigneeLabel,
  progressOf,
  type Assignee,
  type OnboardingTask,
  type OnboardingWorkstream,
} from "../../onboarding-model";
import { AssigneePicker } from "./assignee-picker";
import { countTasksBelow } from "./count-tasks-below";

interface WorkstreamColumnProps {
  /** Carries every readable member; `tasks` is the filtered subset shown. */
  workstream: OnboardingWorkstream;
  tasks: OnboardingTask[];
  children: ReactNode;
  canAssign: boolean;
  isPending: boolean;
  onAssign: (owner: Assignee | undefined) => Promise<boolean> | boolean | void;
}

export function WorkstreamColumn({
  workstream,
  tasks,
  children,
  canAssign,
  isPending,
  onAssign,
}: WorkstreamColumnProps): JSX.Element {
  const allTasks = workstream.tasks;
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
  const assignedCount = allTasks.filter((task) => task.assignee).length;
  const ownerSummary = assignee
    ? undefined
    : assignedCount === 0
      ? undefined
      : assignedCount < allTasks.length
        ? "Partially assigned"
        : "Mixed owners";
  const ownerCounts = new Map<string, { label: string; count: number }>();
  for (const task of allTasks) {
    const key = task.assignee
      ? `${task.assignee.kind}:${assigneeIdentity(task.assignee)}`
      : "unassigned";
    const previous = ownerCounts.get(key);
    ownerCounts.set(key, {
      label: task.assignee ? assigneeLabel(task.assignee) : "Unassigned",
      count: (previous?.count ?? 0) + 1,
    });
  }
  const ownerBreakdown = ownerSummary
    ? [...ownerCounts.values()]
        .map(({ label, count }) => `${label}: ${count}`)
        .join(" · ")
    : undefined;
  // Progress describes the workstream, not the current assignment filter.
  const { done: completedTasks, total: requiredCount } = progressOf(allTasks);
  const progress =
    requiredCount > 0 ? (completedTasks / requiredCount) * 100 : 0;
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
      className="bg-card flex min-h-64 flex-col overflow-hidden border md:min-h-0 md:max-h-full"
    >
      <header className="bg-surface-secondary-default min-h-16 shrink-0 space-y-2 border-b px-4 py-2">
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
            placeholder={ownerSummary ?? workstream.suggestedOwner}
            ownerBreakdown={ownerBreakdown}
            bulkAssignment
            disabled={!canAssign || isPending}
            onChange={onAssign}
          />
        </div>
        <p className="text-muted-foreground text-xs font-medium tabular-nums">
          {requiredCount === 0
            ? "No required tasks"
            : `${completedTasks} / ${requiredCount} required tasks complete`}
        </p>
        {requiredCount > 0 ? (
          <div
            role="progressbar"
            aria-label={`${workstream.title} progress`}
            aria-valuemin={0}
            aria-valuemax={requiredCount}
            aria-valuenow={completedTasks}
            aria-valuetext={`${completedTasks} of ${requiredCount} required tasks complete`}
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
        className="grid min-h-0 flex-1 content-start gap-3 p-3 md:overflow-y-auto md:overscroll-contain"
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
