import { useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";

import { ApiErrorAlert } from "@/components/api-error-alert";
import { StatRow } from "@/components/stat-row";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useRowSelection } from "@/hooks/useRowSelection";
import { pluralize } from "@/lib/format";
import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";
import type { OktaResourceConnectionServer } from "@gram/client/models/components/oktaresourceconnectionserver.js";
import { useIdentityProviderConnectionApplications } from "@gram/client/react-query/identityProviderConnectionApplications.js";
import { useOktaResourceConnections } from "@gram/client/react-query/oktaResourceConnections.js";

import { ClearConfirmationDialog } from "./ClearConfirmationDialog";
import { ClearedConfirmationNotice } from "./ClearedConfirmationNotice";
import { ConnectionGate } from "../../ConnectionGate";
import { SESSION_SECURITY } from "../../identityProviderQueries";
import { OktaLinkButton } from "./OktaLinkButton";
import {
  oktaApplicationsUrl,
  oktaConnectionsUrl,
  oktaConsoleUrl,
} from "../../oktaConsoleLinks";
import { AGENT_SECTION_ID, oktaViewHref } from "../../tabs";
import { useClearedConfirmations } from "./useClearedConfirmations";
import { useXaaConfirm } from "./useXaaConfirm";
import { XaaBulkConfirmBar } from "./XaaBulkConfirmBar";
import { XaaExportButtons } from "./XaaExportButtons";
import type { XaaConfirmValues } from "./XaaConfirmFields";
import { XaaReadinessTable } from "./XaaReadinessTable";
import { XaaReviewPanel } from "./XaaReviewPanel";
import {
  appInstanceOptions,
  isConfirmable,
  type AppInstanceOption,
} from "./xaaView";

type ReadinessFilter = "pending" | "all";

function serverId(row: OktaResourceConnectionServer): string {
  return row.mcpServerId;
}

function OpenOktaButton({
  deepLink,
}: {
  deepLink: string | undefined;
}): JSX.Element {
  const href = oktaConnectionsUrl(deepLink);
  if (href) {
    return (
      <OktaLinkButton href={href} variant="primary">
        Open Okta
      </OktaLinkButton>
    );
  }
  return (
    <Button
      variant="primary"
      size="sm"
      disabled
      tooltip="Record the AI agent first"
    >
      Open Okta
    </Button>
  );
}

function useAppInstances(connectionId: string): AppInstanceOption[] {
  const applications = useIdentityProviderConnectionApplications(
    { id: connectionId, includeRemoved: false },
    SESSION_SECURITY,
    { throwOnError: false, retry: false },
  );
  return useMemo(
    () => appInstanceOptions(applications.data?.applications ?? []),
    [applications.data],
  );
}

