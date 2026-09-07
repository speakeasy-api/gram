import { IdentityLink } from "@/components/identity-link";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { useSession } from "@/contexts/Auth";
import { capitalize } from "@/lib/utils";
import { useOrgRoutes } from "@/routes";
import { useQueryClient } from "@tanstack/react-query";
import { useMembers } from "@gram/client/react-query/members.js";
import {
  invalidateKillswitch,
  useKillswitch,
} from "@gram/client/react-query/killswitch.js";
import { invalidateAllKillswitches } from "@gram/client/react-query/killswitches.js";
import { useKillswitchCapabilities } from "@gram/client/react-query/killswitchCapabilities.js";
import { useKillswitchMCPServers } from "@gram/client/react-query/killswitchMCPServers.js";
import { useEditKillswitchMutation } from "@gram/client/react-query/editKillswitch.js";
import { useLiftKillswitchMutation } from "@gram/client/react-query/liftKillswitch.js";
import { usePreviewKillswitchOverlapsMutation } from "@gram/client/react-query/previewKillswitchOverlaps.js";
import type { KillswitchDetail as KillswitchDetailModel } from "@gram/client/models/components/killswitchdetail.js";
import type { KillswitchHistoryEvent } from "@gram/client/models/components/killswitchhistoryevent.js";
import type { KillswitchOverlap } from "@gram/client/models/components/killswitchoverlap.js";
import { Link, useParams } from "react-router";
import {
  lazy,
  memo,
  Suspense,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  conflictName,
  draftToSchedule,
  draftToScope,
  nextScheduleBoundaryDelay,
  scheduleLabel,
  scopeLabel,
  serverDiff,
  type EditorDraft,
} from "./killswitch-view-model";

const KillswitchEditorSheet = lazy(() =>
  import("./KillswitchEditorSheet").then((module) => ({
    default: module.KillswitchEditorSheet,
  })),
);
const LiftKillswitchDialog = lazy(() =>
  import("./LiftKillswitchDialog").then((module) => ({
    default: module.LiftKillswitchDialog,
  })),
);
const EMPTY: never[] = [];

function isTerminalStatus(status: KillswitchDetailModel["status"]): boolean {
  return status === "expired" || status === "lifted";
}

type OverlapPreviewState = {
  killswitchId: string;
  version: number;
  status: "loading" | "ready" | "error";
  overlaps: KillswitchOverlap[];
  truncated: boolean;
  error?: string;
};

type RouteAuthority = { id: string };

function versionConflictError(): Error {
  return Object.assign(new Error("The Killswitch changed."), {
    statusCode: 409,
    data$: { name: "version_conflict" },
  });
}

function overlapPreviewKey(detail: KillswitchDetailModel): string {
  return `${detail.id}:${detail.version}`;
}

