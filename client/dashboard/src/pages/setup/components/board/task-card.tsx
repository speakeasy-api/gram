import {
  BadgeCheck,
  Check,
  Circle,
  CircleDashed,
  LifeBuoy,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Badge } from "@/components/ui/Badge";
import { type Action, MoreActions } from "@/components/ui/MoreActions";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/Tooltip";
import {
  Popover,
  PopoverAnchor,
  PopoverContent,
} from "@/components/ui/Popover";
import { cn } from "@/lib/utils";
import type { OnboardingTask } from "../../onboarding-model";
import {
  TASK_STATUS_META,
  TASK_STATUSES,
  type TaskStatus,
} from "../../onboarding-tasks";

interface TaskCardProps {
  task: OnboardingTask;
  /** Resolves a prerequisite key to its title. */
  dependencyTitle: (key: string) => string;
  canOpen?: boolean;
  canHide: boolean;
  canSetStatus: boolean;
  isPending: boolean;
  onOpen: () => void;
  onGoToTask: (id: string) => void;
  reachableTaskIds: string[];
  onSetStatus: (status: TaskStatus) => void;
  onToggleHidden: () => void;
}

function buildMenuActions({
  task,
  canHide,
  canOpen = true,
  onOpen,
  onSetStatus,
  onToggleHidden,
  canSetStatus,
  isPending,
}: Pick<
  TaskCardProps,
  | "task"
  | "canOpen"
  | "canHide"
  | "onOpen"
  | "onSetStatus"
  | "onToggleHidden"
  | "canSetStatus"
  | "isPending"
>): Action[] {
  const actions: Action[] = [
    {
      icon: "maximize-2",
      label: "Open task",
      onClick: onOpen,
      disabled: !canOpen,
      description: !canOpen
        ? "An organization admin is required to open setup tasks."
        : undefined,
    },
  ];
  if (canSetStatus && !task.verified) {
    for (const status of TASK_STATUSES) {
      if (status === task.status) continue;
      actions.push({
        label: `Move to ${TASK_STATUS_META[status].label}`,
        onClick: () => onSetStatus(status),
        separatorBefore: actions.length === 1,
        disabled: isPending || (status !== "todo" && task.blockedBy.length > 0),
      });
    }
  }
  if (canHide) {
    actions.push({
      icon: task.hidden ? "eye" : "eye-off",
      label: task.hidden ? "Show on board" : "Hide from board",
      onClick: onToggleHidden,
      separatorBefore: true,
      disabled: isPending,
    });
  }
  return actions;
}

