import { useEffect, useMemo, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ExternalLink, MoreHorizontal } from "lucide-react";
import { Link } from "react-router";
import { toast } from "sonner";

import { ApiErrorAlert } from "@/components/api-error-alert";
import { StatRow } from "@/components/stat-row";
import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { CopyButton } from "@/components/ui/CopyButton";
import { Dialog } from "@/components/ui/Dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { useRowSelection } from "@/hooks/useRowSelection";
import { describeApiError } from "@/lib/api-error";
import { HumanizeDateTime } from "@/lib/dates";
import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";
import type { XaaServerReadiness } from "@gram/client/models/components/xaaserverreadiness.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useConfirmXaaConnectionsMutation } from "@gram/client/react-query/confirmXaaConnections.js";
import { useIdentityProviderConnectionApplications } from "@gram/client/react-query/identityProviderConnectionApplications.js";
import { useResetXaaConnectionMutation } from "@gram/client/react-query/resetXaaConnection.js";
import { useXaaReadiness } from "@gram/client/react-query/xaaReadiness.js";

import { AGENT_SECTION_ID } from "./OktaConnectionSteps";
import { ConnectionGate } from "./ConnectionGate";
import {
  appInstanceOptions,
  buildConfirmRequests,
  canConfirmReadiness,
  clientBindingNote,
  isConfirmable,
  notApplicableReasonLabel,
  notApplicableReasonSummary,
  pluralize,
  xaaStateLabel,
  xaaStateVariant,
  type AppInstanceOption,
} from "./connectionView";
import {
  inlineError,
  invalidateIdentityProviderQueries,
  identityTabHref,
  SESSION_SECURITY,
} from "./identityProviderQueries";
import type { ConnectionTabProps } from "./IdentityProviderArea";
import { XaaConfirmBar } from "./XaaConfirmBar";
import {
  oktaApplicationsUrl,
  oktaConnectionsUrl,
  oktaConsoleUrl,
} from "./oktaConsoleLinks";
import {
  type ChecklistFormat,
  downloadXaaChecklist,
} from "./xaaChecklistDownload";

type ReadinessRow = XaaServerReadiness;
type ReadinessFilter = "pending" | "all";

function recoveryKey(row: ReadinessRow): string {
  return row.issuerId
    ? JSON.stringify([row.issuerId, row.resourceIndicator])
    : JSON.stringify([row.mcpServerId]);
}

function rowId(row: ReadinessRow): string {
  return row.mcpServerId;
}

function CopyCell({
  value,
  label,
}: {
  value: string | undefined;
  label: string;
}): JSX.Element {
  if (!value) {
    return <span className="text-muted-foreground text-xs leading-6">—</span>;
  }
  return (
    <div className="grid w-full min-w-0 grid-cols-[minmax(0,1fr)_auto] items-center gap-2 has-[button:hover]:bg-btn-secondary-hover has-[button:focus-visible]:bg-btn-secondary-hover">
      <span
        className="min-w-0 truncate font-mono text-xs leading-6"
        title={value}
      >
        {value}
      </span>
      <span className="shrink-0">
        <CopyButton
          text={value}
          size="xs"
          tooltip={`Copy ${label}`}
          className="hover:bg-transparent"
        />
      </span>
    </div>
  );
}

function StateCell({ row }: { row: ReadinessRow }): JSX.Element {
  const notApplicable = row.state === "not_applicable";
  return (
    <div className="flex min-w-0 flex-col items-start gap-2 pt-1">
      <Badge variant={xaaStateVariant(row.state)} size="sm">
        {xaaStateLabel(row.state)}
      </Badge>
      {notApplicable && (
        <SimpleTooltip
          tooltip={notApplicableReasonLabel(row.notApplicableReason)}
        >
          <Text
            muted
            small
            className="line-clamp-3 text-xs leading-5 whitespace-normal"
          >
            {notApplicableReasonSummary(row.notApplicableReason)}
          </Text>
        </SimpleTooltip>
      )}
    </div>
  );
}

function ConnectionCell({ row }: { row: ReadinessRow }): JSX.Element {
  if (row.state !== "connected") {
    return <span className="text-muted-foreground text-xs leading-6">—</span>;
  }
  const app = row.oktaApplicationLabel ?? row.oktaApplicationId;
  return (
    <div className="flex w-full min-w-0 flex-col gap-1">
      <CopyCell value={row.audience} label="Issuer URL" />
      <span
        className="text-muted-foreground truncate text-xs leading-5"
        title={app}
      >
        {app ?? "Application not recorded"}
      </span>
      {row.confirmedAt && (
        <span className="text-muted-foreground text-xs leading-5">
          Confirmed <HumanizeDateTime date={row.confirmedAt} />
        </span>
      )}
    </div>
  );
}

