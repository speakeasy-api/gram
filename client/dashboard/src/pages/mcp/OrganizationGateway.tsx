import { MCPAssignments } from "../access/MCPAssignments";
import { GatewayAccessReview } from "./GatewayAccessReview";
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
import { useIsCurrentOrganization } from "@/hooks/useIsCurrentOrganization";
import { AgentPlatformPickerItem } from "@/pages/setup/components/agent-platform-picker-item";
import { PlatformInstrumentationSheet } from "@/pages/setup/components/platform-instrumentation-sheet";
import { useMemo, useState } from "react";
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

  const [reviewing, setReviewing] = useState(false);

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
      primaryAction={
        isOn !== undefined && !!features.data && !features.error ? (
          // Not a <label>: label activation would forward clicks on the text
          // to the switch past RequireScope's pointer-events guard.
          <div className="flex items-center gap-3">
            <Text small muted as="span">
              {isOn ? "Enabled" : "Off"}
            </Text>
            <OrganizationGatewaySwitch
              on={isOn}
              setOn={(enabled, options) => {
                if (enabled) setReviewing(true);
                else setOn(false, options);
              }}
              isPending={isPending}
            />
          </div>
        ) : undefined
      }
    >
      {reviewing && isOn === false && !!features.data && !features.error && (
        <RequireScope scope="org:admin" level="section">
          <GatewayAccessReview
            isPending={isPending}
            onCancel={() => setReviewing(false)}
            onEnable={() =>
              setOn(true, { onSuccess: () => setReviewing(false) })
            }
          />
        </RequireScope>
      )}
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
      {!reviewing && (
        <div className="space-y-8">
          <GatewayBody
            gateway={gateway}
            isOn={isOn}
            serverName={organizationSlug}
          />
          <RequireScope scope="org:admin" level="section">
            <section aria-label="MCP assignments" className="space-y-4">
              <Text variant="subheading">MCP assignments</Text>
              <MCPAssignments />
            </section>
          </RequireScope>
        </div>
      )}
    </SettingsPage>
  );
}

function GatewayBody({
  gateway,
  isOn,
  serverName,
}: {
  gateway: OrganizationGatewaySetting["gateway"];
  isOn: boolean | undefined;
  serverName: string;
}): JSX.Element {
  if (gateway.isLoading) {
    return (
      <div className="space-y-8">
        <Skeleton className="h-16 w-full" />
        <Skeleton className="h-72 w-full" />
      </div>
    );
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
    <div className="space-y-10">
      <section>
        <h2 className="text-eyebrow mb-3">Endpoint</h2>
        <div className="border-border bg-card flex items-center gap-3 border px-4 py-3">
          <Text className="min-w-0 flex-1 truncate font-mono text-sm">
            {url}
          </Text>
          <CopyButton text={url} size="sm" tooltip="Copy endpoint" />
        </div>
      </section>

      <OrganizationGatewayInstall serverName={serverName} url={url} />
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
      <h2 className="text-eyebrow">Install</h2>
      <Text small muted className="mt-1 mb-4">
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
