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
import { assignedTo, type OnboardingTask } from "../../onboarding-model";
import {
  useOnboarding,
  useOnboardingActions,
  type OnboardingWriteOutcome,
} from "../../use-onboarding";
import { useRBAC } from "@/hooks/useRBAC";
import { canonicalSetupSearch, setupTaskKeyForSlug } from "../../task-slugs";
import { useSession } from "@/contexts/Auth";
import { useOrgSetupStarted } from "@/hooks/useOrgSetupStarted";
import { OnboardingFooter } from "../onboarding-footer";
import { OnboardingHeader } from "../onboarding-header";
import { SetupViewButton } from "../setup-view-button";
import { TaskCard } from "./task-card";
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

// Views own their feedback; the shared actions only report what happened.
function writeMessage(outcome: OnboardingWriteOutcome): string | null {
  if (outcome.status === "failed") return outcome.message;
  if (outcome.status === "saved_stale") return `Saved. ${outcome.message}`;
  return null;
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

  const onboarding = useOnboarding();
  const actions = useOnboardingActions(onboarding);
  const { model, canAssign, canInspectHidden } = onboarding;
  const session = useSession();
  const [showMine, setShowMine] = useState(false);
  const [showHidden, setShowHidden] = useState(false);
  const [writeError, setWriteError] = useState<string | null>(null);
  const canonicalSearch = canonicalSetupSearch(searchParams).toString();
  useEffect(() => {
    if (canonicalSearch !== searchParams.toString())
      void navigate({ search: canonicalSearch, hash }, { replace: true });
  }, [canonicalSearch, searchParams, hash, navigate]);

  const ready = !onboarding.isLoading && !onboarding.error;
  const taskParam = new URLSearchParams(canonicalSearch).get("task");
  // The wizard writes short aliases (?task=idp); resolve them like keys.
  const requestedTask = taskParam
    ? model.task(setupTaskKeyForSlug(taskParam) ?? taskParam)
    : undefined;
  // Tasks a reader may open: supported ones, hidden only for staff.
  const openable = (task: OnboardingTask | undefined): task is OnboardingTask =>
    Boolean(canAssign && task?.supported && (!task.hidden || canInspectHidden));
  const openTask = openable(requestedTask) ? requestedTask : undefined;
  useEffect(() => {
    if (ready && openTask) {
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
  }, [ready, hash, openTask, canonicalSearch, navigate, routes]);

  const displayedTasks =
    canInspectHidden && showHidden ? model.tasks : model.visibleTasks;
  const visibleTasks = showMine
    ? displayedTasks.filter((task) => assignedTo(task, session.user))
    : displayedTasks;
  const shown = new Set(visibleTasks.map((task) => task.id));

  const handleLeave = () => {
    void navigate(`/${orgSlug}`);
  };

  /** Resolves true once the write committed, even if the refresh failed. */
  const report = async (
    write: Promise<OnboardingWriteOutcome>,
  ): Promise<boolean> => {
    const outcome = await write;
    // Refusals mean nothing was sent, so they leave the last message alone.
    if (outcome.status !== "rejected") setWriteError(writeMessage(outcome));
    return outcome.status === "saved" || outcome.status === "saved_stale";
  };

  const openSetupTask = (id: string) => {
    const target = model.task(id);
    if (!openable(target)) return;
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

  const reachableTaskIds = model.tasks
    .filter((task) => openable(task))
    .map((task) => task.id);

  const renderTask = (task: OnboardingTask) => (
    <TaskCard
      key={task.id}
      task={task}
      canOpen={openable(task)}
      canHide={canInspectHidden}
      canSetStatus={actions.canSetStatus(task)}
      isPending={actions.isPending}
      onOpen={() => openSetupTask(task.id)}
      reachableTaskIds={reachableTaskIds}
      dependencyTitle={model.titleFor}
      onGoToTask={openSetupTask}
      onSetStatus={(next) => void report(actions.setStatus(task.id, next))}
      onToggleHidden={() =>
        void report(actions.setHidden(task.id, !task.hidden))
      }
    />
  );

  return (
    <div className="bg-background flex h-screen max-h-dvh flex-col overflow-hidden supports-[height:100dvh]:h-dvh">
      <OnboardingHeader onLeave={handleLeave}>
        {canAssign && <SetupViewButton disabled={actions.isPending} />}
      </OnboardingHeader>

      <main className="flex min-h-0 flex-1 justify-center py-6">
        <div className={cn(SETUP_CONTAINER, "flex min-h-0 flex-col gap-4")}>
          <BoardHeader />
          {!canAssign && (
            <p className="text-muted-foreground text-sm">
              An organization admin is required to open setup tasks. You can
              still update the status of tasks assigned to you.
            </p>
          )}
          {model.unsupportedTaskKeys.length > 0 && (
            <p role="alert" className="text-sm text-default-warning">
              Some setup tasks cannot be opened in this version:{" "}
              {model.unsupportedTaskKeys.join(", ")}. Refresh to update, or
              contact support.
            </p>
          )}
          {onboarding.error && (
            <div role="alert">
              Could not load setup tasks: {onboarding.error}
              <Button onClick={() => void onboarding.retry()}>Retry</Button>
            </div>
          )}
          {writeError && <p role="alert">{writeError}</p>}
          {ready && taskParam && !openTask && (
            <p role="status">
              {requestedTask
                ? "This task is not part of your current onboarding"
                : "Setup task not found"}
            </p>
          )}
          <BoardToolbar
            doneCount={model.progress.done}
            totalCount={model.progress.total}
            showMine={showMine}
            onShowMineChange={setShowMine}
            canHide={canInspectHidden}
            showHidden={showHidden}
            onShowHiddenChange={setShowHidden}
          />
          {ready && visibleTasks.length === 0 && (
            <p role="status">
              {showMine ? "No tasks assigned to you" : "No selected tasks"}
            </p>
          )}

          {onboarding.isLoading && <BoardSkeleton />}
          {ready && (
            <div
              role="region"
              aria-label="Setup workstreams"
              className={WORKSTREAM_GRID_CLASS}
            >
              {model.workstreams.map((workstream) => {
                const workstreamTasks = workstream.tasks.filter((task) =>
                  shown.has(task.id),
                );
                if (workstreamTasks.length === 0) return null;
                return (
                  <WorkstreamColumn
                    key={workstream.id}
                    workstream={workstream}
                    tasks={workstreamTasks}
                    canAssign={canAssign}
                    isPending={actions.isPending}
                    onAssign={(owner) =>
                      report(actions.assignWorkstream(workstream.id, owner))
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
