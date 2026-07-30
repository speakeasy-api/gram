import { Page } from "@/components/page-layout";
import { SkeletonTable } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useDeviceIntegrationProviders } from "@gram/client/react-query/deviceIntegrationProviders.js";
import React from "react";
import { DeviceIntegrationConnectionRow } from "./device-integration-connection-row";

// MDM Integrations catalog, rendered as a tab of the Device Agent page: one
// row per vendor; clicking a row opens its detail page with coverage,
// schedules, and the synced device inventory. The tab itself is gated behind
// the gram-device-integrations flag by the Device Agent tab shell.
export function MdmIntegrationsTab(): React.JSX.Element {
  const { data, isLoading } = useDeviceIntegrationProviders(
    undefined,
    undefined,
    // The provider registry only changes on deploy.
    { staleTime: 300_000 },
  );
  const providers = data?.providers ?? [];

  return (
    <Page.Section>
      <Page.Section.Title>MDM Integrations</Page.Section.Title>
      <Page.Section.Description>
        Connect your MDM and compliance vendors. MDM inventory reveals which
        managed devices are missing the device agent; compliance connections
        push that coverage as evidence.
      </Page.Section.Description>
      <Page.Section.Body>
        {isLoading ? (
          <SkeletonTable />
        ) : (
          <div className="border-border bg-card divide-border divide-y overflow-hidden rounded-lg border">
            {providers.map((provider) => (
              <DeviceIntegrationConnectionRow
                key={provider.id}
                provider={provider}
              />
            ))}
            {providers.length === 0 ? (
              <Type muted className="block p-4">
                No integration providers are available.
              </Type>
            ) : null}
          </div>
        )}
      </Page.Section.Body>
    </Page.Section>
  );
}
