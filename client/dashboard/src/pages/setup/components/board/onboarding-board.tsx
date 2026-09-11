import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router";
import { Skeleton } from "@/components/ui/Skeleton";
import { Switch } from "@/components/ui/Switch";
import { Button } from "@/components/ui/Button";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { assignedTo, type BoardTask } from "./board-store";
import { useRBAC } from "@/hooks/useRBAC";
import { canonicalSetupSearch } from "../../task-slugs";
import { useSession } from "@/contexts/Auth";
import { useOrgSetupStarted } from "@/hooks/useOrgSetupStarted";
import { OnboardingFooter } from "../onboarding-footer";
import { OnboardingHeader } from "../onboarding-header";
import { SetupViewButton } from "../setup-view-button";
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
        Progress is now shared across your organization. Browser-only progress
        is not imported; existing browser records are retained.
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
    <Page.Toolbar>
      <Page.Toolbar.Leading>
        <span className="text-foreground whitespace-nowrap text-sm">
          {totalCount === 0
            ? "No required tasks"
            : `${doneCount} of ${totalCount} required tasks complete`}
        </span>
      </Page.Toolbar.Leading>
      <Page.Toolbar.Actions>
        <label className="text-foreground flex items-center gap-2 text-sm font-medium">
          <span>Assigned to me</span>
          <Switch
            checked={showMine}
            onCheckedChange={onShowMineChange}
            aria-label="Assigned to me"
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
      </Page.Toolbar.Actions>
    </Page.Toolbar>
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
  const session = useSession();
  const { isLoading, error, hasScope } = useRBAC();
  if (!session.organization.id || !session.user.id || isLoading) {
    return (
      <div role="status" className="p-8">
        Loading onboarding access...
      </div>
    );
  }
  if (error) {
    return (
      <div role="alert" className="p-8">
        Could not load onboarding access.{" "}
        <Button onClick={() => window.location.reload()}>Retry</Button>
      </div>
    );
  }
  return (
    <RequireScope
      scope="org:read"
      resourceId={session.organization.id}
      level="page"
    >
      <OnboardingBoardInner
        key={`${session.organization.id}:${session.user.id}:${session.session}:${session.organizationOverride}:${hasScope("org:admin", session.organization.id)}`}
      />
    </RequireScope>
  );
}

function OnboardingBoardInner(): JSX.Element {
  const navigate = useNavigate();
  const { orgSlug } = useParams();
  const [searchParams, setSearchParams] = useSearchParams();
  const { markSetupStarted } = useOrgSetupStarted(orgSlug);

  useEffect(() => {
    markSetupStarted();
  }, [markSetupStarted]);

  const projectSlug = searchParams.get("projectSlug") ?? undefined;
  const board = useOnboardingBoard();
  const session = useSession();
  const [showMine, setShowMine] = useState(false);
  const [showHidden, setShowHidden] = useState(false);

  const canonicalSearch = canonicalSetupSearch(searchParams).toString();
  useEffect(() => {
    if (canonicalSearch !== searchParams.toString())
      setSearchParams(canonicalSearch, { replace: true });
  }, [canonicalSearch, searchParams, setSearchParams]);

  const taskParam = new URLSearchParams(canonicalSearch).get("task");
  const openTaskId =
    taskParam && isOnboardingTaskId(taskParam) ? taskParam : null;
  const openTask = useMemo(
    () =>
      board.tasks.find(
        (task) =>
          task.id === openTaskId && (!task.hidden || board.canHideTasks),
      ) ?? null,
    [board.tasks, openTaskId, board.canHideTasks],
  );

  const setOpenTask = useCallback(
    (id: OnboardingTaskId | null) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          next.delete("step");
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
  const requiredTasks = activeTasks.filter((task) => !task.badge);
  const doneCount = requiredTasks.filter(
    (task) => task.status === "done",
  ).length;
  const displayedTasks =
    board.canHideTasks && showHidden ? board.tasks : activeTasks;
  const visibleTasks = showMine
    ? displayedTasks.filter((task) => assignedTo(task, session.user))
    : displayedTasks;

  const handleLeave = () => {
    void navigate(`/${orgSlug}`);
  };

  const renderTask = (task: BoardTask) => (
    <TaskCard
      key={task.id}
      task={task}
      canHide={board.canHideTasks}
      canAssign={board.canAssign}
      canSetStatus={board.canSetStatus(task)}
      isPending={board.isPending}
      onOpen={() => setOpenTask(task.id)}
      onSetStatus={(next) => void board.setStatus(task.id, next)}
      onAssign={(assignee) => void board.assign(task.id, assignee)}
      onToggleHidden={() => void board.setHidden(task.id, !task.hidden)}
    />
  );

  return (
    <div className="bg-background flex h-screen max-h-dvh flex-col overflow-hidden supports-[height:100dvh]:h-dvh">
      <OnboardingHeader onLeave={handleLeave}>
        {board.canAssign && <SetupViewButton disabled={board.isPending} />}
      </OnboardingHeader>

      <main className="flex min-h-0 flex-1 justify-center px-4 py-6 sm:px-8">
        <div className="flex min-h-0 w-full max-w-7xl flex-col gap-4">
          <BoardHeader />
          {board.error && (
            <div role="alert">
              Could not load setup tasks: {board.error}
              <Button onClick={() => void board.retry()}>Retry</Button>
            </div>
          )}
          {board.writeError && <p role="alert">{board.writeError}</p>}
          {!board.isLoading && !board.error && taskParam && !openTask && (
            <p role="status">
              {openTaskId
                ? "This task is not part of your current onboarding"
                : "Setup task not found"}
            </p>
          )}
          <BoardToolbar
            doneCount={doneCount}
            totalCount={requiredTasks.length}
            showMine={showMine}
            onShowMineChange={setShowMine}
            canHide={board.canHideTasks}
            showHidden={showHidden}
            onShowHiddenChange={setShowHidden}
          />
          {!board.isLoading && !board.error && visibleTasks.length === 0 && (
            <p role="status">
              {showMine ? "No tasks assigned to you" : "No selected tasks"}
            </p>
          )}

          {board.isLoading && <BoardSkeleton />}
          {!board.isLoading && !board.error && (
            <div
              role="region"
              aria-label="Setup workstreams"
              className={WORKSTREAM_GRID_CLASS}
            >
              {ONBOARDING_WORKSTREAMS.map((workstream) => {
                const workstreamTasks = workstream.taskIds
                  .map((id) => visibleTasks.find((task) => task.id === id))
                  .filter((task) => task !== undefined);
                if (workstreamTasks.length === 0) return null;
                return (
                  <WorkstreamColumn
                    key={workstream.id}
                    workstream={workstream}
                    tasks={workstreamTasks}
                  >
                    {workstreamTasks.map(renderTask)}
                  </WorkstreamColumn>
                );
              })}
            </div>
          )}
        </div>
      </main>

      <OnboardingFooter />

      <TaskDialog
        task={board.error ? null : openTask}
        projectSlug={projectSlug}
        canAssign={board.canAssign}
        canSetStatus={openTask !== null && board.canSetStatus(openTask)}
        isPending={board.isPending}
        error={
          board.writeErrorTaskId === openTask?.id ? board.writeError : null
        }
        onClose={() => setOpenTask(null)}
        onSetStatus={board.setStatus}
        onAssign={(id, assignee) => void board.assign(id, assignee)}
      />
    </div>
  );
}
