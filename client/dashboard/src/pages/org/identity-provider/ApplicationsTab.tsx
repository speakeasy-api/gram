import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ChevronDown, ChevronUp } from "lucide-react";

import { ApiErrorAlert } from "@/components/api-error-alert";
import { StatRow } from "@/components/stat-row";
import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Label } from "@/components/ui/Label";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Switch } from "@/components/ui/Switch";
import { Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { HumanizeDateTime } from "@/lib/dates";
import type { IdentityProviderConnectionApplication } from "@gram/client/models/components/identityproviderconnectionapplication.js";
import type { IdentityProviderConnectionApplicationsSync } from "@gram/client/models/components/identityproviderconnectionapplicationssync.js";
import type { IdentityProviderConnectionReconcileRun } from "@gram/client/models/components/identityproviderconnectionreconcilerun.js";
import type { ListIdentityProviderConnectionApplicationsResult } from "@gram/client/models/components/listidentityproviderconnectionapplicationsresult.js";
import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";
import { useIdentityProviderConnectionApplications } from "@gram/client/react-query/identityProviderConnectionApplications.js";
import { useSyncIdentityProviderConnectionApplicationsMutation } from "@gram/client/react-query/syncIdentityProviderConnectionApplications.js";

import { ConnectionGate } from "./ConnectionGate";
import {
  applicationStatusVariant,
  humanizeOktaToken,
  isConnectionVerified,
  reconcileErrorLabel,
  reconcileStatusLabel,
  reconcileStatusVariant,
} from "./connectionView";
import {
  inlineError,
  invalidateIdentityProviderQueries,
  SESSION_SECURITY,
} from "./identityProviderQueries";
import type { ConnectionTabProps } from "./IdentityProviderArea";

type Application = IdentityProviderConnectionApplication;

const SNAPSHOT_CAP = 2000;
const PREVIEW_ROWS = 7;
const SYNC_POLL_MS = 15_000;

const columns: Column<Application>[] = [
  {
    key: "label",
    header: "Label",
    width: "2fr",
    render: (app) => (
      <div className="flex min-w-0 flex-col">
        <Text className="truncate font-medium">{app.label}</Text>
        <Text muted small className="truncate font-mono">
          {app.oktaAppId}
        </Text>
      </div>
    ),
  },
  {
    key: "name",
    header: "Name",
    width: "1fr",
    render: (app) => (
      <Text small className="truncate font-mono">
        {app.name}
      </Text>
    ),
  },
  {
    key: "signOnMode",
    header: "Sign-on",
    width: "1fr",
    render: (app) => (
      <Text small className="whitespace-normal">
        {humanizeOktaToken(app.signOnMode)}
      </Text>
    ),
  },
  {
    key: "status",
    header: "Status",
    width: "110px",
    render: (app) =>
      app.removedAt ? (
        <div className="flex min-w-0 flex-col gap-1">
          <Badge variant="destructive" size="sm">
            Removed
          </Badge>
          <Text muted small>
            <HumanizeDateTime date={app.removedAt} includeTime={false} />
          </Text>
        </div>
      ) : (
        <Badge variant={applicationStatusVariant(app.status)} size="sm">
          {humanizeOktaToken(app.status)}
        </Badge>
      ),
  },
  {
    key: "userAssignments",
    header: "Users",
    width: "80px",
    render: (app) => (
      <Text className="tabular-nums">{app.userAssignments}</Text>
    ),
  },
  {
    key: "groupAssignments",
    header: "Groups",
    width: "80px",
    render: (app) => (
      <Text className="tabular-nums">{app.groupAssignments}</Text>
    ),
  },
  {
    key: "firstSeenAt",
    header: "First seen",
    width: "140px",
    render: (app) => (
      <Text small muted>
        <HumanizeDateTime date={app.firstSeenAt} includeTime={false} />
      </Text>
    ),
  },
  {
    key: "lastSeenAt",
    header: "Last seen",
    width: "140px",
    render: (app) => (
      <Text small muted>
        <HumanizeDateTime date={app.lastSeenAt} includeTime={false} />
      </Text>
    ),
  },
];

function syncRequestPending(
  sync: IdentityProviderConnectionApplicationsSync,
): boolean {
  return (
    sync.requestedAt != null &&
    (sync.syncedAt == null || sync.requestedAt > sync.syncedAt)
  );
}

function syncInFlight(
  data: ListIdentityProviderConnectionApplicationsResult | undefined,
): boolean {
  if (!data) return false;
  return data.lastRun?.status === "running" || syncRequestPending(data.sync);
}

function CadenceLine({
  sync,
}: {
  sync: IdentityProviderConnectionApplicationsSync;
}): JSX.Element {
  return (
    <Text muted small>
      Updates automatically on a schedule.{" "}
      {sync.syncedAt ? (
        <>
          Last update started <HumanizeDateTime date={sync.syncedAt} />.
        </>
      ) : (
        "No update has completed yet."
      )}
    </Text>
  );
}

function LastRunSummary({
  run,
}: {
  run: IdentityProviderConnectionReconcileRun;
}): JSX.Element {
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-3">
        <span className="text-eyebrow">Last update</span>
        <Badge variant={reconcileStatusVariant(run.status)} size="sm">
          {reconcileStatusLabel(run.status)}
        </Badge>
        <Text muted small>
          started <HumanizeDateTime date={run.startedAt} />
          {run.finishedAt && (
            <>
              , finished <HumanizeDateTime date={run.finishedAt} />
            </>
          )}
        </Text>
      </div>
      <StatRow
        metrics={[
          {
            label: "Apps found",
            value: run.applicationsSeen,
            tone: "information",
          },
          {
            label: "Apps added",
            value: run.applicationsAdded,
            tone: "neutral",
          },
          {
            label: "Apps removed",
            value: run.applicationsRemoved,
            tone: run.applicationsRemoved > 0 ? "warning" : "neutral",
          },
          {
            label: "Assignments added",
            value: run.assignmentsAdded,
            delta:
              run.assignmentsRemoved > 0
                ? `-${run.assignmentsRemoved}`
                : undefined,
            description:
              run.assignmentsRemoved > 0
                ? `${run.assignmentsRemoved} removed`
                : "None removed",
            tone: "neutral",
          },
        ]}
      />
      {run.error && (
        <Alert variant="error">
          <Text variant="small">{reconcileErrorLabel(run.error)}</Text>
        </Alert>
      )}
      {run.truncated && (
        <Alert variant="warning">
          <Text variant="small">
            This update reached the limit on how many applications we can
            retrieve, so the list may be incomplete. Apps missing from this
            update will not be marked removed.
          </Text>
        </Alert>
      )}
      {run.skippedAppIds.length > 0 && (
        <Text muted small>
          Skipped {run.skippedAppIds.length} Okta-internal app
          {run.skippedAppIds.length === 1 ? "" : "s"}:{" "}
          <span className="font-mono">{run.skippedAppIds.join(", ")}</span>
        </Text>
      )}
    </div>
  );
}

