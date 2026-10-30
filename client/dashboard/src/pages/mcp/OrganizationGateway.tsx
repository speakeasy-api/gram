import { ClientsAndSessionsTab } from "@/components/sessions/ClientsAndSessionsTab";
import {
  BRAND_MESH_SURFACE_CLASS,
  BrandMeshLayers,
} from "@/components/brand-mesh";
import {
  PageTabsList,
  PageTabsTrigger,
  Tabs,
  TabsContent,
} from "@/components/ui/Tabs";
import { cn } from "@/lib/utils";
import { MCPAssignments } from "../access/MCPAssignments";
import { GatewayAccessReview } from "./GatewayAccessReview";
import { GatewayActivitySection } from "./gateway/GatewayActivitySection";
import { SpeakeasyMark } from "@/components/mcp/OrganizationGatewayCard";
import {
  GatewayNotServed,
  OrganizationGatewaySwitch,
} from "@/components/mcp/organization-gateway-setting";
import {
  type OrganizationGatewaySetting,
  useOrganizationGatewaySetting,
} from "@/components/mcp/useOrganizationGatewaySetting";
import { SettingsPage } from "@/components/page-templates";
import { RequireScope } from "@/components/require-scope";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { useIsCurrentOrganization } from "@/hooks/useIsCurrentOrganization";
import { AgentPlatformPickerItem } from "@/pages/setup/components/agent-platform-picker-item";
import { PlatformInstrumentationSheet } from "@/pages/setup/components/platform-instrumentation-sheet";
import { useMemo, useState } from "react";
import { useSearchParams } from "react-router";
import { organizationGatewayInstallPlatforms } from "./organization-gateway-install";

/**
 * The Organization Gateway: one MCP URL that gives each person every server they can
 * reach across the organization's projects. Opened from its card at the head
 * of the MCP Servers list.
 */
export default function OrganizationGateway(): JSX.Element {
  const organization = useOrganization();
  return (
    <RequireScope scope="org:read" level="page">
      <OrganizationGatewayContent
        key={organization.id}
        organizationId={organization.id}
        organizationSlug={organization.slug}
      />
    </RequireScope>
  );
}

function OrganizationGatewayContent({
  organizationId,
  organizationSlug,
}: {
  organizationId: string;
  organizationSlug: string;
}): JSX.Element {
  const isCurrentOrganization = useIsCurrentOrganization(organizationId);
  const {
    features,
    gateway,
    on: isOn,
    setOn,
    isPending,
  } = useOrganizationGatewaySetting({ organizationId, isCurrentOrganization });

  const { hasScope, isLoading: permissionsLoading } = useRBAC();
  const canManage =
    !permissionsLoading && hasScope("org:admin", organizationId);
  const [reviewing, setReviewing] = useState(false);
  const [searchParams, setSearchParams] = useSearchParams();
  const selectedTab = searchParams.get("tab");
  const setTab = (value: string) =>
    setSearchParams(
      (previous) => {
        const next = new URLSearchParams(previous);
        next.set("tab", value);
        return next;
      },
      { replace: true },
    );
  const tab =
    (["install", "assignments", "connections", "activity"].includes(
      selectedTab ?? "",
    )
      ? selectedTab
      : undefined) ?? (isOn === false && canManage ? "assignments" : "install");
  const endpointUrl =
    isOn && gateway.data?.available ? gateway.data.url : undefined;

  let installStatus =
    "Organization Gateway installation is unavailable. Check the gateway status above.";
  if (isOn === false) {
    installStatus = canManage
      ? "Enable Organization Gateway to get your endpoint and installation steps."
      : "An organization admin must enable Organization Gateway before installation details are available.";
  } else if (gateway.isLoading || isOn === undefined) {
    installStatus = "Loading Organization Gateway installation details…";
  }

  return (
    <SettingsPage
      title={
        <span className="inline-flex items-center gap-3">
          <span className="border-border bg-card flex size-9 shrink-0 items-center justify-center border">
            <SpeakeasyMark className="size-6" />
          </span>
          Organization Gateway
        </span>
      }
      description={
        <span className="block max-w-2xl">
          One unified MCP server containing every MCP server each person can
          access across all projects. Each person signs in with their Speakeasy
          account. As their access changes, so does what they can use.
        </span>
      }
      renderHeader={(header) => (
        <section
          className={cn(
            BRAND_MESH_SURFACE_CLASS,
            "border-border mb-8 border px-6 py-6 sm:px-8 sm:py-8",
          )}
        >
          <BrandMeshLayers />
          {header}
          <section
            aria-label={endpointUrl ? "Gateway endpoint" : "Gateway status"}
            className="border-border flex min-w-0 flex-col gap-4 bg-card text-black dark:text-foreground border px-5 py-4 sm:flex-row sm:items-center"
          >
            <div className="min-w-0 flex-1">
              <GatewayBody gateway={gateway} isOn={isOn} />
            </div>
            <div className="flex shrink-0 items-stretch self-end divide-black/10 border-black/10 bg-black/5 dark:divide-border dark:border-border dark:bg-muted divide-x border sm:self-auto">
              {isOn !== undefined && !!features.data && !features.error ? (
                // Not a <label>: label activation would forward clicks on the text
                // to the switch past RequireScope's pointer-events guard.
                <div className="flex items-center gap-3 px-4 py-2">
                  <Text
                    small
                    as="span"
                    className="text-black/60 dark:text-muted-foreground"
                  >
                    {isOn ? "Enabled" : "Disabled"}
                  </Text>
                  <OrganizationGatewaySwitch
                    on={isOn}
                    setOn={(enabled, options) => {
                      if (enabled) {
                        setReviewing(true);
                        setTab("assignments");
                      } else setOn(false, options);
                    }}
                    isPending={isPending}
                  />
                </div>
              ) : undefined}
              {endpointUrl && (
                <CopyButton
                  text={endpointUrl}
                  size="lg"
                  tooltip="Copy endpoint"
                  className="h-auto shrink-0 text-black hover:bg-black/5 hover:text-black dark:text-foreground dark:hover:bg-accent dark:hover:text-foreground rounded-none"
                />
              )}
            </div>
          </section>
        </section>
      )}
    >
      {features.error && (
        <Alert variant="error">
          <AlertDescription>
            Couldn&apos;t load the gateway setting.
            <Button variant="secondary" onClick={() => void features.refetch()}>
              Retry
            </Button>
          </AlertDescription>
        </Alert>
      )}
      <div className="space-y-8">
        <Tabs value={tab} onValueChange={setTab}>
          <div className="border-border border-b">
            <PageTabsList>
              <PageTabsTrigger value="install">Install</PageTabsTrigger>
              <PageTabsTrigger value="connections">Connections</PageTabsTrigger>
              <PageTabsTrigger value="activity">Activity</PageTabsTrigger>
              <RequireScope scope="org:admin" level="component">
                <PageTabsTrigger value="assignments">
                  View assignments
                </PageTabsTrigger>
              </RequireScope>
            </PageTabsList>
          </div>
          <TabsContent value="install" className="mt-6">
            {endpointUrl ? (
              <OrganizationGatewayInstall
                serverName={organizationSlug}
                url={endpointUrl}
              />
            ) : (
              <Text small muted>
                {installStatus}
              </Text>
            )}
          </TabsContent>
          <TabsContent value="connections" className="mt-6">
            <ClientsAndSessionsTab gateway={{}} issuerId={undefined} />
          </TabsContent>
          <TabsContent value="activity" className="mt-6">
            <GatewayActivitySection
              scope={{ kind: "organization", organizationId }}
            />
          </TabsContent>
          <TabsContent value="assignments" className="mt-6">
            <RequireScope scope="org:admin" level="section">
              {reviewing &&
              isOn === false &&
              !!features.data &&
              !features.error ? (
                <GatewayAccessReview
                  isPending={isPending}
                  onCancel={() => setReviewing(false)}
                  onEnable={() =>
                    setOn(true, { onSuccess: () => setReviewing(false) })
                  }
                />
              ) : (
                <MCPAssignments />
              )}
            </RequireScope>
          </TabsContent>
        </Tabs>
      </div>
    </SettingsPage>
  );
}

