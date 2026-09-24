import { useEffect, useState } from "react";
import {
  useLocation,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router";
import { cn } from "@/lib/utils";
import { SETUP_CONTAINER } from "../setup-container";
import { useOrgRoutes } from "@/routes";
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
import { isOnboardingTaskId } from "./tasks";
import { useOnboardingBoard } from "./use-onboarding-board";
import { WorkstreamColumn } from "./workstream-column";

const WORKSTREAM_GRID_CLASS =
  "grid min-h-0 flex-1 auto-rows-max grid-cols-1 justify-center gap-4 overflow-y-auto md:auto-rows-[minmax(0,1fr)] md:grid-cols-[repeat(auto-fit,minmax(0,calc((100%_-_1rem)/2)))] xl:grid-cols-[repeat(auto-fit,minmax(0,calc((100%_-_3rem)/4)))] xl:overflow-hidden";

function BoardHeader(): JSX.Element {
  return (
    <div className="max-w-2xl">
      <span className="text-eyebrow">Organization</span>
      {/* Optically align Tobias's opening O with the smaller text below. */}
      <h1 className="text-display-sm text-foreground mt-1 -ms-[0.05em] font-thin">
        Onboarding
      </h1>
      <p className="text-muted-foreground mt-2 text-sm">
        Assign and track the work required to prepare your organization.
      </p>
    </div>
  );
}

function BoardToolbar({
  doneCount,
  totalCount,
  hasUnsupportedTasks,
  showMine,
  onShowMineChange,
  canHide,
  showHidden,
  onShowHiddenChange,
}: {
  hasUnsupportedTasks: boolean;
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
            ? hasUnsupportedTasks
              ? "No supported required tasks"
              : "No required tasks"
            : `${doneCount} of ${totalCount} ${hasUnsupportedTasks ? "supported " : ""}required tasks complete`}
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
      {[0, 1, 2, 3].map((slot) => (
        <Skeleton key={slot}>
          <div className="h-6 w-2/3" />
          <div className="h-28 w-full" />
          <div className="h-28 w-full" />
        </Skeleton>
      ))}
    </div>
  );
}

/**
 * The organization setup flow as a board: consolidated setup tasks,
 * grouped into outcome-oriented workstreams. Cards open the matching setup
 * step in the admin-only wizard; `?task=<id>` deep links straight to one.
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
  const { hash } = useLocation();
  const routes = useOrgRoutes();
  const { orgSlug } = useParams();
  const [searchParams] = useSearchParams();
  const { markSetupStarted } = useOrgSetupStarted(orgSlug);

  useEffect(() => {
    markSetupStarted();
  }, [markSetupStarted]);

  const board = useOnboardingBoard();
  const session = useSession();
  const [showMine, setShowMine] = useState(false);
  const [showHidden, setShowHidden] = useState(false);
  const canonicalSearch = canonicalSetupSearch(searchParams).toString();
  useEffect(() => {
    if (canonicalSearch !== searchParams.toString())
      void navigate({ search: canonicalSearch, hash }, { replace: true });
  }, [canonicalSearch, searchParams, hash, navigate]);

  const taskParam = new URLSearchParams(canonicalSearch).get("task");
  const openTaskId =
    taskParam && isOnboardingTaskId(taskParam) ? taskParam : null;
  const openTask = board.tasks.find(
    (task) => task.id === openTaskId && (!task.hidden || board.canHideTasks),
  );
  useEffect(() => {
    if (!board.isLoading && !board.error && openTask && board.canAssign) {
      const search = new URLSearchParams(canonicalSearch);
      search.delete("view");
      void navigate(
        {
          pathname: routes.setupWizard.href(),
          search: search.toString(),
          hash,
        },
        { replace: true },
      );
    }
  }, [
    board.canAssign,
    hash,
    board.isLoading,
    board.error,
    openTask,
    canonicalSearch,
    navigate,
    routes,
  ]);

  const activeTasks = board.tasks.filter((task) => !task.hidden);
  const requiredTasks = activeTasks.filter((task) => !task.badge);
  const doneCount = requiredTasks.filter(
    (task) => task.verified || task.status === "done",
  ).length;
  const displayedTasks =
    board.canHideTasks && showHidden ? board.tasks : activeTasks;
  const visibleTasks = showMine
    ? displayedTasks.filter((task) => assignedTo(task, session.user))
    : displayedTasks;

  const handleLeave = () => {
    void navigate(`/${orgSlug}`);
  };

  const openSetupTask = (id: string) => {
    const target = board.tasks.find((task) => task.id === id);
    if (!board.canAssign || !target || (target.hidden && !board.canHideTasks))
      return;
    const search = new URLSearchParams(canonicalSearch);
    search.delete("view");
    search.delete("step");
    search.set("task", target.id);
    search.set("from", "workstreams");
    void navigate({
      pathname: routes.setupWizard.href(),
      hash,
      search: search.toString(),
    });
  };

  const renderTask = (task: BoardTask) => (
    <TaskCard
      key={task.id}
      task={task}
      canOpen={board.canAssign}
      canHide={board.canHideTasks}
      canSetStatus={board.canSetStatus(task)}
      isPending={board.isPending}
      onOpen={() => openSetupTask(task.id)}
      reachableTaskIds={
        board.canAssign
          ? board.tasks
              .filter((item) => !item.hidden || board.canHideTasks)
              .map((item) => item.id)
          : []
      }
      onGoToTask={openSetupTask}
      onSetStatus={(next) => void board.setStatus(task.id, next)}
      onToggleHidden={() => void board.setHidden(task.id, !task.hidden)}
    />
  );

  return (
    <div className="bg-background flex h-screen max-h-dvh flex-col overflow-hidden supports-[height:100dvh]:h-dvh">
      <OnboardingHeader onLeave={handleLeave}>
        {board.canAssign && <SetupViewButton disabled={board.isPending} />}
      </OnboardingHeader>

      <main className="flex min-h-0 flex-1 justify-center py-6">
        <div className={cn(SETUP_CONTAINER, "flex min-h-0 flex-col gap-4")}>
          <BoardHeader />
          {!board.canAssign && (
            <p className="text-muted-foreground text-sm">
              An organization admin is required to open setup tasks. You can
              still update the status of tasks assigned to you.
            </p>
          )}
          {board.unsupportedTaskKeys?.length > 0 && (
            <p role="alert" className="text-sm text-default-warning">
              Some setup tasks are not supported by this version:{" "}
              {board.unsupportedTaskKeys.join(", ")}. Progress below covers
              supported tasks only, not all onboarding work. Refresh to update,
              or contact support.
            </p>
          )}
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
            hasUnsupportedTasks={Boolean(board.unsupportedTaskKeys?.length)}
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
              {board.workstreams.map((workstream) => {
                const workstreamTasks = workstream.taskKeys
                  .map((id) => visibleTasks.find((task) => task.id === id))
                  .filter((task) => task !== undefined);
                if (workstreamTasks.length === 0) return null;
                return (
                  <WorkstreamColumn
                    key={workstream.id}
                    workstream={workstream}
                    tasks={workstreamTasks}
                    allTasks={board.tasks.filter((task) =>
                      workstream.taskKeys.includes(task.id),
                    )}
                    canAssign={board.canAssign}
                    isPending={board.isPending}
                    onAssign={(owner) =>
                      board.assignWorkstream(workstream, owner)
                    }
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
    </div>
  );
}