function ReadinessChecklist({
  connection,
}: {
  connection: OktaIdentityProviderConnection;
}): JSX.Element {
  const [filter, setFilter] = useState<ReadinessFilter>("pending");
  const includeAll = filter === "all";
  const readiness = useOktaResourceConnections(
    { includeAll },
    SESSION_SECURITY,
    {
      throwOnError: false,
      retry: false,
      placeholderData: (previous) => previous,
    },
  );
  const rows = useMemo(() => readiness.data?.servers ?? [], [readiness.data]);
  const confirmable = useMemo(() => rows.filter(isConfirmable), [rows]);
  const selection = useRowSelection(confirmable, serverId);
  const appInstances = useAppInstances(connection.id);
  const cleared = useClearedConfirmations(
    {
      servers: readiness.data?.servers,
      isPlaceholderData: readiness.isPlaceholderData,
    },
    appInstances,
  );
  const { confirming, feedback, clearFeedback, confirmRows } = useXaaConfirm(
    cleared.retire,
  );
  const [clearTarget, setClearTarget] =
    useState<OktaResourceConnectionServer | null>(null);
  const [reviewTarget, setReviewTarget] = useState<{
    row: OktaResourceConnectionServer;
    initialValues: XaaConfirmValues;
  } | null>(null);
  const reviewRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (reviewTarget) {
      reviewRef.current?.scrollIntoView({
        block: "nearest",
        behavior: "smooth",
      });
      reviewRef.current?.focus({ preventScroll: true });
    }
  }, [reviewTarget]);

  const locked = confirming || readiness.isPlaceholderData;

  const resetInteraction = () => {
    clearFeedback();
    selection.clear();
    setReviewTarget(null);
  };

  const confirmSelected = async (
    audience: string,
    oktaApplicationId: string | undefined,
  ) => {
    if (locked) return;
    const selected = selection.selectedItems;
    const outcome = await confirmRows(selected, audience, oktaApplicationId);
    if (!outcome) return;
    if (outcome.ok) {
      toast.success(`${pluralize(outcome.count, "server")} confirmed`);
      selection.clear();
      return;
    }
    // Keep only the unsuccessful batches selected for a safe retry.
    for (const row of selected) {
      if (outcome.confirmedIds.has(row.mcpServerId)) {
        selection.toggle(row.mcpServerId);
      }
    }
  };

  const confirmOne = async (
    row: OktaResourceConnectionServer,
    audience: string,
    oktaApplicationId: string | undefined,
  ) => {
    if (locked) return;
    const outcome = await confirmRows([row], audience, oktaApplicationId);
    if (!outcome?.ok) return;
    toast.success("Confirmation saved. Nothing was changed in Okta.");
    selection.clear();
    setReviewTarget(null);
  };

  const openReview = (row: OktaResourceConnectionServer) => {
    if (locked) return;
    const saved = cleared.snapshotFor(row.mcpServerId) ?? row;
    resetInteraction();
    setReviewTarget({
      row,
      initialValues: {
        audience: saved.audience,
        oktaApplicationId: saved.oktaApplicationId,
      },
    });
  };

  if (readiness.isPending) return <SkeletonTable />;
  if (readiness.data === undefined) {
    return <ApiErrorAlert error={readiness.error} />;
  }

  const data = readiness.data;
  const applicationsUrl = oktaApplicationsUrl(connection.orgUrl);
  const reviewDeepLink = reviewTarget?.row.deepLink ?? data.deepLink;

  return (
    <div className="flex min-w-0 flex-col gap-6">
      {readiness.isError && <ApiErrorAlert error={readiness.error} />}
      <div className="flex flex-wrap items-start justify-between gap-4">
        <Text className="max-w-2xl">
          Review each server’s Cross App Access setup in Okta, then record its
          confirmation here. Not confirmed means Speakeasy has no saved
          confirmation; the connection may already exist in Okta.
        </Text>
        <div className="flex flex-wrap items-center gap-2">
          <XaaExportButtons includeAll={includeAll} />
          <OpenOktaButton deepLink={data.deepLink} />
        </div>
      </div>

      <StatRow
        metrics={[
          {
            label: "Not confirmed",
            value: data.pendingCount,
            tone: data.pendingCount > 0 ? "warning" : "success",
            description:
              data.pendingCount === 0
                ? "All confirmations are saved"
                : data.agentRecorded
                  ? "Review existing setup before creating anything in Okta"
                  : "Save the AI agent details before reviewing setup",
          },
          {
            label: "Eligible servers",
            value: data.totalCount,
            tone: "information",
            description: "Servers with their own authorization server",
          },
          {
            label: "Waiting for server details",
            value: data.undiscoveredCount,
            tone: data.undiscoveredCount > 0 ? "warning" : "neutral",
            description:
              "Speakeasy has not checked this server’s sign-in settings yet",
          },
        ]}
      />

      {!data.agentRecorded && (
        <Alert variant="warning" alignTop>
          <Text variant="small">
            Save the Okta AI agent details first. The Okta links and connections
            below need the agent&apos;s ID.{" "}
            <Link
              to={oktaViewHref("setup", AGENT_SECTION_ID)}
              className="underline underline-offset-2"
            >
              Record it on the Okta Setup tab
            </Link>
            .
          </Text>
        </Alert>
      )}

      <Alert variant="info" alignTop>
        <Text variant="small">
          These confirmations record settings you reviewed, not a live check of
          Okta configuration or access. Saving a confirmation does not create or
          verify the Okta connection. Some servers share one confirmation.
          Editing or clearing it updates all servers that share it.
        </Text>
      </Alert>

      <div className="flex min-w-0 flex-col gap-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <SegmentedControl<ReadinessFilter>
            value={filter}
            disabled={confirming}
            onChange={(next) => {
              setFilter(next);
              resetInteraction();
            }}
            options={[
              { value: "pending", label: "Needs action" },
              { value: "all", label: "All" },
            ]}
          />
          <Text muted small>
            {rows.length} of {pluralize(data.totalCount, "server")}
          </Text>
        </div>

        {feedback.kind === "confirmed" && feedback.count > 0 && (
          <Alert variant="success" alignTop>
            <div className="flex flex-wrap items-center justify-between gap-3">
              <Text variant="small">
                {pluralize(feedback.count, "server")} confirmed
                {includeAll
                  ? "."
                  : " and moved out of Needs action. Use All to review or edit the saved settings."}
              </Text>
              {!includeAll && (
                <Button
                  variant="tertiary"
                  size="xs"
                  onClick={() => {
                    setFilter("all");
                    resetInteraction();
                  }}
                >
                  Show all servers
                </Button>
              )}
            </div>
          </Alert>
        )}

        {cleared.snapshots.map((snapshot) => (
          <ClearedConfirmationNotice
            key={snapshot.mcpServerId}
            snapshot={snapshot}
            canUndo={cleared.canUndo(snapshot)}
            locked={locked}
            onUndo={() => {
              if (snapshot.audience && cleared.canUndo(snapshot))
                void confirmOne(
                  snapshot,
                  snapshot.audience,
                  snapshot.oktaApplicationId,
                );
            }}
            onReview={() =>
              openReview({ ...snapshot, state: "needs_connection" })
            }
          />
        ))}

        {readiness.isPlaceholderData && (
          <Text muted small role="status">
            Loading servers...
          </Text>
        )}
        {feedback.kind === "failed" && (
          <>
            {feedback.confirmedCount > 0 && (
              <Alert variant="warning">
                <Text variant="small">
                  {pluralize(feedback.confirmedCount, "server")} confirmed
                  before the request failed. Completed servers have been
                  deselected. Review the remaining selection and retry.
                </Text>
              </Alert>
            )}
            <ApiErrorAlert error={feedback.error} />
          </>
        )}
        {!reviewTarget && selection.selectedCount > 0 && (
          <XaaBulkConfirmBar
            selectedCount={selection.selectedCount}
            applications={appInstances}
            applicationsUrl={applicationsUrl}
            pending={locked}
            onConfirm={(audience, appId) =>
              void confirmSelected(audience, appId)
            }
          />
        )}

        {reviewTarget && (
          <div ref={reviewRef} tabIndex={-1} className="outline-none">
            <XaaReviewPanel
              key={`${reviewTarget.row.mcpServerId}:${reviewTarget.row.state}`}
              serverName={reviewTarget.row.serverName}
              confirmed={reviewTarget.row.state === "connected"}
              connectionsUrl={oktaConnectionsUrl(reviewDeepLink)}
              createUrl={oktaConsoleUrl(reviewDeepLink)}
              applications={appInstances}
              applicationsUrl={applicationsUrl}
              initialValues={reviewTarget.initialValues}
              pending={locked}
              onCancel={resetInteraction}
              onConfirm={(audience, appId) =>
                void confirmOne(reviewTarget.row, audience, appId)
              }
            />
          </div>
        )}

        <XaaReadinessTable
          rows={rows}
          selection={selection}
          locked={locked}
          fallbackDeepLink={data.deepLink}
          emptyMessage={
            includeAll
              ? "No MCP servers use their own authorization server yet."
              : "Nothing needs action. Switch to All to see confirmed and not-applicable servers."
          }
          onSelectionChange={() => setReviewTarget(null)}
          onReview={openReview}
          onClear={setClearTarget}
        />
      </div>

      <ClearConfirmationDialog
        target={clearTarget}
        canUndo={clearTarget != null && cleared.canUndo(clearTarget)}
        onCleared={(row) => {
          cleared.remember(row);
          resetInteraction();
        }}
        onClose={() => setClearTarget(null)}
      />
    </div>
  );
}

export function CrossAppAccessTab({
  connection,
}: {
  connection: OktaIdentityProviderConnection;
}): JSX.Element {
  return (
    <ConnectionGate
      connection={connection}
      requires="checked"
      icon="route"
      purpose="to set up Cross App Access"
    >
      <ReadinessChecklist connection={connection} />
    </ConnectionGate>
  );
}