function ApplicationsSnapshot({
  connection,
}: {
  connection: OktaIdentityProviderConnection;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [includeRemoved, setIncludeRemoved] = useState(false);
  const [expanded, setExpanded] = useState(false);
  const applications = useIdentityProviderConnectionApplications(
    { id: connection.id, includeRemoved },
    SESSION_SECURITY,
    {
      throwOnError: false,
      retry: false,
      refetchInterval: (query) =>
        syncInFlight(query.state.data) ? SYNC_POLL_MS : false,
    },
  );
  const sync = useSyncIdentityProviderConnectionApplicationsMutation({
    onSuccess: () => {
      toast.success("Update requested. It will start shortly.");
      void invalidateIdentityProviderQueries(queryClient);
    },
    onError: inlineError,
  });

  if (applications.isPending) return <SkeletonTable />;
  if (applications.data === undefined) {
    return <ApiErrorAlert error={applications.error} />;
  }

  const { applications: rows, sync: cadence, lastRun } = applications.data;
  const capped = rows.length >= SNAPSHOT_CAP;
  const requestPending = syncRequestPending(cadence);
  const running = lastRun?.status === "running";
  const visibleRows = expanded ? rows : rows.slice(0, PREVIEW_ROWS);

  return (
    <div className="flex min-w-0 flex-col gap-8">
      <div className="flex min-w-0 flex-col gap-3">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="flex flex-col gap-1">
            <span className="text-eyebrow">Application updates</span>
            <CadenceLine sync={cadence} />
          </div>
          <div className="flex flex-col items-end gap-1">
            <Button
              variant="secondary"
              size="sm"
              disabled={sync.isPending || requestPending || running}
              onClick={() =>
                sync.mutate({
                  security: SESSION_SECURITY,
                  request: {
                    syncIdentityProviderConnectionApplicationsRequestBody: {
                      id: connection.id,
                    },
                  },
                })
              }
            >
              {sync.isPending
                ? "Requesting..."
                : running
                  ? "Updating..."
                  : requestPending
                    ? "Update requested"
                    : "Update now"}
            </Button>
            {requestPending && cadence.requestedAt && (
              <Text muted small>
                Requested <HumanizeDateTime date={cadence.requestedAt} />;
                waiting to start
              </Text>
            )}
          </div>
        </div>
        <ApiErrorAlert error={sync.error} />
        {applications.isError && <ApiErrorAlert error={applications.error} />}
      </div>

      {lastRun ? (
        <LastRunSummary run={lastRun} />
      ) : (
        <Text muted small>
          Applications will appear shortly after the connection is verified.
        </Text>
      )}

      <div className="flex min-w-0 flex-col gap-3">
        <div className="flex flex-wrap items-center justify-between gap-4">
          <span className="text-eyebrow">
            Applications · {rows.length}
            {capped ? ` (showing up to ${SNAPSHOT_CAP})` : ""}
          </span>
          <Label className="gap-2">
            <Switch
              checked={includeRemoved}
              onCheckedChange={setIncludeRemoved}
              aria-labelledby="okta-include-removed-label"
            />
            <span
              id="okta-include-removed-label"
              className="text-sm font-normal"
            >
              Include removed
            </span>
          </Label>
        </div>
        <div
          role="region"
          id="identity-provider-applications-table"
          aria-label="Identity provider applications"
          tabIndex={0}
          className="min-w-0 overflow-x-auto focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset"
        >
          <div className={rows.length > 0 ? "min-w-[880px]" : undefined}>
            <Table
              columns={columns}
              data={visibleRows}
              rowKey={(app) => app.oktaAppId}
              noResultsMessage={
                <Text muted>
                  {running || requestPending
                    ? "The application list is updating. Applications will appear here when it finishes."
                    : lastRun?.status === "failed"
                      ? "The last update failed. Review the error above, then request another update."
                      : "No applications listed yet. Update now or wait for the next update."}
                </Text>
              }
            />
          </div>
        </div>
        {rows.length > PREVIEW_ROWS && (
          <div className="flex justify-center">
            <Button
              variant="secondary"
              size="sm"
              className="h-10"
              aria-expanded={expanded}
              aria-controls="identity-provider-applications-table"
              onClick={() => setExpanded((previous) => !previous)}
            >
              <Button.LeftIcon aria-hidden="true">
                {expanded ? (
                  <ChevronUp className="h-4 w-4" />
                ) : (
                  <ChevronDown className="h-4 w-4" />
                )}
              </Button.LeftIcon>
              {expanded
                ? "Show fewer applications"
                : `Show all ${rows.length} applications`}
            </Button>
          </div>
        )}
      </div>
    </div>
  );
}

export function ApplicationsTab({
  connection,
  rolloutEnabled,
}: ConnectionTabProps): JSX.Element {
  const gate = (
    <ConnectionGate
      connection={connection}
      rolloutEnabled={rolloutEnabled}
      icon="layout-grid"
      purpose="to view your Okta applications"
      requires="verified"
    />
  );
  if (!connection || !isConnectionVerified(connection)) return gate;
  return <ApplicationsSnapshot connection={connection} />;
}
