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
import { useRBAC } from "@/hooks/useRBAC";
import { MdmIntegrationsTab } from "@/pages/org/device-integrations/DeviceIntegrations";
import React from "react";
import { Navigate, Outlet, useLocation, useNavigate } from "react-router";
import { AiScanTargetsSection } from "./ai-scan-targets";
import { DeviceAgentConfigurationTab } from "./device-agent-configuration";
import { DeviceAgentDevicesTab } from "./device-agent-devices";
import { DeviceAgentSetup } from "./device-agent-setup";

// Route shell shared by the setup, devices, configuration, and MDM pages.
export function DeviceAgentRoot(): React.JSX.Element {
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

  // Fleet configuration is org-admin only. Org readers (plain members) can
  // reach the Setup tab but must not see or open the Configuration tab; the
  // server enforces the same org:admin gate on agent.getConfiguration.
  const { hasScope, isLoading: rbacLoading } = useRBAC();
  const canConfigure = hasScope("org:admin");

  // filter(Boolean) normalizes trailing slashes so a bookmarked
  // /device-agent/mdm-integrations/ still lands on the MDM tab and tab
  // switches never build double-slash URLs.
  const segments = location.pathname.split("/").filter(Boolean);
  const lastSegment = segments[segments.length - 1] ?? "";
  const onDevicesTab = lastSegment === "devices";
  const onConfigurationTab = lastSegment === "configuration";
  const onScanTargetsTab = lastSegment === "scan-targets";
  const onMdmTab = lastSegment === "mdm-integrations";
  let currentTab = "setup";
  if (onDevicesTab) {
    currentTab = "devices";
  } else if (onConfigurationTab && canConfigure) {
    currentTab = "configuration";
  } else if (onScanTargetsTab && canConfigure) {
    currentTab = "scan-targets";
  } else if (onMdmTab) {
    currentTab = "mdm-integrations";
  }
  const onSubTab =
    onDevicesTab || onMdmTab || onConfigurationTab || onScanTargetsTab;
  const basePath =
    "/" + (onSubTab ? segments.slice(0, -1) : segments).join("/");

  const handleTabChange = (value: string) => {
    void navigate(value === "setup" ? basePath : `${basePath}/${value}`);
  };

  // Same guard for a deep link to the Configuration tab as a non-admin: only
  // redirect once grants have resolved, so an admin refreshing the URL isn't
  // bounced while RBAC is still loading.
  if (
    (onConfigurationTab || onScanTargetsTab) &&
    !rbacLoading &&
    !canConfigure
  ) {
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
            <PageTabsTrigger value="devices">Devices</PageTabsTrigger>
            {canConfigure && (
              <PageTabsTrigger value="configuration">
                Configuration
              </PageTabsTrigger>
            )}
            {canConfigure && (
              <PageTabsTrigger value="scan-targets">
                <span className="inline-flex items-center gap-2">
                  Scan Targets
                  <ReleaseStageBadge stage="preview" noTooltip />
                </span>
              </PageTabsTrigger>
            )}
            <PageTabsTrigger value="mdm-integrations">
              <span className="inline-flex items-center gap-2">
                MDM Integrations
                <ReleaseStageBadge stage="preview" noTooltip />
              </span>
            </PageTabsTrigger>
          </PageTabsList>
        </div>

        <TabsContent value="setup" className="mt-6">
          <DeviceAgentSetup />
        </TabsContent>

        <TabsContent value="devices" className="mt-6">
          <DeviceAgentDevicesTab />
        </TabsContent>

        {canConfigure && (
          <TabsContent value="configuration" className="mt-6">
            <DeviceAgentConfigurationTab />
          </TabsContent>
        )}

        {canConfigure && (
          <TabsContent value="scan-targets" className="mt-6">
            <AiScanTargetsSection />
          </TabsContent>
        )}

        <TabsContent value="mdm-integrations" className="mt-6">
          <MdmIntegrationsTab />
        </TabsContent>
      </Tabs>
    </>
  );
}