function ConfigurationCell({ row }: { row: ReadinessRow }): JSX.Element {
  const clientNote = clientBindingNote(row.clientBinding);
  return (
    <dl className="grid w-full min-w-0 grid-cols-[4.5rem_minmax(0,1fr)] items-start gap-x-2 gap-y-1">
      <dt
        className="text-muted-foreground text-xs leading-6"
        title="Resource indicator"
      >
        Resource
      </dt>
      <dd className="min-w-0">
        <CopyCell value={row.resourceIndicator} label="resource indicator" />
      </dd>
      <dt className="text-muted-foreground text-xs leading-6">Client ID</dt>
      <dd className="min-w-0">
        {clientNote ? (
          <SimpleTooltip tooltip={clientNote}>
            <span
              className="text-muted-foreground block truncate text-xs leading-6"
              title={clientNote}
            >
              {row.clientBinding === "ambiguous"
                ? "Multiple Client IDs found"
                : "No Client ID found"}
            </span>
          </SimpleTooltip>
        ) : (
          <CopyCell value={row.clientId} label="client ID" />
        )}
      </dd>
      <dt className="text-muted-foreground text-xs leading-6">Scopes</dt>
      <dd className="min-w-0">
        <CopyCell value={row.scopes.join(" ")} label="scopes" />
      </dd>
    </dl>
  );
}

function DeepLinkCell({
  row,
  deepLink,
}: {
  row: ReadinessRow;
  deepLink?: string;
}): JSX.Element | null {
  const href = oktaConnectionsUrl(deepLink);
  if (!href) return null;
  return (
    <Button
      asChild
      variant="tertiary"
      size="xs"
      tooltip="Open Okta connections"
    >
      <a
        href={href}
        target="_blank"
        rel="noopener noreferrer"
        aria-label={`Open Okta connections for ${row.serverName}`}
      >
        <Button.LeftIcon>
          <ExternalLink />
        </Button.LeftIcon>
      </a>
    </Button>
  );
}

function ExportButtons({ includeAll }: { includeAll: boolean }): JSX.Element {
  const client = useGramContext();
  const [exporting, setExporting] = useState<ChecklistFormat | null>(null);

  const run = async (format: ChecklistFormat) => {
    setExporting(format);
    try {
      await downloadXaaChecklist(client, format, includeAll);
    } catch (error) {
      toast.error(describeApiError(error).message);
    } finally {
      setExporting(null);
    }
  };

  return (
    <>
      <Button
        variant="secondary"
        size="sm"
        disabled={exporting != null}
        onClick={() => void run("csv")}
      >
        {exporting === "csv" ? "Exporting..." : "Export CSV"}
      </Button>
      <Button
        variant="secondary"
        size="sm"
        disabled={exporting != null}
        onClick={() => void run("markdown")}
      >
        {exporting === "markdown" ? "Exporting..." : "Export Markdown"}
      </Button>
    </>
  );
}