export default function KillswitchDetail(): JSX.Element {
  const { killswitchId = "" } = useParams();
  const session = useSession();
  const security = { sessionHeaderGramSession: session.session };
  const routes = useOrgRoutes();
  const queryClient = useQueryClient();
  const [editOpen, setEditOpen] = useState(false);
  const [liftOpen, setLiftOpen] = useState(false);
  const [liftReview, setLiftReview] = useState<KillswitchDetailModel>();
  const [authorityBlocked, setAuthorityBlocked] = useState(false);
  const detailQuery = useKillswitch(
    security,
    { id: killswitchId, gramSession: session.session },
    {
      throwOnError: false,
    },
  );
  const membersQuery = useMembers({ gramSession: session.session }, security, {
    throwOnError: false,
  });
  const capabilitiesQuery = useKillswitchCapabilities(
    security,
    { gramSession: session.session },
    {
      enabled: editOpen,
      throwOnError: false,
    },
  );
  const serversQuery = useKillswitchMCPServers(
    security,
    { gramSession: session.session },
    { throwOnError: false },
  );
  const editMutation = useEditKillswitchMutation();
  const liftMutation = useLiftKillswitchMutation();
  const overlapMutation = usePreviewKillswitchOverlapsMutation();
  const [overlapPreview, setOverlapPreview] = useState<OverlapPreviewState>();
  const renderedRoute = useMemo<RouteAuthority>(
    () => ({ id: killswitchId }),
    [killswitchId],
  );
  const routeEpoch = useRef(renderedRoute);
  const previewRequest = useRef(0);
  const previewedKey = useRef<string | undefined>(undefined);
  const changeableRef = useRef<{
    detail?: KillswitchDetailModel;
    routeId: string;
    blocked: boolean;
  }>({ routeId: killswitchId, blocked: true });
  const refetchDetail = detailQuery.refetch;

  const detail = detailQuery.data;
  const changeBlocked = Boolean(
    authorityBlocked ||
    detailQuery.error ||
    detailQuery.isFetching ||
    membersQuery.error ||
    serversQuery.error,
  );
  useLayoutEffect(() => {
    routeEpoch.current = renderedRoute;
    changeableRef.current = {
      detail,
      routeId: killswitchId,
      blocked: changeBlocked,
    };
  }, [changeBlocked, detail, killswitchId, renderedRoute]);

  const currentOverlapPreview =
    detail?.id === killswitchId &&
    overlapPreview?.killswitchId === killswitchId &&
    overlapPreview.version === detail.version
      ? overlapPreview
      : undefined;
  const overlaps = currentOverlapPreview?.overlaps ?? EMPTY;
  const overlapsTruncated = currentOverlapPreview?.truncated ?? false;
  const overlapError = currentOverlapPreview?.error;
  const overlapPreviewStatus = currentOverlapPreview?.status ?? "loading";
  const liftOverlapPreview =
    liftReview &&
    overlapPreview?.killswitchId === liftReview.id &&
    overlapPreview.version === liftReview.version
      ? overlapPreview
      : undefined;
  const members = membersQuery.data?.members ?? EMPTY;
  const servers = serversQuery.data?.servers ?? EMPTY;
  const serverNames = useMemo(
    () => new Map(servers.map((server) => [server.id, server.name])),
    [servers],
  );
  const memberName = members.find(
    (member) => member.id === detail?.userId,
  )?.name;
  const catalogError = membersQuery.error ?? serversQuery.error;
  const catalogUnavailable = Boolean(
    (membersQuery.error && !membersQuery.data) ||
    (serversQuery.error && !serversQuery.data),
  );
  const retainedCatalogError =
    catalogError && !catalogUnavailable ? catalogError : undefined;
  const catalogLoading = membersQuery.isLoading || serversQuery.isLoading;

  useEffect(() => {
    if (!detail || !isTerminalStatus(detail.status)) return;
    setEditOpen(false);
    setLiftOpen(false);
    setLiftReview(undefined);
  }, [detail]);

  const requireChangeable = (): KillswitchDetailModel => {
    const current = changeableRef.current;
    if (!current.detail || isTerminalStatus(current.detail.status)) {
      if (routeEpoch.current === renderedRoute) {
        setEditOpen(false);
        setLiftOpen(false);
      }
      throw new Error("This Killswitch can no longer be changed.");
    }
    if (
      current.detail.id !== current.routeId ||
      current.routeId !== renderedRoute.id ||
      routeEpoch.current !== renderedRoute ||
      current.blocked
    ) {
      if (routeEpoch.current === renderedRoute) setLiftOpen(false);
      throw new Error("The latest Killswitch version is unavailable.");
    }
    return current.detail;
  };

  const loadOverlaps = async (current: KillswitchDetailModel) => {
    const route = renderedRoute;
    if (routeEpoch.current !== route || current.id !== route.id) return;
    const request = ++previewRequest.current;
    const base = {
      killswitchId: current.id,
      version: current.version,
      overlaps: [],
      truncated: false,
    };
    setOverlapPreview({ ...base, status: "loading" });
    try {
      const result = await overlapMutation.mutateAsync({
        security,
        request: {
          killswitchPreviewOverlapsRequest: {
            id: current.id,
            userId: current.userId,
            capabilityKey: current.capabilityKey,
            scope: current.scope,
            schedule: current.schedule,
          },
        },
      });
      if (request !== previewRequest.current || routeEpoch.current !== route)
        return;
      setOverlapPreview({
        ...base,
        status: "ready",
        overlaps: result.overlaps,
        truncated: result.truncated,
      });
    } catch (error) {
      if (request === previewRequest.current && routeEpoch.current === route) {
        setOverlapPreview({
          ...base,
          status: "error",
          error:
            error instanceof Error ? error.message : "Unable to load overlaps.",
        });
      }
      throw error;
    }
  };

  useEffect(() => {
    previewRequest.current += 1;
    previewedKey.current = undefined;
    setOverlapPreview(undefined);
    setAuthorityBlocked(false);
    setEditOpen(false);
    setLiftOpen(false);
    setLiftReview(undefined);
  }, [killswitchId]);

  useEffect(() => {
    if (!detail || detail.id !== killswitchId) return;
    const key = overlapPreviewKey(detail);
    if (liftOpen && liftReview && key !== overlapPreviewKey(liftReview)) return;
    if (previewedKey.current === key) return;
    previewedKey.current = key;
    void loadOverlaps(detail).catch(() => undefined);
    // The generated preview endpoint is a POST mutation, so the detail page
    // triggers it once per API record version rather than issuing one request per row.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    detail?.id,
    detail?.version,
    killswitchId,
    liftOpen,
    liftReview?.id,
    liftReview?.version,
  ]);

  useEffect(() => {
    if (!detail) return;
    const delay = nextScheduleBoundaryDelay([detail.schedule]);
    if (delay == null) return;
    const timer = window.setTimeout(() => {
      void Promise.all([
        refetchDetail(),
        invalidateAllKillswitches(queryClient),
      ]);
    }, delay);
    return () => window.clearTimeout(timer);
  }, [detail, refetchDetail, queryClient]);

  const previewDraft = (draft: EditorDraft) => {
    const current = requireChangeable();
    return overlapMutation.mutateAsync({
      security,
      request: {
        killswitchPreviewOverlapsRequest: {
          id: current.id,
          userId: draft.userId,
          capabilityKey: "mcp_tool_calls",
          scope: draftToScope(draft),
          schedule: draftToSchedule(draft),
        },
      },
    });
  };

  const invalidate = (id: string) =>
    Promise.all([
      invalidateAllKillswitches(queryClient),
      invalidateKillswitch(queryClient, [{ id }]),
    ]);

  const edit = async (
    draft: EditorDraft,
    operationId: string,
    expectedVersion?: number,
  ) => {
    const current = requireChangeable();
    if (expectedVersion == null)
      throw new Error("The current version is unavailable.");
    try {
      const receipt = await editMutation.mutateAsync({
        security,
        request: {
          killswitchEditRequest: {
            id: current.id,
            expectedVersion,
            operationId,
            scope: draftToScope(draft),
            schedule: draftToSchedule(draft),
            externalNote: draft.externalNote,
            internalNote: draft.internalNote,
          },
        },
      });
      await invalidate(current.id);
      return receipt;
    } catch (error) {
      if (
        conflictName(error) === "version_conflict" &&
        routeEpoch.current === renderedRoute &&
        changeableRef.current.routeId === current.id
      ) {
        setAuthorityBlocked(true);
        setLiftOpen(false);
      }
      throw error;
    }
  };

  const refreshConflict = async (): Promise<KillswitchDetailModel> => {
    const route = renderedRoute;
    const routeId = route.id;
    if (
      routeEpoch.current !== route ||
      changeableRef.current.routeId !== routeId
    )
      throw new Error("The latest version could not be loaded.");
    setAuthorityBlocked(true);
    const result = await detailQuery.refetch();
    if (
      result.error ||
      result.isError ||
      !result.data ||
      result.data.id !== routeId ||
      routeEpoch.current !== route ||
      changeableRef.current.routeId !== routeId
    ) {
      if (routeEpoch.current === route) setLiftOpen(false);
      throw new Error("The latest version could not be loaded.");
    }
    if (isTerminalStatus(result.data.status)) {
      setEditOpen(false);
      setLiftOpen(false);
      throw new Error("This Killswitch can no longer be changed.");
    }
    previewedKey.current = overlapPreviewKey(result.data);
    try {
      await loadOverlaps(result.data);
    } catch (error) {
      if (routeEpoch.current === route) setLiftOpen(false);
      throw error;
    }
    if (
      routeEpoch.current !== route ||
      changeableRef.current.routeId !== routeId
    )
      throw new Error("The latest version could not be loaded.");
    setAuthorityBlocked(false);
    return result.data;
  };

  const lift = async (operationId: string, reviewed: KillswitchDetailModel) => {
    const route = renderedRoute;
    const current = requireChangeable();
    if (current.id !== reviewed.id || current.version !== reviewed.version) {
      const latest = await refreshConflict();
      if (routeEpoch.current === route) setLiftReview(latest);
      throw versionConflictError();
    }
    try {
      const result = await liftMutation.mutateAsync({
        security,
        request: {
          killswitchLiftRequest: {
            id: current.id,
            expectedVersion: current.version,
            operationId,
          },
        },
      });
      if (
        routeEpoch.current === route &&
        changeableRef.current.routeId === current.id
      ) {
        previewedKey.current = `${result.result.id}:${result.result.version}`;
        setOverlapPreview({
          killswitchId: result.result.id,
          version: result.result.version,
          status: "ready",
          overlaps: result.remainingOverlaps,
          truncated: result.truncated,
        });
      }
      await invalidate(current.id);
    } catch (error) {
      if (
        conflictName(error) === "version_conflict" &&
        routeEpoch.current === route
      ) {
        try {
          const latest = await refreshConflict();
          if (routeEpoch.current === route) setLiftReview(latest);
        } catch (refreshError) {
          if (routeEpoch.current === route) setLiftOpen(false);
          throw refreshError;
        }
      }
      throw error;
    }
  };

  if (detailQuery.isLoading) {
    return (
      <main className="mx-auto max-w-[1100px] p-8 text-sm text-muted-foreground">
        Loading Killswitch…
      </main>
    );
  }
  if (!detail) {
    return (
      <main className="mx-auto max-w-[1100px] space-y-4 p-8">
        <Alert variant="error">
          <AlertTitle>Killswitch unavailable</AlertTitle>
          <AlertDescription>
            {detailQuery.error?.message ?? "This Killswitch was not found."}
          </AlertDescription>
        </Alert>
        {detailQuery.error && (
          <Button
            variant="secondary"
            onClick={() => void detailQuery.refetch()}
          >
            Try again
          </Button>
        )}
        <Button variant="tertiary" asChild>
          <Link to={routes.killswitch.href()}>Back to Killswitch</Link>
        </Button>
      </main>
    );
  }

  if (catalogLoading) {
    return (
      <main className="mx-auto max-w-[1100px] p-8 text-sm text-muted-foreground">
        Loading Killswitch details…
      </main>
    );
  }
  if (catalogUnavailable) {
    return (
      <main className="mx-auto max-w-[1100px] space-y-4 p-8">
        <Alert variant="error">
          <AlertTitle>Unable to load Killswitch details</AlertTitle>
          <AlertDescription>{catalogError?.message}</AlertDescription>
        </Alert>
        <Button
          variant="secondary"
          onClick={() =>
            void Promise.all([membersQuery.refetch(), serversQuery.refetch()])
          }
        >
          Try again
        </Button>
      </main>
    );
  }

  const canChange =
    detail.id === killswitchId &&
    !detailQuery.error &&
    !detailQuery.isFetching &&
    !catalogError &&
    (detail.status === "active" || detail.status === "scheduled");
  return (
    <main className="mx-auto w-full max-w-[1100px] space-y-8 px-4 py-6 sm:px-8 sm:py-8">
      {detailQuery.error && (
        <Alert variant="error">
          <AlertTitle>Latest Killswitch version unavailable</AlertTitle>
          <AlertDescription>
            The last loaded details remain visible, but changes are blocked
            until a refresh succeeds.
            <div className="mt-2">
              <Button
                size="sm"
                variant="secondary"
                onClick={() => {
                  if (authorityBlocked) {
                    void refreshConflict().catch(() => undefined);
                  } else {
                    void detailQuery.refetch();
                  }
                }}
              >
                Try again
              </Button>
            </div>
          </AlertDescription>
        </Alert>
      )}
      {retainedCatalogError && (
        <Alert variant="error">
          <AlertTitle>Latest Killswitch resources unavailable</AlertTitle>
          <AlertDescription>
            Existing member and MCP server details remain visible, but changes
            are blocked until a refresh succeeds.
            <div className="mt-2">
              <Button
                size="sm"
                variant="secondary"
                onClick={() =>
                  void Promise.all([
                    membersQuery.refetch(),
                    serversQuery.refetch(),
                  ])
                }
              >
                Try again
              </Button>
            </div>
          </AlertDescription>
        </Alert>
      )}
      <header className="space-y-4">
        <Link
          className="text-muted-foreground text-sm hover:underline"
          to={routes.killswitch.href()}
        >
          ← Killswitch
        </Link>
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-2xl font-semibold">
                {detail.capabilityLabel}
              </h1>
              <Badge
                variant={detail.status === "active" ? "success" : "neutral"}
              >
                {capitalize(detail.status)}
              </Badge>
            </div>
            <p className="text-muted-foreground mt-1">
              <IdentityLink
                identifier={detail.userId ? { userId: detail.userId } : null}
              >
                {memberName ?? "Deleted member"}
              </IdentityLink>{" "}
              · Version {detail.version}
            </p>
          </div>
          {canChange && (
            <div className="flex w-full flex-col gap-2 sm:w-auto sm:flex-row">
              <Button variant="secondary" onClick={() => setEditOpen(true)}>
                Edit killswitch
              </Button>
              {!authorityBlocked && (
                <Button
                  variant="destructive-primary"
                  onClick={() => {
                    const current = requireChangeable();
                    setLiftReview(current);
                    setLiftOpen(true);
                    void loadOverlaps(current).catch(() => undefined);
                  }}
                >
                  Lift killswitch
                </Button>
              )}
            </div>
          )}
        </div>
      </header>

      <section className="grid gap-4 border p-5 sm:grid-cols-2">
        <DetailValue
          label="Member"
          value={
            <IdentityLink
              identifier={detail.userId ? { userId: detail.userId } : null}
            >
              {memberName ?? "Deleted member"}
            </IdentityLink>
          }
        />
        <DetailValue
          label="Capability"
          value={`${detail.capabilityLabel} (${detail.capabilityKey})`}
        />
        <DetailValue
          label="MCP server scope"
          value={scopeLabel(detail.scope, serverNames)}
        />
        <DetailValue label="Schedule" value={scheduleLabel(detail.schedule)} />
      </section>

      <section className="space-y-4">
        <h2 className="text-lg font-semibold">Notes</h2>
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="border p-4">
            <h3 className="text-sm font-medium">
              Public member-facing message
            </h3>
            <p className="text-muted-foreground mt-1 text-xs">
              Shown exactly as plain text.
            </p>
            <p className="mt-2 whitespace-pre-wrap break-words text-sm">
              {detail.externalNote}
            </p>
          </div>
          <div className="border p-4">
            <h3 className="text-sm font-medium">Internal note</h3>
            <p className="text-muted-foreground mt-1 text-xs">
              Visible only to organization admins.
            </p>
            <p className="mt-2 whitespace-pre-wrap break-words text-sm">
              {detail.internalNote}
            </p>
          </div>
        </div>
      </section>

      <section className="space-y-3">
        <h2 className="text-lg font-semibold">Current overlaps</h2>
        <p className="text-muted-foreground text-sm">
          Includes only Killswitches for this member and capability whose server
          scope and schedule intersect.
        </p>
        {overlapPreviewStatus === "loading" ? (
          <p role="status" className="text-muted-foreground text-sm">
            Refreshing overlaps…
          </p>
        ) : overlapError ? (
          <Alert variant="error">
            <AlertTitle>Unable to load overlaps</AlertTitle>
            <AlertDescription>
              {overlapError}
              <div className="mt-2">
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() => {
                    void loadOverlaps(detail).catch(() => undefined);
                  }}
                >
                  Try again
                </Button>
              </div>
            </AlertDescription>
          </Alert>
        ) : overlaps.length === 0 ? (
          <div className="border border-dashed p-4 text-sm">
            No overlapping active or scheduled Killswitches.
          </div>
        ) : (
          <ul className="divide-y border">
            {overlaps.map((overlap) => (
              <li key={overlap.id} className="p-4 text-sm">
                <Link
                  className="font-medium hover:underline"
                  to={routes.killswitch.detail.href(overlap.id)}
                >
                  {scopeLabel(overlap.scope, serverNames)}
                </Link>
                <div className="text-muted-foreground">
                  {scheduleLabel(overlap.schedule)} ·{" "}
                  {capitalize(overlap.status)}
                </div>
              </li>
            ))}
          </ul>
        )}
        {overlapsTruncated && (
          <p className="text-muted-foreground text-sm">
            Additional overlaps are not shown by the API.
          </p>
        )}
      </section>

      <History detail={detail} serverNames={serverNames} />

      {editOpen && (
        <Suspense
          fallback={
            <p role="status" className="text-muted-foreground text-sm">
              Loading editor…
            </p>
          }
        >
          <KillswitchEditorSheet
            open
            onOpenChange={setEditOpen}
            mode="edit"
            members={members}
            servers={servers}
            capabilities={capabilitiesQuery.data?.capabilities ?? []}
            comingSoon={capabilitiesQuery.data?.comingSoon ?? []}
            capabilitiesLoading={capabilitiesQuery.isLoading}
            capabilitiesError={capabilitiesQuery.error}
            onRetryCapabilities={() => {
              void capabilitiesQuery.refetch();
            }}
            initial={detail}
            initiallyStale={authorityBlocked}
            onPreview={previewDraft}
            onSubmit={edit}
            onRefreshConflict={refreshConflict}
            onView={() => setEditOpen(false)}
          />
        </Suspense>
      )}
      {liftOpen && liftReview && (
        <Suspense
          fallback={
            <p role="status" className="text-muted-foreground text-sm">
              Loading lift dialog…
            </p>
          }
        >
          <LiftKillswitchDialog
            open
            onOpenChange={(open) => {
              if (routeEpoch.current !== renderedRoute) return;
              setLiftOpen(open);
              if (!open) setLiftReview(undefined);
            }}
            overlaps={liftOverlapPreview?.overlaps ?? EMPTY}
            overlapsTruncated={liftOverlapPreview?.truncated ?? false}
            serverNames={serverNames}
            previewStatus={liftOverlapPreview?.status ?? "loading"}
            previewError={liftOverlapPreview?.error}
            onRetryPreview={() => loadOverlaps(liftReview)}
            onLift={(operationId) => lift(operationId, liftReview)}
          />
        </Suspense>
      )}
    </main>
  );
}

