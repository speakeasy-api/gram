import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router";
import { Skeleton } from "@/components/ui/Skeleton";
import { Switch } from "@/components/ui/Switch";
import { useOrgSetupStarted } from "@/hooks/useOrgSetupStarted";
import { OnboardingFooter } from "../onboarding-footer";
import { OnboardingHeader } from "../onboarding-header";
import { BoardColumn } from "./board-column";
import { TaskCard } from "./task-card";
import { TaskDialog } from "./task-dialog";
import {
  isOnboardingTaskId,
  type OnboardingTaskId,
  TASK_STATUSES,
} from "./tasks";
import { useOnboardingBoard } from "./use-onboarding-board";

const COLUMN_GRID_CLASS =
  "grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-4";

function BoardHeader({
  doneCount,
  totalCount,
  hiddenCount,
  canHide,
  showHidden,
  onShowHiddenChange,
}: {
  doneCount: number;
  totalCount: number;
  hiddenCount: number;
  canHide: boolean;
  showHidden: boolean;
  onShowHiddenChange: (show: boolean) => void;
}): JSX.Element {
  const percent =
    totalCount === 0 ? 0 : Math.round((doneCount / totalCount) * 100);
  return (
    <div className="flex flex-wrap items-end justify-between gap-6">
      <div className="max-w-2xl">
        <span className="text-eyebrow">Organization</span>
        <h1 className="text-display-sm text-foreground mt-1 font-thin">
          Onboarding
        </h1>
        <p className="text-muted-foreground mt-2 text-sm">
          Every setup task on one board. Hand each one to an owner, track where
          it stands, and send a reminder when it stalls.
        </p>
      </div>
      <div className="flex flex-wrap items-center gap-6">
        <div className="flex flex-col gap-1.5">
          <span className="text-eyebrow">Progress</span>
          <div className="flex items-center gap-3">
            <div
              className="bg-border h-1 w-40"
              role="progressbar"
              aria-label="Tasks done"
              aria-valuenow={percent}
              aria-valuemin={0}
              aria-valuemax={100}
            >
              <div
                className="bg-foreground h-full transition-[width]"
                style={{ width: `${percent}%` }}
              />
            </div>
            <span className="text-foreground text-sm tabular-nums">
              {doneCount} of {totalCount} done
            </span>
          </div>
        </div>
        {canHide && hiddenCount > 0 && (
          <label className="text-foreground flex items-center gap-2 text-sm">
            <Switch
              checked={showHidden}
              onCheckedChange={onShowHiddenChange}
              aria-label="Show hidden tasks"
            />
            <span>Show hidden ({hiddenCount})</span>
          </label>
        )}
      </div>
    </div>
  );
}

function BoardSkeleton(): JSX.Element {
  return (
    <div className={COLUMN_GRID_CLASS}>
      {TASK_STATUSES.map((status) => (
        <Skeleton key={status}>
          <div className="h-6 w-2/3" />
          <div className="h-28 w-full" />
          <div className="h-28 w-full" />
        </Skeleton>
      ))}
    </div>
  );
}

/**
 * The organization setup flow as a board: one card per setup task, grouped by
 * status. Cards open the original setup step in a dialog; `?task=<id>` deep
 * links straight to one, which is what reminder emails will point at.
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
  const hiddenCount = board.tasks.length - activeTasks.length;
  const doneCount = activeTasks.filter((task) => task.status === "done").length;
  const visibleTasks =
    board.canHideTasks && showHidden ? board.tasks : activeTasks;

  const handleLeave = () => {
    void navigate(`/${orgSlug}`);
  };

  return (
    <div className="bg-background flex min-h-screen flex-col">
      <OnboardingHeader onLeave={handleLeave} />

      <main className="flex flex-1 justify-center px-8 py-12">
        <div className="flex w-full max-w-7xl flex-col gap-8">
          <BoardHeader
            doneCount={doneCount}
            totalCount={activeTasks.length}
            hiddenCount={hiddenCount}
            canHide={board.canHideTasks}
            showHidden={showHidden}
            onShowHiddenChange={setShowHidden}
          />

          {board.isLoading ? (
            <BoardSkeleton />
          ) : (
            <div className={COLUMN_GRID_CLASS}>
              {TASK_STATUSES.map((status) => {
                const columnTasks = visibleTasks.filter(
                  (task) => task.status === status,
                );
                return (
                  <BoardColumn
                    key={status}
                    status={status}
                    count={columnTasks.length}
                    onDropTask={(id) => board.setStatus(id, status)}
                  >
                    {columnTasks.map((task) => (
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
                  </BoardColumn>
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
