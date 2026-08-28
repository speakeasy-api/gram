import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { useSession } from "@/contexts/Auth";
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
import { useEffect, useMemo, useRef, useState } from "react";
import { KillswitchEditorSheet } from "./KillswitchEditorSheet";
import { LiftKillswitchDialog } from "./LiftKillswitchDialog";
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

const EMPTY: never[] = [];

function isTerminalStatus(status: KillswitchDetailModel["status"]): boolean {
  return status === "expired" || status === "lifted";
}

export default function KillswitchDetail(): JSX.Element {
  const { killswitchId = "" } = useParams();
  const session = useSession();
  const security = { sessionHeaderGramSession: session.session };
  const routes = useOrgRoutes();
  const queryClient = useQueryClient();
  const [editOpen, setEditOpen] = useState(false);
  const [liftOpen, setLiftOpen] = useState(false);
  const detailQuery = useKillswitch(security, { id: killswitchId });
  const membersQuery = useMembers(undefined, security);
  const capabilitiesQuery = useKillswitchCapabilities(security, undefined, {
    enabled: editOpen,
  });
  const serversQuery = useKillswitchMCPServers(security);
  const editMutation = useEditKillswitchMutation();
  const liftMutation = useLiftKillswitchMutation();
  const overlapMutation = usePreviewKillswitchOverlapsMutation();
  const [overlaps, setOverlaps] = useState<KillswitchOverlap[]>([]);
  const [overlapsTruncated, setOverlapsTruncated] = useState(false);
  const [overlapError, setOverlapError] = useState<string>();
  const [overlapPreviewStatus, setOverlapPreviewStatus] = useState<
    "loading" | "ready" | "error"
  >("loading");
  const previewedVersion = useRef<number | undefined>(undefined);
  const refetchDetail = detailQuery.refetch;

  const detail = detailQuery.data;
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
  const catalogLoading = membersQuery.isLoading || serversQuery.isLoading;

  useEffect(() => {
    if (!detail || !isTerminalStatus(detail.status)) return;
    setEditOpen(false);
    setLiftOpen(false);
  }, [detail]);

  const requireChangeable = (): KillswitchDetailModel => {
    if (!detail || isTerminalStatus(detail.status)) {
      setEditOpen(false);
      setLiftOpen(false);
      throw new Error("This Killswitch can no longer be changed.");
    }
    return detail;
  };

  const loadOverlaps = async (current: KillswitchDetailModel) => {
    setOverlapPreviewStatus("loading");
    setOverlapError(undefined);
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
      setOverlaps(result.overlaps);
      setOverlapsTruncated(result.truncated);
      setOverlapPreviewStatus("ready");
    } catch (error) {
      setOverlapError(
        error instanceof Error ? error.message : "Unable to load overlaps.",
      );
      setOverlapPreviewStatus("error");
      throw error;
    }
  };

  useEffect(() => {
    if (!detail || previewedVersion.current === detail.version) return;
    previewedVersion.current = detail.version;
    void loadOverlaps(detail).catch(() => undefined);
    // The generated preview endpoint is a POST mutation, so the detail page
    // triggers it once per API version rather than issuing one request per row.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [detail?.id, detail?.version]);

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

  const invalidate = () =>
    Promise.all([
      invalidateAllKillswitches(queryClient),
      invalidateKillswitch(queryClient, [{ id: killswitchId }]),
    ]);

  const edit = async (
    draft: EditorDraft,
    operationId: string,
    expectedVersion?: number,
  ) => {
    const current = requireChangeable();
    if (expectedVersion == null)
      throw new Error("The current version is unavailable.");
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
    await invalidate();
    return receipt;
  };

  const refreshConflict = async (): Promise<KillswitchDetailModel> => {
    const result = await detailQuery.refetch();
    if (!result.data)
      throw new Error("The latest version could not be loaded.");
    if (isTerminalStatus(result.data.status)) {
      setEditOpen(false);
      setLiftOpen(false);
      throw new Error("This Killswitch can no longer be changed.");
    }
    previewedVersion.current = undefined;
    await loadOverlaps(result.data);
    return result.data;
  };

  const lift = async (operationId: string) => {
    const current = requireChangeable();
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
      setOverlaps(result.remainingOverlaps);
      setOverlapsTruncated(result.truncated);
      await invalidate();
    } catch (error) {
      if (conflictName(error) === "version_conflict") {
        await refreshConflict();
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
  if (detailQuery.error || !detail) {
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
  if (catalogError) {
    return (
      <main className="mx-auto max-w-[1100px] space-y-4 p-8">
        <Alert variant="error">
          <AlertTitle>Unable to load Killswitch details</AlertTitle>
          <AlertDescription>{catalogError.message}</AlertDescription>
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

  const canChange = detail.status === "active" || detail.status === "scheduled";
  return (
    <main className="mx-auto w-full max-w-[1100px] space-y-8 px-4 py-6 sm:px-8 sm:py-8">
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
              {memberName ?? "Deleted member"} · Version {detail.version}
            </p>
          </div>
          {canChange && (
            <div className="flex w-full flex-col gap-2 sm:w-auto sm:flex-row">
              <Button variant="secondary" onClick={() => setEditOpen(true)}>
                Edit killswitch
              </Button>
              <Button
                variant="destructive-primary"
                onClick={() => {
                  setLiftOpen(true);
                  void loadOverlaps(detail).catch(() => undefined);
                }}
              >
                Lift killswitch
              </Button>
            </div>
          )}
        </div>
      </header>

      <section className="grid gap-4 border p-5 sm:grid-cols-2">
        <DetailValue label="Member" value={memberName ?? "Deleted member"} />
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
                  onClick={() => void loadOverlaps(detail)}
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

      <KillswitchEditorSheet
        open={editOpen}
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
        onPreview={previewDraft}
        onSubmit={edit}
        onRefreshConflict={refreshConflict}
        onView={() => setEditOpen(false)}
      />
      <LiftKillswitchDialog
        open={liftOpen}
        onOpenChange={setLiftOpen}
        overlaps={overlaps}
        overlapsTruncated={overlapsTruncated}
        serverNames={serverNames}
        previewStatus={overlapPreviewStatus}
        previewError={overlapError}
        onRetryPreview={() => loadOverlaps(detail)}
        onLift={lift}
      />
    </main>
  );
}

function DetailValue({
  label,
  value,
}: {
  label: string;
  value: string;
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

function History({
  detail,
  serverNames,
}: {
  detail: KillswitchDetailModel;
  serverNames: ReadonlyMap<string, string>;
}): JSX.Element {
  const history = [...detail.history].sort((a, b) => b.sequence - a.sequence);
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
            const previous = detail.history.find(
              (candidate) => candidate.version === event.version - 1,
            );
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
}

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
        {event.actorDisplayName ??
          (event.actorUserId ? "Deleted actor" : "System")}{" "}
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
        <div className="mt-2 grid gap-2 sm:grid-cols-2">
          <p className="whitespace-pre-wrap break-words border p-2">
            {event.externalNote}
          </p>
          <p className="whitespace-pre-wrap break-words border p-2">
            {event.internalNote}
          </p>
        </div>
      </details>
    </li>
  );
}

function formatDiff(ids: string[], names: ReadonlyMap<string, string>): string {
  return ids.length === 0
    ? "None"
    : ids.map((id) => names.get(id) ?? "Deleted MCP server").join(", ");
}

function capitalize(value: string): string {
  return value.charAt(0).toUpperCase() + value.slice(1);
}
