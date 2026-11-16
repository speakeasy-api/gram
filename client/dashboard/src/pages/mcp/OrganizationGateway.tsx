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
import { useRoutes } from "@/routes";
import { MCPAssignments } from "../access/MCPAssignments";
import { GatewayActivitySection } from "./gateway/GatewayActivitySection";
import { SpeakeasyMark } from "@/components/mcp/ProjectGatewayCard";
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
import { useOrganization, useProject } from "@/contexts/Auth";
import { useProjectGateway } from "@gram/client/react-query/projectGateway.js";
import type { OrganizationGateway } from "@gram/client/models/components/organizationgateway.js";
import { useRBAC } from "@/hooks/useRBAC";
import { useIsCurrentOrganization } from "@/hooks/useIsCurrentOrganization";
import { AgentPlatformPickerItem } from "@/pages/setup/components/agent-platform-picker-item";
import { PlatformInstrumentationSheet } from "@/pages/setup/components/platform-instrumentation-sheet";
import { useMemo, useState } from "react";
import { useSearchParams } from "react-router";
import { organizationGatewayInstallPlatforms } from "./organization-gateway-install";

/**
 * The Organization Gateway: one MCP URL that gives each person every server they can
 * reach across the organization's projects. Opened from the organization's
 * settings. It is giving way to Project Gateways, so the page is its endpoint,
 * its switch, and a pointer to them.
 */
export default function OrganizationGateway(): JSX.Element {
  const organization = useOrganization();
  return (
    <RequireScope scope="org:read" level="page">
      <OrganizationGatewayContent
        key={organization.id}
        organizationId={organization.id}
      />
    </RequireScope>
  );
}

function OrganizationGatewayContent({
  organizationId,
}: {
  organizationId: string;
}): JSX.Element {
  const isCurrentOrganization = useIsCurrentOrganization(organizationId);
  const setting = useOrganizationGatewaySetting({
    organizationId,
    isCurrentOrganization,
  });
  const { gateway, on: isOn } = setting;
  const endpointUrl =
    isOn && gateway.data?.available ? gateway.data.url : undefined;

  return (
    <SettingsPage
      title={<GatewayTitle name="Organization Gateway" />}
      description={
        <span className="block max-w-2xl">
          One unified MCP server containing every MCP server each person can
          access across all projects. Each person signs in with their Speakeasy
          account. As their access changes, so does what they can use.
        </span>
      }
      renderHeader={(header) => (
        <>
          <MoveToProjectGateways />
          <GatewayHero
            header={header}
            endpointUrl={endpointUrl}
            status={
              <GatewayBody
                read={gateway}
                isOn={isOn}
                endpointUrl={endpointUrl}
                name="Organization Gateway"
                offMessage="Organization Gateway is off. An organization admin can turn it on for the team."
              />
            }
            setting={setting}
          />
        </>
      )}
    >
      <SettingLoadError setting={setting} />
    </SettingsPage>
  );
}

/**
 * The Project Gateway: the Organization Gateway limited to one project's
 * servers. Opened from its card at the head of the MCP Servers list. Its
 * endpoint, connections and activity need only access to the project; the
 * switch, which turns every gateway on or off, needs organization access.
 */
export function ProjectGatewayPage(): JSX.Element {
  const organization = useOrganization();
  const project = useProject();
  return (
    <RequireScope scope="project:read" level="page">
      <ProjectGatewayContent
        key={`${organization.id}/${project.id}`}
        organizationId={organization.id}
        project={{ id: project.id, name: project.name, slug: project.slug }}
      />
    </RequireScope>
  );
}

function ProjectGatewayContent({
  organizationId,
  project,
}: {
  organizationId: string;
  project: { id: string; name: string; slug: string };
}): JSX.Element {
  const { hasScope, isLoading: permissionsLoading } = useRBAC();
  const canReadOrganization =
    !permissionsLoading && hasScope("org:read", organizationId);
  const canManage =
    !permissionsLoading && hasScope("org:admin", organizationId);
  const isCurrentOrganization = useIsCurrentOrganization(organizationId);
  const setting = useOrganizationGatewaySetting({
    organizationId,
    isCurrentOrganization,
    enabled: canReadOrganization,
  });
  const gateway = useProjectGateway(undefined, undefined, {
    throwOnError: false,
  });
  // The stored setting decides when it can be read; otherwise the project
  // gateway's own answer does.
  const isOn = canReadOrganization
    ? setting.on
    : gateway.data?.available
      ? true
      : gateway.data?.unavailableReason === "turned_off"
        ? false
        : undefined;
  const endpointUrl =
    isOn && gateway.data?.available ? gateway.data.url : undefined;

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

  let installStatus =
    "Project Gateway installation is unavailable. Check the gateway status above.";
  if (isOn === false) {
    installStatus = canManage
      ? "Turn on gateways to get your Project Gateway endpoint and installation steps."
      : "An organization admin must turn on gateways before Project Gateway installation details are available.";
  } else if (gateway.isLoading || isOn === undefined) {
    installStatus = "Loading Project Gateway installation details…";
  }

  return (
    <SettingsPage
      title={<GatewayTitle name="Project Gateway" />}
      description={
        <span className="block max-w-2xl">
          One unified MCP server containing every MCP server in {project.name}{" "}
          each person can access. Each person signs in with their Speakeasy
          account. As their access changes, so does what they can use.
        </span>
      }
      renderHeader={(header) => (
        <GatewayHero
          header={header}
          endpointUrl={endpointUrl}
          status={
            <GatewayBody
              read={gateway}
              isOn={isOn}
              endpointUrl={endpointUrl}
              name="Project Gateway"
              offMessage="Gateways are off for every project. An organization admin can turn them on for the team."
            />
          }
          setting={canReadOrganization ? setting : undefined}
        />
      )}
    >
      {canReadOrganization && <SettingLoadError setting={setting} />}
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
                serverName={project.slug}
                url={endpointUrl}
              />
            ) : (
              <Text small muted>
                {installStatus}
              </Text>
            )}
          </TabsContent>
          <TabsContent value="connections" className="mt-6">
            <ClientsAndSessionsTab
              gateway={{ projectId: project.id }}
              issuerId={undefined}
            />
          </TabsContent>
          <TabsContent value="activity" className="mt-6">
            <GatewayActivitySection
              scope={{ kind: "project", projectId: project.id }}
            />
          </TabsContent>
          <TabsContent value="assignments" className="mt-6">
            <RequireScope scope="org:admin" level="section">
              <MCPAssignments />
            </RequireScope>
          </TabsContent>
        </Tabs>
      </div>
    </SettingsPage>
  );
}

