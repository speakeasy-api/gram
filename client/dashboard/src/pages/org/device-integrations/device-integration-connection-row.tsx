import { RequireScope } from "@/components/require-scope";
import { Switch } from "@/components/ui/switch";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import type { DeviceIntegrationProvider } from "@gram/client/models/components/deviceintegrationprovider.js";
import { Button, Stack } from "@speakeasy-api/moonshine";
import { ChevronDown, PlugZap, Trash2 } from "lucide-react";
import { useState } from "react";
import {
  ConnectionStatusBadge,
  DeviceIntegrationConfigureSheet,
} from "./device-integration-configure-sheet";
import { providerUI } from "./provider-ui";
import {
  type DeviceIntegrationScheduleRow,
  DeviceIntegrationSchedulesTable,
} from "./device-integration-schedules-table";
import { useDeviceIntegrationConfigForm } from "./use-device-integration-config";
import {
  runtimeOrDefault,
  useDeviceScheduleRuntimes,
} from "./use-device-integration-schedules";

// One provider connection: a collapsed row that expands to reveal the
// provider's sync schedules, with the credential form in a side sheet.
export function DeviceIntegrationConnectionRow({
  provider,
}: {
  provider: DeviceIntegrationProvider;
}): JSX.Element {
  const [configureOpen, setConfigureOpen] = useState(false);
  const [expanded, setExpanded] = useState(false);
  const form = useDeviceIntegrationConfigForm(provider, {
    onDeleteSuccess: () => setConfigureOpen(false),
  });
  const { runtimes, toggle, retry } = useDeviceScheduleRuntimes(provider.id);
  const ui = providerUI(provider);
  const Icon = ui.icon;

  const activeCount = provider.schedules.filter(
    (schedule) => runtimeOrDefault(runtimes, schedule.schedule).enabled,
  ).length;
  const summary = scheduleSummary({
    configured: form.isConfigured,
    activeCount,
    total: provider.schedules.length,
  });

  const scheduleRows = provider.schedules.map(
    (schedule): DeviceIntegrationScheduleRow => ({
      key: `${provider.id}:${schedule.schedule}`,
      schedule,
      runtime: runtimeOrDefault(runtimes, schedule.schedule),
      configured: form.isConfigured,
      connectionEnabled: form.enabled,
      toggle,
      retry,
    }),
  );

  const handleDelete = () => {
    if (!form.isConfigured) return;
    if (
      !window.confirm(`Delete the ${provider.displayName} device integration?`)
    ) {
      return;
    }
    form.remove();
  };

  return (
    <div className="flex flex-col">
      {/* The whole header row toggles the schedules, so interactive children
          stop propagation to keep their own clicks from collapsing it. */}
      <div
        role="button"
        tabIndex={0}
        aria-expanded={expanded}
        aria-label={`${expanded ? "Hide" : "Show"} ${provider.displayName} schedules`}
        onClick={() => setExpanded((current) => !current)}
        onKeyDown={(event) => {
          if (event.key !== "Enter" && event.key !== " ") return;
          event.preventDefault();
          setExpanded((current) => !current);
        }}
        className="hover:bg-muted/50 cursor-pointer p-4 transition-colors focus-visible:outline-none"
      >
        <Stack
          direction="horizontal"
          justify="space-between"
          align="center"
          gap={4}
        >
          <Stack gap={1} className="min-w-0 flex-1">
            <Stack
              direction="horizontal"
              align="center"
              gap={2}
              className="min-w-0"
            >
              <Icon className="text-foreground h-4 w-4 shrink-0" />
              <Type variant="body" className="min-w-0 truncate font-medium">
                {provider.displayName}
              </Type>
              <ConnectionStatusBadge
                enabled={form.enabled}
                configured={form.isConfigured}
              />
            </Stack>
            <Type muted small className="ml-6 truncate">
              {ui.description}
            </Type>
          </Stack>

          <Stack
            direction="horizontal"
            align="center"
            gap={3}
            className="shrink-0"
          >
            {/* Secondary info: drop it before squeezing the provider name.
                Sized against the main content container, not the viewport. */}
            <Type muted small className="hidden whitespace-nowrap @3xl:block">
              {summary}
            </Type>
            <RequireScope scope="org:admin" level="component">
              <SimpleTooltip
                tooltip={
                  form.isConfigured
                    ? "Pause or resume the whole connection. Applies immediately."
                    : "Connect the provider before enabling it."
                }
              >
                <span onClick={(event) => event.stopPropagation()}>
                  <Switch
                    checked={form.enabled}
                    onCheckedChange={form.saveEnabled}
                    disabled={
                      !form.isConfigured || form.isLoading || form.isMutating
                    }
                    aria-label={`Enable ${provider.displayName} connection`}
                  />
                </span>
              </SimpleTooltip>
            </RequireScope>
            <RequireScope scope="org:admin" level="component">
              <Button
                variant="secondary"
                size="sm"
                onClick={(event) => {
                  event.stopPropagation();
                  setConfigureOpen(true);
                }}
              >
                <Button.LeftIcon>
                  <PlugZap className="size-3.5" />
                </Button.LeftIcon>
                <Button.Text>
                  {form.isConfigured ? "Configure" : "Connect"}
                </Button.Text>
              </Button>
            </RequireScope>
            <ChevronDown
              aria-hidden
              className={`text-muted-foreground h-4 w-4 shrink-0 transition-transform ${expanded ? "rotate-180" : ""}`}
            />
          </Stack>
        </Stack>
      </div>

      {expanded ? (
        <Stack gap={3} className="px-4 pb-4 pl-10">
          <DeviceIntegrationSchedulesTable rows={scheduleRows} />
          {form.isConfigured ? (
            <Stack direction="horizontal" justify="end" align="center">
              <RequireScope scope="org:admin" level="component">
                <Button
                  variant="destructive-secondary"
                  size="sm"
                  onClick={handleDelete}
                  disabled={form.isMutating}
                >
                  <Button.LeftIcon>
                    <Trash2 className="size-3.5" />
                  </Button.LeftIcon>
                  <Button.Text>Delete connection</Button.Text>
                </Button>
              </RequireScope>
            </Stack>
          ) : null}
        </Stack>
      ) : null}

      <DeviceIntegrationConfigureSheet
        provider={provider}
        form={form}
        open={configureOpen}
        onOpenChange={setConfigureOpen}
      />
    </div>
  );
}

function scheduleSummary({
  configured,
  activeCount,
  total,
}: {
  configured: boolean;
  activeCount: number;
  total: number;
}): string {
  const noun = total === 1 ? "schedule" : "schedules";
  if (!configured) return `${total} ${noun} available`;
  return `${activeCount} of ${total} ${noun} active`;
}