function DetailValue({
  label,
  value,
}: {
  label: string;
  value: React.ReactNode;
}): JSX.Element {
  return (
    <div>
      <dt className="text-muted-foreground text-xs font-medium uppercase">
        {label}
      </dt>
      <dd className="mt-1 text-sm">{value}</dd>
    </div>
  );
}

const History = memo(function History({
  detail,
  serverNames,
}: {
  detail: KillswitchDetailModel;
  serverNames: ReadonlyMap<string, string>;
}): JSX.Element {
  const { history, eventsByVersion } = useMemo(() => {
    const eventsByVersion = new Map<number, KillswitchHistoryEvent>();
    for (const event of detail.history) {
      if (!eventsByVersion.has(event.version)) {
        eventsByVersion.set(event.version, event);
      }
    }
    return {
      history: [...detail.history].sort((a, b) => b.sequence - a.sequence),
      eventsByVersion,
    };
  }, [detail.history]);
  return (
    <section className="space-y-3">
      <h2 className="text-lg font-semibold">Version history</h2>
      {history.length === 0 ? (
        <div className="border border-dashed p-4 text-sm">
          No version history is available.
        </div>
      ) : (
        <ol className="divide-y border">
          {history.map((event) => {
            const previous = eventsByVersion.get(event.version - 1);
            return (
              <HistoryRow
                key={`${event.sequence}-${event.version}`}
                event={event}
                previous={previous}
                serverNames={serverNames}
              />
            );
          })}
        </ol>
      )}
      {detail.historyTruncated && (
        <Alert variant="warning">
          <AlertTitle>History is incomplete</AlertTitle>
          <AlertDescription>
            The management API reported additional history that it did not
            return.
          </AlertDescription>
        </Alert>
      )}
    </section>
  );
});

