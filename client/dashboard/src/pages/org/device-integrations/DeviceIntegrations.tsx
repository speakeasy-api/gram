import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { SkeletonTable } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useTelemetry } from "@/contexts/Telemetry";
import { useDeviceIntegrationProviders } from "@gram/client/react-query/deviceIntegrationProviders.js";
import React from "react";
import { Navigate } from "react-router";
import { DeviceIntegrationConnectionRow } from "./device-integration-connection-row";

// Device Integrations: one row per MDM / compliance vendor connection that
// expands to reveal its sync schedules, each with its own status and controls.
export default function DeviceIntegrations(): React.JSX.Element | null {
  const telemetry = useTelemetry();
  const isEnabled = telemetry.isFeatureEnabled("gram-device-integrations");

  // Flags haven't resolved yet — render nothing rather than flashing a redirect.
  if (isEnabled === undefined) {
    return null;
  }

  if (!isEnabled) {
    return <Navigate to=".." replace />;
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope={["org:read", "org:admin"]} level="page">
          <DeviceIntegrationsInner />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

function DeviceIntegrationsInner() {
  const { data, isLoading } = useDeviceIntegrationProviders();
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