export function TaskCard({
  task,
  canHide,
  canOpen = true,
  canSetStatus,
  isPending,
  onOpen,
  onSetStatus,
  onToggleHidden,
  onGoToTask,
  reachableTaskIds,
  dependencyTitle,
}: TaskCardProps): JSX.Element {
  const blocked = task.blockedBy.length > 0;
  const showsPopover = blocked || !canOpen;
  const StatusIcon = task.verified
    ? BadgeCheck
    : {
        todo: Circle,
        in_progress: CircleDashed,
        awaiting_support: LifeBuoy,
        done: Check,
      }[task.status];
  const effectiveStatus = task.verified ? "done" : task.status;
  const statusMeta = TASK_STATUS_META[effectiveStatus];
  const statusColor = {
    todo: "text-muted-foreground",
    in_progress: "text-default-information",
    awaiting_support: "text-default-warning",
    done: "text-default-success",
  }[effectiveStatus];
  const [prerequisitesOpen, setPrerequisitesOpen] = useState(false);
  const closeTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const contentRef = useRef<HTMLDivElement>(null);
  const activationRef = useRef<HTMLButtonElement>(null);
  const hoverOpened = useRef(false);
  const cancelClose = () => {
    if (closeTimer.current) clearTimeout(closeTimer.current);
  };
  const scheduleClose = () => {
    cancelClose();
    closeTimer.current = setTimeout(() => {
      if (!contentRef.current?.contains(document.activeElement)) {
        setPrerequisitesOpen(false);
      }
    }, 200);
  };
  useEffect(
    () => () => {
      if (closeTimer.current) clearTimeout(closeTimer.current);
    },
    [],
  );
  const descriptionRef = useRef<HTMLParagraphElement>(null);
  const [descriptionTruncated, setDescriptionTruncated] = useState(false);
  const [tooltipOpen, setTooltipOpen] = useState(false);
  const hasTooltip = !blocked && descriptionTruncated;
  useEffect(() => {
    const description = descriptionRef.current;
    if (!description) return;
    const measure = () => {
      const truncated =
        description.scrollHeight > description.clientHeight ||
        description.scrollWidth > description.clientWidth;
      setDescriptionTruncated(truncated);
      if (!blocked && !truncated) setTooltipOpen(false);
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
  }, [task.description, blocked]);
  const activation = (
    <button
      type="button"
      aria-label={`${task.title}, ${TASK_STATUS_META[task.verified ? "done" : task.status].label}`}
      ref={activationRef}
      aria-haspopup={showsPopover ? "dialog" : undefined}
      aria-expanded={showsPopover ? prerequisitesOpen : undefined}
      onPointerEnter={() => {
        if (!showsPopover) return;
        cancelClose();
        hoverOpened.current = true;
        setPrerequisitesOpen(true);
      }}
      onPointerLeave={() => {
        if (showsPopover) scheduleClose();
      }}
      onClick={() => {
        if (!showsPopover) {
          onOpen();
        } else {
          hoverOpened.current = false;
          cancelClose();
          setPrerequisitesOpen(true);
          contentRef.current
            ?.querySelector<HTMLButtonElement>("button")
            ?.focus();
        }
      }}
      className={cn(
        "absolute inset-0 focus-visible:outline-none",
        "cursor-pointer",
      )}
    />
  );
  return (
    <article
      data-onboarding-task={task.id}
      className={cn(
        "group bg-card border-border focus-within:ring-ring focus-within:ring-1 relative flex min-w-0 flex-col gap-1 border p-3 text-left text-sm leading-5 transition-colors",
        !blocked && "hover:border-foreground/40",
        task.hidden && "opacity-60",
      )}
    >
      {blocked && (
        <div
          aria-hidden="true"
          className="pointer-events-none absolute inset-0 text-muted-foreground opacity-[0.06]"
          style={{
            backgroundImage:
              "repeating-linear-gradient(45deg, currentColor 0 1px, transparent 1px 7px), repeating-linear-gradient(-45deg, currentColor 0 1px, transparent 1px 7px)",
          }}
        />
      )}
      {showsPopover ? (
        <Popover open={prerequisitesOpen} onOpenChange={setPrerequisitesOpen}>
          <PopoverAnchor asChild>{activation}</PopoverAnchor>
          <PopoverContent
            ref={contentRef}
            aria-label={blocked ? "Required tasks" : "Admin required"}
            className="w-auto max-w-72 bg-card px-3 py-1.5 text-xs leading-relaxed text-foreground"
            onPointerEnter={cancelClose}
            onPointerLeave={scheduleClose}
            onOpenAutoFocus={(event) => {
              if (hoverOpened.current) event.preventDefault();
            }}
            onCloseAutoFocus={(event) => event.preventDefault()}
            onEscapeKeyDown={() => activationRef.current?.focus()}
          >
            {!canOpen && (
              <p>
                An organization admin is required to open setup tasks. You can
                still update the status of tasks assigned to you.
              </p>
            )}
            <ul aria-label="Prerequisite tasks" className="space-y-1">
              {task.blockedBy.map((dependency) => (
                <li key={dependency}>
                  {reachableTaskIds.includes(dependency) ? (
                    <button
                      type="button"
                      aria-label={`Go to task: ${dependencyTitle(dependency)}`}
                      className="hover:text-foreground/70 focus-visible:ring-ring flex w-full items-center justify-between gap-2 text-left focus-visible:ring-2 focus-visible:outline-none"
                      onClick={() => {
                        cancelClose();
                        setPrerequisitesOpen(false);
                        onGoToTask(dependency);
                      }}
                    >
                      <span>{dependencyTitle(dependency)}</span>
                      <span aria-hidden="true" className="shrink-0">
                        →
                      </span>
                    </button>
                  ) : (
                    <div>
                      <p>{dependencyTitle(dependency)}</p>
                      <p className="text-muted-foreground mt-1">
                        Not available on this board. Ask an organization admin
                        for help.
                      </p>
                    </div>
                  )}
                </li>
              ))}
            </ul>
          </PopoverContent>
        </Popover>
      ) : (
        <TooltipProvider>
          <Tooltip
            disableHoverableContent
            open={hasTooltip && tooltipOpen}
            onOpenChange={(open) => setTooltipOpen(hasTooltip && open)}
          >
            <TooltipTrigger asChild>{activation}</TooltipTrigger>
            {hasTooltip && (
              <TooltipContent className="pointer-events-none max-w-72 bg-card text-left text-foreground">
                {task.description}
              </TooltipContent>
            )}
          </Tooltip>
        </TooltipProvider>
      )}
      <div className="pointer-events-none flex items-start justify-between gap-2">
        <div className="flex min-w-0 items-start gap-2">
          <h3
            title={task.title}
            className="text-foreground min-w-0 line-clamp-2 break-words text-sm leading-5 font-medium"
          >
            {task.title}
          </h3>
          {task.badge && <Badge size="sm">{task.badge}</Badge>}
        </div>
        <div className="flex shrink-0 items-center gap-1">
          <span
            role="img"
            title={`${statusMeta.label}${task.verified ? " · Verified" : ""}`}
            aria-label={`Status: ${statusMeta.label}${task.verified ? ", verified" : ""}`}
            className={cn(
              "pointer-events-auto relative inline-flex items-center",
              statusColor,
            )}
          >
            <StatusIcon aria-hidden="true" className="size-3" />
          </span>
          {task.hidden && (
            <Badge variant="warning" size="sm">
              Hidden
            </Badge>
          )}
          <div className="pointer-events-auto relative -my-0.5">
            <MoreActions
              size="compact"
              align="end"
              actions={buildMenuActions({
                task,
                canHide,
                canOpen,
                onOpen,
                onSetStatus,
                onToggleHidden,
                canSetStatus,
                isPending,
              })}
            />
          </div>
        </div>
      </div>

      <p
        ref={descriptionRef}
        className={cn(
          "pointer-events-none line-clamp-1 break-words text-xs leading-4",
          blocked ? "text-default-warning" : "text-muted-foreground",
        )}
      >
        {blocked
          ? `Requires: ${task.blockedBy.map(dependencyTitle).join(", ")}`
          : task.description}
      </p>
    </article>
  );
}