function HistoryRow({
  event,
  previous,
  serverNames,
}: {
  event: KillswitchHistoryEvent;
  previous?: KillswitchHistoryEvent;
  serverNames: ReadonlyMap<string, string>;
}): JSX.Element {
  const diff =
    previous && event.action !== "expired"
      ? serverDiff(previous.scope, event.scope)
      : null;
  const scopeTransition =
    previous && previous.scope.type !== event.scope.type
      ? previous.scope.type === "all_servers"
        ? "Scope changed from all MCP servers to selected servers; future servers are no longer covered automatically."
        : "Scope changed from selected servers to all MCP servers, including future servers."
      : undefined;
  return (
    <li className="space-y-2 p-4 text-sm">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="font-medium">
          Version {event.version} · {capitalize(event.action)}
        </div>
        <time
          className="text-muted-foreground"
          dateTime={event.changedAt.toISOString()}
        >
          {event.changedAt.toLocaleString()}
        </time>
      </div>
      <div className="text-muted-foreground">
        <IdentityLink
          identifier={event.actorUserId ? { userId: event.actorUserId } : null}
        >
          {event.actorDisplayName ??
            (event.actorUserId ? "Deleted actor" : "System")}
        </IdentityLink>{" "}
        · {capitalize(event.status)}
      </div>
      <div>
        {scopeLabel(event.scope, serverNames)} · {scheduleLabel(event.schedule)}
      </div>
      {scopeTransition && (
        <p className="text-xs font-medium">{scopeTransition}</p>
      )}
      {diff && (
        <div className="grid gap-1 text-xs sm:grid-cols-3">
          <div>Added: {formatDiff(diff.added, serverNames)}</div>
          <div>Unchanged: {formatDiff(diff.unchanged, serverNames)}</div>
          <div>Removed: {formatDiff(diff.removed, serverNames)}</div>
        </div>
      )}
      <details>
        <summary className="cursor-pointer text-muted-foreground">
          Notes at this version
        </summary>
        <dl className="mt-2 grid gap-2 sm:grid-cols-2">
          <div className="border p-2">
            <dt className="text-xs font-medium">
              Public member-facing message
            </dt>
            <dd className="mt-1 whitespace-pre-wrap break-words">
              {event.externalNote}
            </dd>
          </div>
          <div className="border p-2">
            <dt className="text-xs font-medium">Internal note</dt>
            <dd className="mt-1 whitespace-pre-wrap break-words">
              {event.internalNote}
            </dd>
          </div>
        </dl>
      </details>
    </li>
  );
}

function formatDiff(ids: string[], names: ReadonlyMap<string, string>): string {
  return ids.length === 0
    ? "None"
    : ids.map((id) => names.get(id) ?? "Deleted MCP server").join(", ");
}