function GatewayTitle({ name }: { name: string }): JSX.Element {
  return (
    <span className="inline-flex items-center gap-3">
      <span className="border-border bg-card flex size-9 shrink-0 items-center justify-center border">
        <SpeakeasyMark className="size-6" />
      </span>
      {name}
    </span>
  );
}

/**
 * The page header over the brand mesh: the title, then the gateway's status
 * or endpoint beside the switch and copy button.
 */
function GatewayHero({
  header,
  endpointUrl,
  status,
  setting,
}: {
  header: React.ReactNode;
  endpointUrl: string | undefined;
  status: React.ReactNode;
  /** The organization's switch; absent for a reader who cannot see it. */
  setting: OrganizationGatewaySetting | undefined;
}): JSX.Element {
  const showSwitch =
    !!setting &&
    setting.on !== undefined &&
    !!setting.features.data &&
    !setting.features.error;
  return (
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
        <div className="min-w-0 flex-1">{status}</div>
        <div className="flex shrink-0 items-stretch self-end divide-black/10 border-black/10 bg-black/5 dark:divide-border dark:border-border dark:bg-muted divide-x border sm:self-auto">
          {showSwitch ? (
            // Not a <label>: label activation would forward clicks on the text
            // to the switch past RequireScope's pointer-events guard.
            <div className="flex items-center gap-3 px-4 py-2">
              <Text
                small
                as="span"
                className="text-black/60 dark:text-muted-foreground"
              >
                {setting.on ? "Enabled" : "Disabled"}
              </Text>
              <OrganizationGatewaySwitch
                on={!!setting.on}
                setOn={setting.setOn}
                isPending={setting.isPending}
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
  );
}

function SettingLoadError({
  setting,
}: {
  setting: OrganizationGatewaySetting;
}): JSX.Element | null {
  if (!setting.features.error) return null;
  return (
    <Alert variant="error">
      <AlertDescription>
        Couldn&apos;t load the gateway setting.
        <Button
          variant="secondary"
          onClick={() => void setting.features.refetch()}
        >
          Retry
        </Button>
      </AlertDescription>
    </Alert>
  );
}

/**
 * The Organization Gateway is giving way to each project's own gateway, so its
 * page points there instead of carrying installation, connections or activity.
 */
function MoveToProjectGateways(): JSX.Element {
  const routes = useRoutes();
  return (
    <Alert className="mb-8">
      <div className="flex flex-1 flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <AlertTitle>Use Project Gateways instead</AlertTitle>
          <AlertDescription>
            Each project has its own gateway: every server in that project at
            one URL, with its own connections and activity. Connect agents to a
            Project Gateway rather than this one.
          </AlertDescription>
        </div>
        <Button
          variant="secondary"
          size="sm"
          onClick={() => routes.mcp.projectGateway.goTo()}
        >
          Open Project Gateway
        </Button>
      </div>
    </Alert>
  );
}

/** Either gateway read: the organization's or the project's. */
type GatewayRead = {
  isLoading: boolean;
  isFetching: boolean;
  error: unknown;
  data:
    | {
        available: boolean;
        url?: string | undefined;
        unavailableReason?: OrganizationGateway["unavailableReason"];
      }
    | undefined;
  refetch: () => Promise<unknown>;
};

function GatewayBody({
  read,
  isOn,
  endpointUrl,
  name,
  offMessage,
}: {
  read: GatewayRead;
  isOn: boolean | undefined;
  endpointUrl: string | undefined;
  name: string;
  offMessage: string;
}): JSX.Element {
  if (read.isLoading) {
    return <Skeleton className="h-6 w-full" />;
  }

  if (read.error || !read.data) {
    return (
      <Alert variant="error">
        <div className="flex flex-1 items-center justify-between gap-3">
          <AlertDescription>Couldn&apos;t load the {name}.</AlertDescription>
          <Button
            variant="secondary"
            size="sm"
            disabled={read.isFetching}
            onClick={() => void read.refetch()}
          >
            {read.isFetching ? "Retrying…" : "Retry"}
          </Button>
        </div>
      </Alert>
    );
  }

  // The gateway's own "turned off" counts too, so a setting read that lags
  // behind it doesn't leave an unexplained "Not available".
  if (isOn === false || read.data.unavailableReason === "turned_off") {
    return (
      <Alert variant="warning">
        <AlertDescription>{offMessage}</AlertDescription>
      </Alert>
    );
  }

  const url = read.data.available ? endpointUrl : undefined;
  if (!url) {
    return (
      <Alert variant="warning">
        <div>
          <AlertTitle>Not available</AlertTitle>
          <GatewayNotServed reason={read.data.unavailableReason} />
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
