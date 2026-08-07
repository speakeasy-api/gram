import { Heading } from "@/components/ui/Heading";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import type { DeviceIntegrationProvider } from "@gram/client/models/components/deviceintegrationprovider.js";
import { useDeviceIntegrationProviders } from "@gram/client/react-query/deviceIntegrationProviders.js";
import { Stack } from "@/components/ui/Stack";
import React from "react";
import { CoveragePipeline } from "./coverage-pipeline";
import { DeviceIntegrationConnectionRow } from "./device-integration-connection-row";
import { isProviderVisible, isSink } from "./provider-role";

// MDM Integrations catalog, rendered as a tab of the Device Agent page. The
// page is a one-directional pipeline: inventory sources pull the managed
// fleet, coverage is computed against agent heartbeats, and evidence
// destinations push that coverage out. The layout mirrors that flow — a
// pipeline banner over two role-labeled groups — so the two opposite roles
// never read as interchangeable. The tab is gated behind the
// gram-device-integrations flag by the Device Agent tab shell.
export function MdmIntegrationsTab(): React.JSX.Element {
  const { data, isLoading } = useDeviceIntegrationProviders(
    undefined,
    undefined,
    // The provider registry only changes on deploy.
    { staleTime: 300_000 },
  );
  const providers = (data?.providers ?? []).filter(isProviderVisible);
  const sources = providers.filter((provider) => !isSink(provider));
  const sinks = providers.filter(isSink);

  // The Device Agent page renders the area eyebrow and display-serif page
  // title above the tab strip, so this in-tab header stays a plain section
  // heading rather than repeating the page-header idiom.
  return (
    <Stack gap={6} className="mt-3 mb-6">
      <div>
        <Heading variant="h4" className="mb-2">
          MDM Integrations
        </Heading>
        <Text muted small>
          Pull your managed-device fleet from your MDMs, see which devices are
          running the agent, and publish that coverage to your compliance
          platforms as continuously-tested evidence.
        </Text>
      </div>
      {isLoading ? (
        <SkeletonTable />
      ) : providers.length === 0 ? (
        <Text muted className="block p-4">
          No integration providers are available.
        </Text>
      ) : (
        <Stack gap={8}>
          <CoveragePipeline sources={sources} sinks={sinks} />
          <ProviderGroup
            title="Device inventory sources"
            description="Connect your MDM so Gram knows which devices exist and which are running the agent. Each source pulls its device list on a schedule; together they form the fleet above."
            providers={sources}
          />
          <ProviderGroup
            title="Compliance evidence destinations"
            description="Publish agent coverage as continuously-tested evidence. Each destination pushes the same org-wide fleet above — it has no device list of its own."
            providers={sinks}
          />
        </Stack>
      )}
    </Stack>
  );
}

function ProviderGroup({
  title,
  description,
  providers,
}: {
  title: string;
  description: string;
  providers: DeviceIntegrationProvider[];
}): React.JSX.Element | null {
  if (providers.length === 0) return null;
  return (
    <Stack gap={3}>
      <Stack gap={1}>
        <Text variant="body" className="font-semibold">
          {title}
        </Text>
        <Text muted small className="max-w-prose">
          {description}
        </Text>
      </Stack>
      <div className="border-border bg-card divide-border divide-y overflow-hidden border">
        {providers.map((provider) => (
          <DeviceIntegrationConnectionRow
            key={provider.id}
            provider={provider}
          />
        ))}
      </div>
    </Stack>
  );
}
