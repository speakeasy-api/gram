import { RequireScope } from "@/components/require-scope";
import { Switch } from "@/components/ui/switch";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import {
  Badge,
  Button,
  type Column,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Stack,
  Table,
} from "@speakeasy-api/moonshine";
import { Activity, ChartLine, Clock3, MoreHorizontal } from "lucide-react";
import type { AIIntegrationSchedule } from "./ai-integration-providers";
import { ScheduleStatusBadge } from "./ai-integration-status-badge";
import {
  formatRelativeTime,
  type ScheduleRuntime,
} from "./use-ai-integration-schedules";

// One row per stream (an imported event or metric feed). The row carries
// everything the cells need so the table itself stays hook-free.
export type AIIntegrationStreamRow = {
  key: string;
  schedule: AIIntegrationSchedule;
  runtime: ScheduleRuntime;
  configured: boolean;
  connectionEnabled: boolean;
  toggle: (schedule: string, enabled: boolean) => void;
  retry: (schedule: string) => void;
};

export function AIIntegrationStreamsTable({
  rows,
}: {
  rows: AIIntegrationStreamRow[];
}): JSX.Element {
  const columns: Column<AIIntegrationStreamRow>[] = [
    {
      key: "name",
      header: "Stream",
      render: (row) => <NameCell row={row} />,
    },
    {
      key: "type",
      header: "Type",
      width: "120px",
      render: (row) => <TypeCell row={row} />,
    },
    {
      key: "cadence",
      header: "Cadence",
      width: "100px",
      render: (row) => (
        <Stack direction="horizontal" align="center" gap={1.5}>
          <Clock3 className="text-muted-foreground size-3.5 shrink-0" />
          <Type muted small className="whitespace-nowrap">
            {row.schedule.cadence}
          </Type>
        </Stack>
      ),
    },
    {
      key: "lastSynced",
      header: "Last synced",
      width: "110px",
      render: (row) => (
        <Type muted small className="whitespace-nowrap">
          {lastSyncedLabel(row)}
        </Type>
      ),
    },
    {
      key: "status",
      header: "Status",
      // Just wide enough for the longest badge ("Not connected").
      width: "140px",
      // Failure detail lives in the badge's tooltip, not inline.
      render: (row) => (
        <ScheduleStatusBadge
          runtime={row.runtime}
          configured={row.configured}
          connectionEnabled={row.connectionEnabled}
        />
      ),
    },
    {
      key: "actions",
      header: "",
      width: "110px",
      render: (row) => <ActionsCell row={row} />,
    },
  ];

  return (
    <Table
      columns={columns}
      data={rows}
      rowKey={(row) => row.key}
      noResultsMessage={<Type muted>No streams</Type>}
    />
  );
}

function NameCell({ row }: { row: AIIntegrationStreamRow }) {
  // The backend registry owns stream identifiers; the static provider
  // metadata only fills in before the backend has a sync row.
  const stream = row.runtime.stream ?? row.schedule.signal;
  return (
    <SimpleTooltip
      tooltip={`${row.schedule.name} — ${row.schedule.description}`}
    >
      <Type variant="small" className="w-fit font-mono text-xs font-medium">
        {stream}
      </Type>
    </SimpleTooltip>
  );
}

function TypeCell({ row }: { row: AIIntegrationStreamRow }) {
  const kind = row.runtime.streamKind ?? row.schedule.kind;
  const isEvents = kind === "events";
  const KindIcon = isEvents ? Activity : ChartLine;
  return (
    <Badge variant="neutral" background className="shrink-0">
      <Badge.LeftIcon>
        <KindIcon className="h-3 w-3" />
      </Badge.LeftIcon>
      <Badge.Text>{isEvents ? "Event" : "Metric"}</Badge.Text>
    </Badge>
  );
}

function ActionsCell({ row }: { row: AIIntegrationStreamRow }) {
  const canRetry = streamNeedsAttention(row) && !row.runtime.isMutating;

  return (
    <Stack direction="horizontal" align="center" justify="end" gap={1}>
      <RequireScope scope="org:admin" level="component">
        <SimpleTooltip
          tooltip={
            row.configured
              ? "Pause or resume this stream. Applies immediately."
              : "Connect the provider before enabling this stream."
          }
        >
          <Switch
            checked={row.configured && row.runtime.enabled}
            onCheckedChange={(checked) =>
              row.toggle(row.schedule.schedule, checked)
            }
            disabled={
              !row.configured ||
              !row.connectionEnabled ||
              row.runtime.isMutating
            }
            aria-label={`Enable ${row.schedule.name}`}
          />
        </SimpleTooltip>
      </RequireScope>
      <RequireScope scope="org:admin" level="component">
        <DropdownMenu modal={false}>
          <DropdownMenuTrigger asChild>
            <Button
              variant="tertiary"
              size="sm"
              disabled={!row.configured}
              aria-label={`${row.schedule.name} actions`}
            >
              <Button.Icon>
                <MoreHorizontal className="size-4" />
              </Button.Icon>
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem
              onSelect={() => row.retry(row.schedule.schedule)}
              disabled={!canRetry}
            >
              Retry now
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </RequireScope>
    </Stack>
  );
}

// A stream needs attention when it is actively polling but the provider
// rejected or failed the last poll.
function streamNeedsAttention(row: AIIntegrationStreamRow): boolean {
  return (
    row.configured &&
    row.connectionEnabled &&
    row.runtime.enabled &&
    (row.runtime.status === "failed" || row.runtime.status === "auto_paused")
  );
}

function lastSyncedLabel(row: AIIntegrationStreamRow): string {
  if (!row.configured || !row.runtime.lastSyncedAt) return "—";
  return formatRelativeTime(row.runtime.lastSyncedAt) ?? "—";
}
