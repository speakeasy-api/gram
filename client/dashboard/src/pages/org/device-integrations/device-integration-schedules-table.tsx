import { WidgetEmptyState } from "@/components/chart/WidgetEmptyState";
import { RequireScope } from "@/components/require-scope";
import { ScheduleStatusBadge } from "@/components/schedule-status-badge";
import { Switch } from "@/components/ui/Switch";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { Text } from "@/components/ui/Text";
import { formatRelativeTime } from "@/lib/dates";
import { cn } from "@/lib/utils";
import type { DeviceIntegrationProviderSchedule } from "@gram/client/models/components/deviceintegrationproviderschedule.js";
import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import { type Column, Table } from "@/components/ui/Table";
import { Clock3, RefreshCw } from "lucide-react";
import { memo, useMemo } from "react";
import { type ProviderRole, ROLE_COPY } from "./provider-role";
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
  // Drives the direction-specific language: a source is "synced", a sink is
  // "pushed". Carried on the row so the action cell reads it without a
  // table-level prop.
  role: ProviderRole;
  toggle: (schedule: string, enabled: boolean) => void;
  retry: (schedule: string) => void;
};

// Columns depend on role only for the last-run header ("Last synced" vs "Last
// pushed"); memoized per role by the caller so identity stays stable.
function scheduleColumns(
  role: ProviderRole,
): Column<DeviceIntegrationScheduleRow>[] {
  return [
    {
      key: "name",
      header: "Schedule",
      render: (row) => (
        <Text variant="small" className="w-fit font-mono text-xs font-medium">
          {row.schedule.schedule}
        </Text>
      ),
    },
    {
      key: "cadence",
      header: "Cadence",
      width: "120px",
      render: (row) => (
        <Stack direction="horizontal" align="center" gap={1.5}>
          <Clock3 className="text-muted-foreground size-3.5 shrink-0" />
          <Text muted small className="whitespace-nowrap">
            {formatCadence(row.schedule.intervalMinutes)}
          </Text>
        </Stack>
      ),
    },
    {
      key: "lastSynced",
      header: ROLE_COPY[role].lastRunHeader,
      width: "110px",
      render: (row) => (
        <Text muted small className="whitespace-nowrap">
          {lastSyncedLabel(row)}
        </Text>
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
}

export const DeviceIntegrationSchedulesTable = memo(
  function DeviceIntegrationSchedulesTable({
    rows,
    role = "source",
  }: {
    rows: DeviceIntegrationScheduleRow[];
    role?: ProviderRole;
  }): JSX.Element {
    const columns = useMemo(() => scheduleColumns(role), [role]);
    return (
      <Table
        columns={columns}
        data={rows}
        rowKey={(row) => row.key}
        noResultsMessage={<WidgetEmptyState message="No schedules" />}
      />
    );
  },
);

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
          {/* The span carries the Radix tooltip trigger props: Switch is a
              plain component that doesn't forward refs or spread props, so
              as a direct asChild child the tooltip would never open.
              inline-flex (not the default block) strips the line-height
              descender that otherwise sits the switch ~2px above the row
              center, misaligning it with the sibling action button. */}
          <span className="inline-flex items-center">
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
          </span>
        </SimpleTooltip>
      </RequireScope>
      <RequireScope scope="org:admin" level="component">
        <SimpleTooltip
          tooltip={
            row.configured
              ? ROLE_COPY[row.role].syncNowTooltip
              : "Connect the provider first."
          }
        >
          {/* A disabled button receives no pointer events, so the tooltip
              trigger has to be this always-enabled span — otherwise the
              "connect first" explanation never shows on the state where it
              matters. inline-flex keeps the button centered with the sibling
              switch (see the switch wrapper above). */}
          <span className="inline-flex items-center">
            <Button
              variant="tertiary"
              size="sm"
              onClick={() => row.retry(row.schedule.schedule)}
              disabled={!canRetry}
              aria-label={ROLE_COPY[row.role].syncNowAria(
                row.schedule.schedule,
              )}
              className="size-8 px-0"
            >
              <Button.Icon>
                <RefreshCw
                  className={cn(
                    "size-4",
                    row.runtime.isMutating && "animate-spin",
                  )}
                />
              </Button.Icon>
            </Button>
          </span>
        </SimpleTooltip>
      </RequireScope>
    </Stack>
  );
}

function lastSyncedLabel(row: DeviceIntegrationScheduleRow): string {
  if (!row.configured || !row.runtime.lastSyncedAt) return "—";
  return formatRelativeTime(row.runtime.lastSyncedAt) ?? "—";
}
