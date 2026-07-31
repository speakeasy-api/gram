import { RequireScope } from "@/components/require-scope";
import { Switch } from "@/components/ui/Switch";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { Text } from "@/components/ui/Text";
import { useOrgRoutes } from "@/routes";
import type { DeviceIntegrationProvider } from "@gram/client/models/components/deviceintegrationprovider.js";
import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import { ChevronRight, PlugZap } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router";
import {
  ConnectionStatusBadge,
  DeviceIntegrationConfigureSheet,
} from "./device-integration-configure-sheet";
import { providerRole } from "./provider-role";
import { providerUI } from "./provider-ui";
import { useDeviceIntegrationConfigForm } from "./use-device-integration-config";
import {
  runtimeOrDefault,
  useDeviceScheduleRuntimes,
} from "./use-device-integration-schedules";

// One provider connection. The row body is a real link to the provider's
// detail page (native cmd/middle-click and keyboard semantics); the quick
// actions — enable/disable and the credential sheet — are SIBLINGS of the
// link, never nested inside it, so their keyboard events cannot leak into
// navigation.
export function DeviceIntegrationConnectionRow({
  provider,
}: {
  provider: DeviceIntegrationProvider;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const [configureOpen, setConfigureOpen] = useState(false);
  const form = useDeviceIntegrationConfigForm(provider);
  const { runtimes } = useDeviceScheduleRuntimes(
    provider.id,
    providerRole(provider),
  );
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

  const handleSheetChange = (open: boolean) => {
    if (!open) form.resetDraft();
    setConfigureOpen(open);
  };

  return (
    <div className="hover:bg-muted/50 relative flex flex-col transition-colors">
      <Stack
        direction="horizontal"
        justify="space-between"
        align="center"
        gap={4}
        className="p-4"
      >
        {/* Stretched link: the ::after overlay makes the whole row navigate
            while the markup stays a single non-nested anchor. Interactive
            controls opt out by sitting above it with relative + z-10. */}
        <Link
          to={orgRoutes.deviceAgent.mdmDetail.href(provider.id)}
          aria-label={`Open ${provider.displayName} details`}
          className="min-w-0 flex-1 focus-visible:outline-none after:absolute after:inset-0 after:content-['']"
        >
          <Stack gap={1} className="min-w-0">
            <Stack
              direction="horizontal"
              align="center"
              gap={2}
              className="min-w-0"
            >
              <Icon className="text-foreground h-4 w-4 shrink-0" />
              <Text variant="body" className="min-w-0 truncate font-medium">
                {provider.displayName}
              </Text>
              <ConnectionStatusBadge
                enabled={form.enabled}
                configured={form.isConfigured}
              />
            </Stack>
            <Text muted small className="ml-6 truncate">
              {ui.description}
            </Text>
          </Stack>
        </Link>

        <Stack
          direction="horizontal"
          align="center"
          gap={3}
          className="shrink-0"
        >
          {/* Secondary info: drop it before squeezing the provider name.
              Sized against the main content container, not the viewport. */}
          <Text muted small className="hidden whitespace-nowrap @3xl:block">
            {summary}
          </Text>
          <RequireScope scope="org:admin" level="component">
            <SimpleTooltip
              tooltip={
                form.isConfigured
                  ? "Pause or resume the whole connection. Applies immediately."
                  : "Connect the provider before enabling it."
              }
            >
              <span className="relative z-10">
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
              className="relative z-10"
              onClick={() => setConfigureOpen(true)}
            >
              <Button.LeftIcon>
                <PlugZap className="size-3.5" />
              </Button.LeftIcon>
              <Button.Text>
                {form.isConfigured ? "Configure" : "Connect"}
              </Button.Text>
            </Button>
          </RequireScope>
          <ChevronRight
            aria-hidden
            className="text-muted-foreground h-4 w-4 shrink-0"
          />
        </Stack>
      </Stack>

      <DeviceIntegrationConfigureSheet
        provider={provider}
        form={form}
        open={configureOpen}
        onOpenChange={handleSheetChange}
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
