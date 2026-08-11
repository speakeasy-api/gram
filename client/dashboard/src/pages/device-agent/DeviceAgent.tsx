import { PageEyebrow } from "@/components/page-eyebrow";
import { Page } from "@/components/page-layout";
import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/Heading";
import {
  PageTabsList,
  PageTabsTrigger,
  Tabs,
  TabsContent,
} from "@/components/ui/Tabs";
import { Text } from "@/components/ui/Text";
import { useTelemetry } from "@/contexts/Telemetry";
import { MdmIntegrationsTab } from "@/pages/org/device-integrations/DeviceIntegrations";
import React from "react";
import { Navigate, Outlet, useLocation, useNavigate } from "react-router";
import { DeviceAgentConfigurationTab } from "./device-agent-configuration";
import { DeviceAgentSetup } from "./device-agent-setup";

// Route shell: subPages (the MDM tab and provider detail pages) render
// through the Outlet; the whole surface gates on the device-agent flag.
export function DeviceAgentRoot(): React.JSX.Element | null {
  const telemetry = useTelemetry();
  const isDeviceAgentEnabled = telemetry.isFeatureEnabled("gram-device-agent");

  // Flags haven't resolved yet — render nothing rather than flashing a redirect.
  if (isDeviceAgentEnabled === undefined) {
    return null;
  }

  if (!isDeviceAgentEnabled) {
    return <Navigate to=".." replace />;
  }

  return <Outlet />;
}

export default function DeviceAgent(): React.JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope={["org:read", "org:admin"]} level="page">
          <DeviceAgentTabs />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

// Tab shell shared by /device-agent (Setup) and
// /device-agent/mdm-integrations (the MDM catalog). The per-provider detail
// pages route to their own component below this shell.
function DeviceAgentTabs() {
  const location = useLocation();
  const navigate = useNavigate();
  const telemetry = useTelemetry();
  const mdmFlag = telemetry.isFeatureEnabled("gram-device-integrations");
  const mdmEnabled = mdmFlag ?? false;

  // filter(Boolean) normalizes trailing slashes so a bookmarked
  // /device-agent/mdm-integrations/ still lands on the MDM tab and tab
  // switches never build double-slash URLs.
  const segments = location.pathname.split("/").filter(Boolean);
  const lastSegment = segments[segments.length - 1] ?? "";
  const onConfigurationTab = lastSegment === "configuration";
  const onMdmTab = lastSegment === "mdm-integrations";
  let currentTab = "setup";
  if (onConfigurationTab) {
    currentTab = "configuration";
  } else if (onMdmTab && mdmEnabled) {
    currentTab = "mdm-integrations";
  }
  const basePath =
    "/" +
    (onMdmTab || onConfigurationTab ? segments.slice(0, -1) : segments).join(
      "/",
    );

  const handleTabChange = (value: string) => {
    void navigate(value === "setup" ? basePath : `${basePath}/${value}`);
  };

  // A deep link to the MDM tab while the rollout flag is off must not strand
  // the browser on an MDM URL rendering the Setup tab. Only redirect once the
  // flag has actually resolved to false.
  if (onMdmTab && mdmFlag === false) {
    return <Navigate to={basePath} replace />;
  }

  return (
    <>
      <div className="mb-6">
        <PageEyebrow className="mb-2" />
        <Heading variant="h4" className="mb-2 text-display-sm font-thin">
          Device Agent
        </Heading>
        <Text muted small className="mt-1">
          Install and manage the on-device agent that enforces your
          organization's AI-tool plugins and MCP configuration.
        </Text>
      </div>
      <Tabs value={currentTab} onValueChange={handleTabChange}>
        <div className="border-border -mx-8 border-b px-8">
          <PageTabsList>
            <PageTabsTrigger value="setup">Setup</PageTabsTrigger>
            <PageTabsTrigger value="configuration">
              <span className="inline-flex items-center gap-2">
                Configuration
                <ReleaseStageBadge stage="preview" noTooltip />
              </span>
            </PageTabsTrigger>
            {mdmEnabled && (
              <PageTabsTrigger value="mdm-integrations">
                <span className="inline-flex items-center gap-2">
                  MDM Integrations
                  <ReleaseStageBadge stage="preview" noTooltip />
                </span>
              </PageTabsTrigger>
            )}
          </PageTabsList>
        </div>

        <TabsContent value="setup" className="mt-6">
          <DeviceAgentSetup />
        </TabsContent>

        <TabsContent value="configuration" className="mt-6">
          <DeviceAgentConfigurationTab />
        </TabsContent>

        {mdmEnabled && (
          <TabsContent value="mdm-integrations" className="mt-6">
            <MdmIntegrationsTab />
          </TabsContent>
        )}
      </Tabs>
    </>
  );
}