function GatewayBody({
  gateway,
  isOn,
}: {
  gateway: OrganizationGatewaySetting["gateway"];
  isOn: boolean | undefined;
}): JSX.Element {
  if (gateway.isLoading) {
    return <Skeleton className="h-6 w-full" />;
  }

  if (gateway.error || !gateway.data) {
    return (
      <Alert variant="error">
        <div className="flex flex-1 items-center justify-between gap-3">
          <AlertDescription>
            Couldn&apos;t load the Organization Gateway.
          </AlertDescription>
          <Button
            variant="secondary"
            size="sm"
            disabled={gateway.isFetching}
            onClick={() => void gateway.refetch()}
          >
            {gateway.isFetching ? "Retrying…" : "Retry"}
          </Button>
        </div>
      </Alert>
    );
  }

  // The gateway's own "turned off" counts too, so a setting read that lags
  // behind it doesn't leave an unexplained "Not available".
  if (isOn === false || gateway.data.unavailableReason === "turned_off") {
    return (
      <Alert variant="warning">
        <AlertDescription>
          Organization Gateway is off. An organization admin must review MCP
          assignments before enabling this endpoint for the team.
        </AlertDescription>
      </Alert>
    );
  }

  const url = gateway.data.available ? gateway.data.url : undefined;
  if (!url) {
    return (
      <Alert variant="warning">
        <div>
          <AlertTitle>Not available</AlertTitle>
          <GatewayNotServed reason={gateway.data.unavailableReason} />
        </div>
      </Alert>
    );
  }

  return (
    <div className="min-w-0 space-y-2">
      <h2 className="text-eyebrow text-black/60 dark:text-muted-foreground">
        Endpoint URL
      </h2>
      <code className="block font-mono text-sm break-all sm:text-base">
        {url}
      </code>
    </div>
  );
}

function OrganizationGatewayInstall({
  serverName,
  url,
}: {
  serverName: string;
  url: string;
}): JSX.Element {
  const platforms = useMemo(
    () => organizationGatewayInstallPlatforms(serverName, url),
    [serverName, url],
  );
  const [openPlatformId, setOpenPlatformId] = useState<string | null>(null);
  return (
    <section>
      <Text small muted className="mb-4">
        Pick your agent for its steps. The first time it connects, it opens your
        browser to sign in.
      </Text>
      <div className="grid grid-cols-1 gap-2 lg:grid-cols-2">
        {platforms.map((platform) => (
          <AgentPlatformPickerItem
            key={platform.id}
            platformId={platform.icon}
            name={platform.name}
            description={platform.description}
            onClick={() => setOpenPlatformId(platform.id)}
          />
        ))}
      </div>
      <PlatformInstrumentationSheet
        open={openPlatformId !== null}
        onOpenChange={(open) => {
          if (!open) setOpenPlatformId(null);
        }}
        platforms={platforms}
        initialPlatformId={openPlatformId}
      />
    </section>
  );
}
