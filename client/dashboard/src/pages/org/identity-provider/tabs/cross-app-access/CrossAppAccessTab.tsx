import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Link } from "react-router";
import { toast } from "sonner";

import { ApiErrorAlert } from "@/components/api-error-alert";
import { SettingsSection } from "@/components/detail/settings-section";
import { StatRow } from "@/components/stat-row";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { VendorEmaTrustNotice } from "@/components/vendor-ema-trust-notice";
import { useRowSelection } from "@/hooks/useRowSelection";
import { pluralize } from "@/lib/format";
import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";
import type { OktaResourceConnectionServer } from "@gram/client/models/components/oktaresourceconnectionserver.js";
import { useIdentityProviderConnectionApplications } from "@gram/client/react-query/identityProviderConnectionApplications.js";
import { useOktaResourceConnections } from "@gram/client/react-query/oktaResourceConnections.js";

import { ClearConfirmationDialog } from "./ClearConfirmationDialog";
import { ConnectionChecklist } from "../setup/ConnectionChecklist";
import { STEP_AFFORDANCES } from "../setup/checklistAffordances";
import { ClearedConfirmationNotice } from "./ClearedConfirmationNotice";
import { ConnectionGate } from "../../ConnectionGate";
import { SESSION_SECURITY } from "../../identityProviderQueries";
import { isConnected } from "../../connectionView";
import { OktaLinkButton } from "./OktaLinkButton";
import {
  normalizeOktaOrgUrl,
  oktaApplicationsUrl,
  oktaConnectionsUrl,
  oktaConsoleUrl,
} from "../../oktaConsoleLinks";
import {
  AGENT_SECTION_ID,
  oktaViewHref,
  READINESS_SECTION_ID,
} from "../../tabs";
import {
  sharedConfirmationKey,
  useClearedConfirmations,
} from "./useClearedConfirmations";
import { useXaaConfirm } from "./useXaaConfirm";
import { XaaBulkConfirmBar } from "./XaaBulkConfirmBar";
import type { XaaConfirmValues } from "./XaaConfirmFields";
import { XaaReadinessTable } from "./XaaReadinessTable";
import { XaaReviewPanel } from "./XaaReviewPanel";
import {
  appInstanceOptions,
  hasConfirmation,
  isConfirmable,
  type AppInstanceOption,
  normalizeAudience,
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

function ReadinessSection({
  action,
  children,
}: {
  action?: ReactNode;
  children: ReactNode;
}): JSX.Element {
  return (
    <SettingsSection id={READINESS_SECTION_ID}>
      <div className="flex flex-wrap items-start justify-between gap-4">
        <SettingsSection.Header className="min-w-0 flex-1 basis-80">
          <SettingsSection.Title>Server connections</SettingsSection.Title>
          <SettingsSection.Description>
            Check each server’s connection in Okta, then record your
            confirmation here. A confirmation is your own record: saving one
            does not create or verify anything in Okta.
          </SettingsSection.Description>
        </SettingsSection.Header>
        {action && <div className="shrink-0">{action}</div>}
      </div>
      <div className="flex min-w-0 flex-col gap-6">{children}</div>
    </SettingsSection>
  );
}

function pendingDescription(data: {
  pendingCount: number;
  agentRecorded: boolean;
}): string {
  if (data.pendingCount === 0) return "All confirmations are saved";
  if (!data.agentRecorded) return "Save the AI agent details first";
  return "Not confirmed or not working. The connection may already exist in Okta.";
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
    // A sibling's cleared confirmation may hold a different audience to undo to.
    const siblingCleared = cleared.snapshots.some(
      (snapshot) =>
        sharedConfirmationKey(snapshot) === sharedConfirmationKey(row),
    );
    const suggested = siblingCleared
      ? undefined
      : normalizeAudience(row.authorizationServerIssuer ?? "");
    resetInteraction();
    setReviewTarget({
      row,
      initialValues: {
        audience: saved.audience ?? suggested,
        oktaApplicationId: saved.oktaApplicationId,
      },
    });
  };

  if (readiness.isPending) {
    return (
      <ReadinessSection>
        <SkeletonTable />
      </ReadinessSection>
    );
  }
  if (readiness.data === undefined) {
    return (
      <ReadinessSection>
        <ApiErrorAlert error={readiness.error} />
      </ReadinessSection>
    );
  }

  const data = readiness.data;
  const applicationsUrl = oktaApplicationsUrl(connection.orgUrl);
  const reviewDeepLink = reviewTarget?.row.deepLink ?? data.deepLink;

  return (
    <ReadinessSection action={<OpenOktaButton deepLink={data.deepLink} />}>
      {readiness.isError && <ApiErrorAlert error={readiness.error} />}

      {!data.agentRecorded && (
        <Alert variant="warning" alignTop>
          <Text variant="small">
            Okta links and confirmations need the AI agent&apos;s ID.{" "}
            <Link
              to={oktaViewHref("cross-app-access", AGENT_SECTION_ID)}
              className="underline underline-offset-2"
            >
              Record it in the setup steps above
            </Link>
            .
          </Text>
        </Alert>
      )}

      <VendorEmaTrustNotice
        issuerUrl={normalizeOktaOrgUrl(connection.orgUrl)}
      />

      <StatRow
        metrics={[
          {
            label: "Not confirmed",
            value: data.pendingCount,
            tone: data.pendingCount > 0 ? "warning" : "success",
            description: pendingDescription(data),
            size: "sm",
          },
          {
            label: "Eligible servers",
            value: data.totalCount,
            tone: "information",
            description: "Use their own authorization server",
            size: "sm",
          },
          {
            label: "Waiting for server details",
            value: data.undiscoveredCount,
            tone: data.undiscoveredCount > 0 ? "warning" : "neutral",
            description: "Sign-in settings not checked yet",
            size: "sm",
          },
        ]}
      />

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
              openReview({
                ...snapshot,
                state: "needs_connection",
                confirmedAt: undefined,
              })
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
              confirmed={hasConfirmation(reviewTarget.row)}
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
    </ReadinessSection>
  );
}

/** The Enterprise Managed Auth phase of the Okta checklist: register the agent, then connect it to each server. */
function EnterpriseManagedAuthSetup({
  connection,
}: {
  connection: OktaIdentityProviderConnection;
}): JSX.Element | null {
  // Older fixtures and partially loaded connections carry no checklist;
  // there is nothing to show until it arrives.
  if (!isConnected(connection) || !connection.checklist?.length) {
    return null;
  }
  return (
    <SettingsSection id="enterprise-managed-auth">
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <ConnectionChecklist
            connection={connection}
            affordances={STEP_AFFORDANCES}
            groups={["cross_app_access"]}
          />
        </SettingsSection.Body>
      </SettingsSection.Panel>
    </SettingsSection>
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
      <div className="flex min-w-0 flex-col gap-10">
        <EnterpriseManagedAuthSetup connection={connection} />
        <ReadinessChecklist connection={connection} />
      </div>
    </ConnectionGate>
  );
}
