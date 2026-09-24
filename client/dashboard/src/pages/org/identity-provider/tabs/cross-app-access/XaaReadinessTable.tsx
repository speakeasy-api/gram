import { ExternalLink, MoreHorizontal } from "lucide-react";

import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { CopyButton } from "@/components/ui/CopyButton";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import type { RowSelection } from "@/hooks/useRowSelection";
import { HumanizeDateTime } from "@/lib/dates";
import type { OktaResourceConnectionServer } from "@gram/client/models/components/oktaresourceconnectionserver.js";

import { OktaLinkButton } from "./OktaLinkButton";
import { oktaConnectionsUrl } from "../../oktaConsoleLinks";
import {
  clientBindingNote,
  isConfirmable,
  notApplicableReasonLabel,
  notApplicableReasonSummary,
  xaaStateLabel,
  xaaStateVariant,
} from "./xaaView";

function EmptyCell(): JSX.Element {
  return <span className="text-muted-foreground text-xs leading-6">—</span>;
}

function CopyCell({
  value,
  label,
}: {
  value: string | undefined;
  label: string;
}): JSX.Element {
  if (!value) return <EmptyCell />;
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

function ServerCell({
  row,
}: {
  row: OktaResourceConnectionServer;
}): JSX.Element {
  return (
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
  );
}

function StateCell({
  row,
}: {
  row: OktaResourceConnectionServer;
}): JSX.Element {
  return (
    <div className="flex min-w-0 flex-col items-start gap-2 pt-1">
      <Badge variant={xaaStateVariant(row.state)} size="sm">
        {xaaStateLabel(row.state)}
      </Badge>
      {row.state === "not_applicable" && (
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

function ConfirmationCell({
  row,
}: {
  row: OktaResourceConnectionServer;
}): JSX.Element {
  if (row.state !== "connected") return <EmptyCell />;
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

function ConfigurationCell({
  row,
}: {
  row: OktaResourceConnectionServer;
}): JSX.Element {
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

function RowActions({
  row,
  connectionsHref,
  locked,
  onReview,
  onClear,
}: {
  row: OktaResourceConnectionServer;
  connectionsHref: string | undefined;
  locked: boolean;
  onReview: (row: OktaResourceConnectionServer) => void;
  onClear: (row: OktaResourceConnectionServer) => void;
}): JSX.Element | null {
  const confirmed = row.state === "connected";
  if (!confirmed && !isConfirmable(row)) return null;
  return (
    <div className="flex w-full items-center justify-end gap-1">
      <Button
        variant="secondary"
        size="xs"
        disabled={locked}
        onClick={() => onReview(row)}
      >
        {confirmed ? "Review / edit" : "Review setup"}
      </Button>
      {confirmed ? (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              variant="tertiary"
              size="xs"
              className="h-6 w-6 shrink-0 p-0"
              aria-label={`More actions for ${row.serverName}`}
              disabled={locked}
            >
              <Button.LeftIcon>
                <MoreHorizontal />
              </Button.LeftIcon>
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            {connectionsHref && (
              <DropdownMenuItem asChild>
                <a
                  href={connectionsHref}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  Open Okta connections
                </a>
              </DropdownMenuItem>
            )}
            <DropdownMenuItem onSelect={() => onClear(row)} disabled={locked}>
              Clear confirmation
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      ) : (
        connectionsHref && (
          <OktaLinkButton
            href={connectionsHref}
            variant="tertiary"
            size="xs"
            tooltip="Open Okta connections"
            aria-label={`Open Okta connections for ${row.serverName}`}
          >
            <Button.LeftIcon>
              <ExternalLink />
            </Button.LeftIcon>
          </OktaLinkButton>
        )
      )}
    </div>
  );
}

export function XaaReadinessTable({
  rows,
  selection,
  locked,
  fallbackDeepLink,
  emptyMessage,
  onSelectionChange,
  onReview,
  onClear,
}: {
  rows: OktaResourceConnectionServer[];
  selection: RowSelection<OktaResourceConnectionServer>;
  /** True while a confirmation is running or the rows are stale. */
  locked: boolean;
  fallbackDeepLink: string | undefined;
  emptyMessage: string;
  onSelectionChange: () => void;
  onReview: (row: OktaResourceConnectionServer) => void;
  onClear: (row: OktaResourceConnectionServer) => void;
}): JSX.Element {
  const columns: Column<OktaResourceConnectionServer>[] = [
    {
      key: "select",
      header: (
        <Checkbox
          checked={selection.allState}
          onCheckedChange={() => {
            onSelectionChange();
            selection.toggleAll();
          }}
          aria-label="Select every server that can be confirmed"
          disabled={locked || !rows.some(isConfirmable)}
        />
      ),
      width: "48px",
      render: (row) => (
        <Checkbox
          checked={selection.isSelected(row.mcpServerId)}
          onCheckedChange={() => {
            onSelectionChange();
            selection.toggle(row.mcpServerId);
          }}
          disabled={locked || !isConfirmable(row)}
          aria-label={`Select ${row.serverName}`}
          className="mt-1"
        />
      ),
    },
    {
      key: "serverName",
      header: "Server",
      width: "1.25fr",
      render: (row) => <ServerCell row={row} />,
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
      render: (row) => <ConfirmationCell row={row} />,
    },
    {
      key: "actions",
      header: <span className="sr-only">Actions</span>,
      width: "176px",
      render: (row) => (
        <RowActions
          row={row}
          connectionsHref={oktaConnectionsUrl(row.deepLink ?? fallbackDeepLink)}
          locked={locked}
          onReview={onReview}
          onClear={onClear}
        />
      ),
    },
  ];

  return (
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
          rowKey={(row) => row.mcpServerId}
          noResultsMessage={<Text muted>{emptyMessage}</Text>}
        />
      </div>
    </div>
  );
}
