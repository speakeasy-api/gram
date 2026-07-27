import { RequireScope } from "@/components/require-scope";
import { ScheduleStatusBadge } from "@/components/schedule-status-badge";
import { Switch } from "@/components/ui/switch";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { formatRelativeTime } from "@/lib/dates";
import type { DeviceIntegrationProviderSchedule } from "@gram/client/models/components/deviceintegrationproviderschedule.js";
import {
  Button,
  type Column,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Stack,
  Table,
} from "@speakeasy-api/moonshine";
import { Clock3, MoreHorizontal } from "lucide-react";
import {
  formatCadence,
  type ScheduleRuntime,
} from "./use-device-integration-schedules";

// One row per sync schedule. The row carries everything the cells need so the
// table itself stays hook-free.
export type DeviceIntegrationScheduleRow = {
  key: string;
  schedule: DeviceIntegrationProviderSchedule;
  runtime: ScheduleRuntime;
  configured: boolean;
  connectionEnabled: boolean;
  toggle: (schedule: string, enabled: boolean) => void;
  retry: (schedule: string) => void;
};

export function DeviceIntegrationSchedulesTable({
  rows,
}: {
  rows: DeviceIntegrationScheduleRow[];
}): JSX.Element {
  const columns: Column<DeviceIntegrationScheduleRow>[] = [
    {
      key: "name",
      header: "Schedule",
      render: (row) => (
        <Type variant="small" className="w-fit font-mono text-xs font-medium">
          {row.schedule.schedule}
        </Type>
      ),
    },
    {
      key: "cadence",
      header: "Cadence",
      width: "120px",
      render: (row) => (
        <Stack direction="horizontal" align="center" gap={1.5}>
          <Clock3 className="text-muted-foreground size-3.5 shrink-0" />
          <Type muted small className="whitespace-nowrap">
            {formatCadence(row.schedule.intervalMinutes)}
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
      noResultsMessage={<Type muted>No schedules</Type>}
    />
  );
}

function ActionsCell({ row }: { row: DeviceIntegrationScheduleRow }) {
  const canRetry =
    row.configured && row.connectionEnabled && !row.runtime.isMutating;

  return (
    <Stack direction="horizontal" align="center" justify="end" gap={1}>
      <RequireScope scope="org:admin" level="component">
        <SimpleTooltip
          tooltip={
            row.configured
              ? "Pause or resume this schedule. Applies immediately."
              : "Connect the provider before enabling this schedule."
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
            aria-label={`Enable ${row.schedule.schedule}`}
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
              aria-label={`${row.schedule.schedule} actions`}
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
              Sync now
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </RequireScope>
    </Stack>
  );
}

function lastSyncedLabel(row: DeviceIntegrationScheduleRow): string {
  if (!row.configured || !row.runtime.lastSyncedAt) return "—";
  return formatRelativeTime(row.runtime.lastSyncedAt) ?? "—";
}
