import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router";
import { Skeleton } from "@/components/ui/Skeleton";
import { Switch } from "@/components/ui/Switch";
import { useSession } from "@/contexts/Auth";
import { useOrgSetupStarted } from "@/hooks/useOrgSetupStarted";
import { OnboardingFooter } from "../onboarding-footer";
import { OnboardingHeader } from "../onboarding-header";
import { TaskCard } from "./task-card";
import { TaskDialog } from "./task-dialog";
import {
  isOnboardingTaskId,
  type OnboardingTaskId,
  ONBOARDING_WORKSTREAMS,
} from "./tasks";
import { useOnboardingBoard } from "./use-onboarding-board";
import { WorkstreamColumn } from "./workstream-column";

const WORKSTREAM_GRID_CLASS =
  "grid min-h-0 flex-1 grid-cols-1 gap-4 overflow-y-auto md:auto-rows-[minmax(0,1fr)] md:grid-cols-2 xl:grid-cols-4 xl:overflow-hidden";

function BoardHeader(): JSX.Element {
  return (
    <div className="max-w-2xl">
      <span className="text-eyebrow">Organization</span>
      <h1 className="text-display-sm text-foreground mt-1 font-thin">
        Onboarding
      </h1>
      <p className="text-muted-foreground mt-2 text-sm">
        Every setup task on one board. Hand each one to an owner, track where it
        stands, and send a reminder when it stalls.
      </p>
    </div>
  );
}

function BoardToolbar({
  doneCount,
  totalCount,
  showMine,
  onShowMineChange,
  canHide,
  showHidden,
  onShowHiddenChange,
}: {
  doneCount: number;
  totalCount: number;
  showMine: boolean;
  onShowMineChange: (show: boolean) => void;
  canHide: boolean;
  showHidden: boolean;
  onShowHiddenChange: (show: boolean) => void;
}): JSX.Element {
  return (
    <div className="border-border bg-surface-secondary-default flex flex-wrap items-center justify-between gap-4 border px-4 py-2.5">
      <div className="flex min-w-0 items-center">
        <span className="text-foreground whitespace-nowrap text-sm tabular-nums">
          {doneCount} of {totalCount} required tasks complete
        </span>
      </div>
      <div className="flex items-center gap-6">
        <label className="text-foreground flex items-center gap-2 text-sm font-medium">
          <span>My tasks</span>
          <Switch
            checked={showMine}
            onCheckedChange={onShowMineChange}
            aria-label="My tasks"
          />
        </label>
        {canHide && (
          <label className="text-foreground flex items-center gap-2 text-sm font-medium">
            <span>Show hidden tasks</span>
            <Switch
              checked={showHidden}
              onCheckedChange={onShowHiddenChange}
              aria-label="Show hidden tasks"
            />
          </label>
        )}
      </div>
    </div>
  );
}

function BoardSkeleton(): JSX.Element {
  return (
    <div className={WORKSTREAM_GRID_CLASS}>
      {ONBOARDING_WORKSTREAMS.map((workstream) => (
        <Skeleton key={workstream.id}>
          <div className="h-6 w-2/3" />
          <div className="h-28 w-full" />
          <div className="h-28 w-full" />
        </Skeleton>
      ))}
    </div>
  );
}

/**
 * The organization setup flow as a board: Quinn's consolidated setup tasks,
 * grouped into outcome-oriented workstreams. Cards open the matching setup
 * step in a dialog; `?task=<id>` deep links straight to one.
 */
export function OnboardingBoard(): JSX.Element {
  const navigate = useNavigate();
  const { orgSlug } = useParams();
  const [searchParams, setSearchParams] = useSearchParams();
  const { markSetupStarted } = useOrgSetupStarted(orgSlug);

  useEffect(() => {
    markSetupStarted();
  }, [markSetupStarted]);

  const projectSlug = searchParams.get("projectSlug") ?? undefined;
  const board = useOnboardingBoard(orgSlug);
  const session = useSession();
  const [showMine, setShowMine] = useState(false);
  const [showHidden, setShowHidden] = useState(false);

  const taskParam = searchParams.get("task");
  const openTaskId =
    taskParam && isOnboardingTaskId(taskParam) ? taskParam : null;
  const openTask = useMemo(
    () => board.tasks.find((task) => task.id === openTaskId) ?? null,
    [board.tasks, openTaskId],
  );

  const setOpenTask = useCallback(
    (id: OnboardingTaskId | null) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (id) {
            next.set("task", id);
          } else {
            next.delete("task");
          }
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const activeTasks = board.tasks.filter((task) => !task.hidden);
  const requiredTasks = activeTasks.filter(
    (task) => task.id !== "platform-mcp",
  );
  const doneCount = requiredTasks.filter(
    (task) => task.status === "done",
  ).length;
  const displayedTasks =
    board.canHideTasks && showHidden ? board.tasks : activeTasks;
  const visibleTasks = showMine
    ? displayedTasks.filter((task) => {
        if (!task.assignee) return false;
        return task.assignee.kind === "user"
          ? task.assignee.userId === session.user.id ||
              task.assignee.email.toLowerCase() ===
                session.user.email.toLowerCase()
          : task.assignee.email.toLowerCase() ===
              session.user.email.toLowerCase();
      })
    : displayedTasks;

  const handleLeave = () => {
    void navigate(`/${orgSlug}`);
  };

  return (
    <div className="bg-background flex h-screen max-h-dvh flex-col overflow-hidden supports-[height:100dvh]:h-dvh">
      <OnboardingHeader onLeave={handleLeave} />

      <main className="flex min-h-0 flex-1 justify-center px-8 py-6">
        <div className="flex min-h-0 w-full max-w-7xl flex-col gap-4">
          <BoardHeader />
          <BoardToolbar
            doneCount={doneCount}
            totalCount={requiredTasks.length}
            showMine={showMine}
            onShowMineChange={setShowMine}
            canHide={board.canHideTasks}
            showHidden={showHidden}
            onShowHiddenChange={setShowHidden}
          />

          {board.isLoading ? (
            <BoardSkeleton />
          ) : (
            <div
              role="region"
              aria-label="Setup workstreams"
              className={WORKSTREAM_GRID_CLASS}
            >
              {ONBOARDING_WORKSTREAMS.map((workstream) => {
                const workstreamTasks = workstream.taskIds
                  .map((id) => visibleTasks.find((task) => task.id === id))
                  .filter((task) => task !== undefined);
                return (
                  <WorkstreamColumn
                    key={workstream.id}
                    workstream={workstream}
                    tasks={workstreamTasks}
                  >
                    {workstreamTasks.map((task) => (
                      <TaskCard
                        key={task.id}
                        task={task}
                        canHide={board.canHideTasks}
                        isReminding={board.remindingTaskId === task.id}
                        onOpen={() => setOpenTask(task.id)}
                        onSetStatus={(next) => board.setStatus(task.id, next)}
                        onAssign={(assignee) => board.assign(task.id, assignee)}
                        onToggleHidden={() =>
                          board.setHidden(task.id, !task.hidden)
                        }
                        onRemind={() => board.remind(task.id)}
                      />
                    ))}
                  </WorkstreamColumn>
                );
              })}
            </div>
          )}
        </div>
      </main>

      <OnboardingFooter />

      <TaskDialog
        task={openTask}
        projectSlug={projectSlug}
        isReminding={openTask !== null && board.remindingTaskId === openTask.id}
        onClose={() => setOpenTask(null)}
        onOpenTask={setOpenTask}
        onSetStatus={board.setStatus}
        onAssign={board.assign}
        onRemind={board.remind}
      />
    </div>
  );
}