function OpenOktaButton({
  deepLink,
}: {
  deepLink: string | undefined;
}): JSX.Element {
  const href = oktaConnectionsUrl(deepLink);
  if (href) {
    return (
      <Button asChild variant="primary" size="sm">
        <a href={href} target="_blank" rel="noopener noreferrer">
          Open Okta
        </a>
      </Button>
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
  rolloutEnabled,
}: {
  connection: OktaIdentityProviderConnection;
  rolloutEnabled: boolean;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [filter, setFilter] = useState<ReadinessFilter>("pending");
  const includeAll = filter === "all";
  const readiness = useXaaReadiness({ includeAll }, SESSION_SECURITY, {
    throwOnError: false,
    retry: false,
    placeholderData: (previous) => previous,
  });
  const rows = useMemo(() => readiness.data?.servers ?? [], [readiness.data]);
  const confirmable = useMemo(
    () => (rolloutEnabled ? rows.filter(isConfirmable) : []),
    [rows, rolloutEnabled],
  );
  const selection = useRowSelection(confirmable, rowId);
  const appInstances = useAppInstances(connection.id);
  const [clearedConfirmations, setClearedConfirmations] = useState<
    ReadinessRow[]
  >([]);
  // Confirmations are shared only by servers with the same issuer ID and resource.
  useEffect(() => {
    if (readiness.isPlaceholderData) return;
    const confirmedResources = new Set(
      (readiness.data?.servers ?? [])
        .filter((row) => row.state === "connected")
        .map(recoveryKey),
    );
    setClearedConfirmations((previous) => {
      const remaining = previous.filter(
        (row) => !confirmedResources.has(recoveryKey(row)),
      );
      return remaining.length === previous.length ? previous : remaining;
    });
  }, [readiness.data, readiness.isPlaceholderData]);
  const retireRecovery = (resources: Set<string>) =>
    setClearedConfirmations((previous) =>
      previous.filter((row) => !resources.has(recoveryKey(row))),
    );
  const canUndo = (row: ReadinessRow) =>
    !!row.audience &&
    (!row.oktaApplicationId ||
      appInstances.some((app) => app.id === row.oktaApplicationId));
  const [reviewTarget, setReviewTarget] = useState<{
    row: ReadinessRow;
    initialValues: { audience?: string; oktaApplicationId?: string };
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

  const confirm = useConfirmXaaConnectionsMutation({ onError: inlineError });
  const [confirmError, setConfirmError] = useState<unknown>(undefined);
  const [confirming, setConfirming] = useState(false);
  const [lastConfirmed, setLastConfirmed] = useState(0);
  const confirmationInFlight = useRef(false);
  const [partialConfirmed, setPartialConfirmed] = useState(0);
  const confirmSelected = async (
    audience: string,
    oktaApplicationId: string | undefined,
  ) => {
    if (
      !rolloutEnabled ||
      reset.isPending ||
      confirmationInFlight.current ||
      readiness.isPlaceholderData ||
      selection.selectedCount === 0
    )
      return;
    confirmationInFlight.current = true;
    setConfirming(true);
    setConfirmError(undefined);
    setPartialConfirmed(0);
    setLastConfirmed(0);
    const recorded = new Set<string>();
    const completed = new Set<string>();
    try {
      for (const body of buildConfirmRequests(
        selection.selectedItems,
        audience,
        oktaApplicationId,
      )) {
        const result = await confirm.mutateAsync({
          security: SESSION_SECURITY,
          request: { confirmXaaConnectionsRequestBody: body },
        });
        retireRecovery(
          new Set(
            selection.selectedItems
              .filter((row) =>
                body.connections.some(
                  (item) => item.mcpServerId === row.mcpServerId,
                ),
              )
              .map(recoveryKey),
          ),
        );
        for (const server of result.servers) recorded.add(server.mcpServerId);
        for (const item of body.connections) completed.add(item.mcpServerId);
      }
      toast.success(`${pluralize(recorded.size, "server")} confirmed`);
      setLastConfirmed(recorded.size);
      selection.clear();
    } catch (error) {
      setConfirmError(error);
      setPartialConfirmed(recorded.size);
      // Keep only the unsuccessful batches selected for a safe retry.
      for (const row of selection.selectedItems) {
        if (completed.has(row.mcpServerId) || recorded.has(row.mcpServerId)) {
          selection.toggle(row.mcpServerId);
        }
      }
    } finally {
      confirmationInFlight.current = false;
      setConfirming(false);
      void invalidateIdentityProviderQueries(queryClient);
    }
  };

  const [resetTarget, setResetTarget] = useState<ReadinessRow | null>(null);
  const reset = useResetXaaConnectionMutation({
    onSuccess: () => {
      if (resetTarget)
        setClearedConfirmations((previous) => [
          ...previous.filter(
            (row) => row.mcpServerId !== resetTarget.mcpServerId,
          ),
          resetTarget,
        ]);
      toast.success(
        "Confirmation cleared. The Okta connection was not changed.",
      );
      setResetTarget(null);
      setReviewTarget(null);
      selection.clear();
      setLastConfirmed(0);
      setPartialConfirmed(0);
      setConfirmError(undefined);
      void invalidateIdentityProviderQueries(queryClient);
    },
    onError: (error) => toast.error(describeApiError(error).message),
  });
  const closeReset = () => {
    if (reset.isPending) return;
    setResetTarget(null);
    reset.reset();
  };

  const busy = confirming || reset.isPending;
  const openReview = (row: ReadinessRow, previous?: ReadinessRow) => {
    if (busy || readiness.isPlaceholderData || !rolloutEnabled) return;
    const saved =
      previous ??
      clearedConfirmations.find(
        (saved) => saved.mcpServerId === row.mcpServerId,
      ) ??
      row;
    selection.clear();
    setConfirmError(undefined);
    setLastConfirmed(0);
    setPartialConfirmed(0);
    setReviewTarget({
      row,
      initialValues: {
        audience: saved.audience,
        oktaApplicationId: saved.oktaApplicationId,
      },
    });
  };

  const confirmOne = async (
    row: ReadinessRow,
    audience: string,
    oktaApplicationId?: string,
  ) => {
    if (
      !rolloutEnabled ||
      reset.isPending ||
      confirmationInFlight.current ||
      readiness.isPlaceholderData
    )
      return;
    confirmationInFlight.current = true;
    setConfirming(true);
    setConfirmError(undefined);
    setLastConfirmed(0);
    setPartialConfirmed(0);
    try {
      const result = await confirm.mutateAsync({
        security: SESSION_SECURITY,
        request: {
          confirmXaaConnectionsRequestBody: {
            connections: [
              {
                mcpServerId: row.mcpServerId,
                audience,
                ...(oktaApplicationId ? { oktaApplicationId } : {}),
              },
            ],
          },
        },
      });
      retireRecovery(new Set([recoveryKey(row)]));
      setReviewTarget(null);
      setLastConfirmed(result.servers.length);
      toast.success("Confirmation saved. Nothing was changed in Okta.");
      selection.clear();
    } catch (error) {
      setConfirmError(error);
    } finally {
      confirmationInFlight.current = false;
      setConfirming(false);
      void invalidateIdentityProviderQueries(queryClient);
    }
  };

  const selectColumn: Column<ReadinessRow> = {
    key: "select",
    header: (
      <Checkbox
        checked={selection.allState}
        onCheckedChange={() => {
          setReviewTarget(null);
          selection.toggleAll();
        }}
        aria-label="Select every server that can be confirmed"
        disabled={
          busy || readiness.isPlaceholderData || confirmable.length === 0
        }
      />
    ),
    width: "48px",
    render: (row) => (
      <Checkbox
        checked={selection.isSelected(row.mcpServerId)}
        onCheckedChange={() => {
          setReviewTarget(null);
          selection.toggle(row.mcpServerId);
        }}
        disabled={busy || readiness.isPlaceholderData || !isConfirmable(row)}
        aria-label={`Select ${row.serverName}`}
        className="mt-1"
      />
    ),
  };
  const columns: Column<ReadinessRow>[] = [
    ...(rolloutEnabled ? [selectColumn] : []),
    {
      key: "serverName",
      header: "Server",
      width: "1.25fr",
      render: (row) => (
        <div className="flex min-w-0 flex-col">
          <Text className="text-sm leading-6 font-medium whitespace-normal [overflow-wrap:anywhere]">
            {row.serverName}
          </Text>
          <Text
            muted
            small
            className="truncate font-mono text-xs leading-5"
            title={`${row.projectSlug}/${row.serverSlug}`}
          >
            {row.projectSlug}/{row.serverSlug}
          </Text>
        </div>
      ),
    },
    {
      key: "state",
      header: "State",
      width: "1fr",
      render: (row) => <StateCell row={row} />,
    },
    {
      key: "configuration",
      header: "Okta configuration",
      width: "2.5fr",
      render: (row) => <ConfigurationCell row={row} />,
    },
    {
      key: "connection",
      header: "Confirmation",
      width: "2fr",
      render: (row) => <ConnectionCell row={row} />,
    },
    {
      key: "actions",
      header: <span className="sr-only">Actions</span>,
      width: "176px",
      render: (row) =>
        row.state === "connected" || isConfirmable(row) ? (
          <div className="flex w-full items-center justify-end gap-1">
            <Button
              variant="secondary"
              size="xs"
              disabled={busy || readiness.isPlaceholderData || !rolloutEnabled}
              onClick={() => openReview(row)}
            >
              {row.state === "connected" ? "Review / edit" : "Review setup"}
            </Button>
            {row.state === "connected" ? (
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button
                    variant="tertiary"
                    size="xs"
                    className="h-6 w-6 shrink-0 p-0"
                    aria-label={`More actions for ${row.serverName}`}
                    disabled={busy || readiness.isPlaceholderData}
                  >
                    <Button.LeftIcon>
                      <MoreHorizontal />
                    </Button.LeftIcon>
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  {oktaConnectionsUrl(
                    row.deepLink ?? readiness.data?.deepLink,
                  ) && (
                    <DropdownMenuItem asChild>
                      <a
                        href={oktaConnectionsUrl(
                          row.deepLink ?? readiness.data?.deepLink,
                        )}
                        target="_blank"
                        rel="noopener noreferrer"
                      >
                        Open Okta connections
                      </a>
                    </DropdownMenuItem>
                  )}
                  <DropdownMenuItem
                    onSelect={() => setResetTarget(row)}
                    disabled={busy}
                  >
                    Clear confirmation
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            ) : (
              <DeepLinkCell
                row={row}
                deepLink={row.deepLink ?? readiness.data?.deepLink}
              />
            )}
          </div>
        ) : null,
    },
  ];

  if (readiness.isPending) return <SkeletonTable />;
  if (readiness.data === undefined) {
    return <ApiErrorAlert error={readiness.error} />;
  }

  const data = readiness.data;

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
          <ExportButtons includeAll={includeAll} />
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
              to={identityTabHref("provider", AGENT_SECTION_ID)}
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

      {!rolloutEnabled && (
        <Alert variant="warning" alignTop>
          <Text variant="small">
            Identity provider connections are not enabled for this organization.
            New confirmations are unavailable, but you can still clear existing
            confirmations using Clear confirmation in the row menu.
          </Text>
        </Alert>
      )}

      <div className="flex min-w-0 flex-col gap-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <SegmentedControl<ReadinessFilter>
            value={filter}
            disabled={busy}
            onChange={(next) => {
              setReviewTarget(null);
              setFilter(next);
              setLastConfirmed(0);
              setPartialConfirmed(0);
              setConfirmError(undefined);
              selection.clear();
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

        {lastConfirmed > 0 && (
          <Alert variant="success" alignTop>
            <div className="flex flex-wrap items-center justify-between gap-3">
              <Text variant="small">
                {pluralize(lastConfirmed, "server")} confirmed
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
                    setLastConfirmed(0);
                    selection.clear();
                  }}
                >
                  Show all servers
                </Button>
              )}
            </div>
          </Alert>
        )}

        {clearedConfirmations.map((clearedConfirmation) => (
          <Alert key={clearedConfirmation.mcpServerId} variant="info" alignTop>
            <div className="flex flex-col gap-3">
              <Text small>
                Confirmation cleared for {clearedConfirmation.serverName}. The
                Okta connection was not changed.
              </Text>
              <Text muted small>
                Previous settings are retained on this page until you leave or
                these settings are confirmed again. Reuse the existing Okta
                connection rather than creating a duplicate.
              </Text>
              {!canUndo(clearedConfirmation) && (
                <Text small>
                  Undo is unavailable because the saved Issuer URL or
                  application is unavailable. Review setup to choose current
                  settings.
                </Text>
              )}
              {rolloutEnabled && (
                <div className="flex flex-wrap gap-2">
                  <Button
                    variant="secondary"
                    size="sm"
                    disabled={
                      busy ||
                      readiness.isPlaceholderData ||
                      !canUndo(clearedConfirmation)
                    }
                    onClick={() => {
                      if (
                        clearedConfirmation.audience &&
                        canUndo(clearedConfirmation)
                      )
                        void confirmOne(
                          clearedConfirmation,
                          clearedConfirmation.audience,
                          clearedConfirmation.oktaApplicationId,
                        );
                    }}
                  >
                    Undo
                  </Button>
                  <Button
                    variant="tertiary"
                    size="sm"
                    disabled={busy || readiness.isPlaceholderData}
                    onClick={() =>
                      openReview(
                        { ...clearedConfirmation, state: "needs_connection" },
                        clearedConfirmation,
                      )
                    }
                  >
                    Review setup
                  </Button>
                </div>
              )}
            </div>
          </Alert>
        ))}

        {readiness.isPlaceholderData && (
          <Text muted small role="status">
            Loading servers...
          </Text>
        )}
        {partialConfirmed > 0 && (
          <Alert variant="warning">
            <Text variant="small">
              {pluralize(partialConfirmed, "server")} confirmed before the
              request failed. Completed servers have been deselected. Review the
              remaining selection and retry.
            </Text>
          </Alert>
        )}
        <ApiErrorAlert error={confirmError} />
        {rolloutEnabled && !reviewTarget && selection.selectedCount > 0 && (
          <XaaConfirmBar
            selectedCount={selection.selectedCount}
            applications={appInstances}
            applicationsUrl={oktaApplicationsUrl(connection.orgUrl)}
            pending={busy || readiness.isPlaceholderData}
            error={undefined}
            onConfirm={(audience, appId) =>
              void confirmSelected(audience, appId)
            }
          />
        )}

        {rolloutEnabled && reviewTarget && (
          <div ref={reviewRef} tabIndex={-1} className="outline-none">
            <XaaConfirmBar
              key={`${reviewTarget.row.mcpServerId}:${reviewTarget.row.state}`}
              selectedCount={1}
              applications={appInstances}
              applicationsUrl={oktaApplicationsUrl(connection.orgUrl)}
              initialValues={reviewTarget.initialValues}
              review={{
                serverName: reviewTarget.row.serverName,
                confirmed: reviewTarget.row.state === "connected",
                connectionsUrl: oktaConnectionsUrl(
                  reviewTarget.row.deepLink ?? data.deepLink,
                ),
                createUrl: oktaConsoleUrl(
                  reviewTarget.row.deepLink ?? data.deepLink,
                ),
              }}
              pending={busy || readiness.isPlaceholderData}
              error={undefined}
              onCancel={() => {
                if (!busy) {
                  setReviewTarget(null);
                  setConfirmError(undefined);
                }
              }}
              onConfirm={(audience, appId) =>
                void confirmOne(reviewTarget.row, audience, appId)
              }
            />
          </div>
        )}

        <div
          role="region"
          aria-label="Cross App Access servers"
          tabIndex={0}
          className="min-w-0 overflow-x-auto focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset"
        >
          <div className={rows.length > 0 ? "min-w-[1040px]" : undefined}>
            <Table
              className="[&_th]:text-left [&_th]:whitespace-normal [&_td]:items-center"
              columns={columns}
              data={rows}
              rowKey={rowId}
              noResultsMessage={
                <Text muted>
                  {includeAll
                    ? "No MCP servers use their own authorization server yet."
                    : "Nothing needs action. Switch to All to see confirmed and not-applicable servers."}
                </Text>
              }
            />
          </div>
        </div>
      </div>

      <Dialog
        open={resetTarget != null}
        onOpenChange={(next) => {
          if (!next) closeReset();
        }}
      >
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Clear this confirmation?</Dialog.Title>
            <Dialog.Description>
              This removes Speakeasy’s saved confirmation and settings for{" "}
              {resetTarget?.serverName}. It does not delete or change the
              connection in Okta. Other servers that share this confirmation
              will also show as not confirmed.
              {rolloutEnabled
                ? resetTarget && canUndo(resetTarget)
                  ? " You can undo this on this page until these settings are confirmed again."
                  : " The saved Issuer URL or application is unavailable, so Undo cannot fully restore these settings. Review setup after clearing to choose current settings."
                : " Restoring it requires confirmations to be enabled for this organization."}
            </Dialog.Description>
          </Dialog.Header>
          <ApiErrorAlert error={reset.error} />
          <Dialog.Footer>
            <Button
              variant="secondary"
              onClick={closeReset}
              disabled={reset.isPending}
            >
              Cancel
            </Button>
            <Button
              variant="destructive-primary"
              disabled={reset.isPending || resetTarget == null}
              onClick={() => {
                if (!resetTarget) return;
                reset.mutate({
                  security: SESSION_SECURITY,
                  request: {
                    resetXaaConnectionRequestBody: {
                      mcpServerId: resetTarget.mcpServerId,
                    },
                  },
                });
              }}
            >
              {reset.isPending ? "Clearing..." : "Clear confirmation"}
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </div>
  );
}

export function CrossAppAccessTab({
  connection,
  rolloutEnabled,
}: ConnectionTabProps): JSX.Element {
  const gate = (
    <ConnectionGate
      connection={connection}
      rolloutEnabled={rolloutEnabled}
      icon="route"
      purpose="to set up Cross App Access"
      requires="checked"
    />
  );
  if (!connection || !canConfirmReadiness(connection)) return gate;
  return (
    <ReadinessChecklist
      connection={connection}
      rolloutEnabled={rolloutEnabled}
    />
  );
}
