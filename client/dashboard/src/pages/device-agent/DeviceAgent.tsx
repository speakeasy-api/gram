import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { useTelemetry } from "@/contexts/Telemetry";
import React from "react";
import { Navigate } from "react-router";
import { DeviceAgentSetup } from "./device-agent-setup";

export default function DeviceAgent(): React.JSX.Element | null {
  const telemetry = useTelemetry();
  const isDeviceAgentEnabled = telemetry.isFeatureEnabled("gram-device-agent");

  // Flags haven't resolved yet — render nothing rather than flashing a redirect.
  if (isDeviceAgentEnabled === undefined) {
    return null;
  }

  if (!isDeviceAgentEnabled) {
    return <Navigate to=".." replace />;
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope={["org:read", "org:admin"]} level="page">
          <DeviceAgentSetup />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
