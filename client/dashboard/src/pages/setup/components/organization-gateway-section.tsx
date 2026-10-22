import { type ReactNode, useMemo, useState } from "react";
import { Link } from "react-router";
import type { OrganizationGateway } from "@gram/client/models/components/organizationgateway.js";
import { useOrganizationGateway } from "@gram/client/react-query/organizationGateway.js";
import { GatewayNotServed } from "@/components/mcp/organization-gateway-setting";
import { Button } from "@/components/ui/Button";
import { useOrganization } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { organizationGatewayInstallPlatforms } from "@/pages/mcp/organization-gateway-install";
import { useOrgRoutes } from "@/routes";
import { platformSteps } from "../setup-data";
import { PlatformSetupStepBody } from "./platform-setup-steps";

// The Organization Gateway as a Claude connector, inside the Anthropic admin
// controls card. The gateway read is the only gate: its URL shows once the
// server says the gateway is served. New organizations get the
// organization-wide sign-in it needs when they are created, so the only thing
// an admin does here is turn the gateway on, and that stays behind the MCP assignment review on
// the Organization Gateway page. This section never turns it on itself.
export function OrganizationGatewayConnectorSection(): JSX.Element {
  const organization = useOrganization();
  const { hasScope, isLoading: permissionsLoading } = useRBAC();
  const canRead = hasScope("org:read", organization.id);
  const canManage = hasScope("org:admin", organization.id);
  const gateway = useOrganizationGateway(undefined, undefined, {
    throwOnError: false,
    enabled: canRead,
    // Ask again on mount so the card reflects the gateway's live state rather
    // than an answer cached by the MCP list or the gateway page.
    refetchOnMount: "always",
  });

  if (permissionsLoading) {
    return <Muted>Checking your access…</Muted>;
  }
  if (!canRead) {
    return (
      <Muted>
        Ask an organization admin for the Organization Gateway URL. You need
        organization read access to see it.
      </Muted>
    );
  }
  // A cached answer (from the MCP list or the gateway page) may be stale, so
  // hold the URL back until this card's own read has come back.
  if (gateway.isLoading || !gateway.isFetchedAfterMount) {
    return <Muted>Loading the Organization Gateway…</Muted>;
  }
  const retry = (
    <Button
      variant="secondary"
      size="sm"
      disabled={gateway.isFetching}
      onClick={() => void gateway.refetch()}
    >
      {gateway.isFetching ? "Checking…" : "Check again"}
    </Button>
  );
  if (gateway.error || !gateway.data) {
    return (
      <WithAction action={retry}>
        Couldn&apos;t load the Organization Gateway.
      </WithAction>
    );
  }

  const url =
    gateway.data.available && gateway.data.url ? gateway.data.url : undefined;
  if (url) {
    return (
      <ConnectorSteps
        name={organization.slug}
        url={url}
        footer={
          <Muted>
            For Claude Code, ChatGPT and other clients, open the{" "}
            <GatewayPageLink /> page.
          </Muted>
        }
      />
    );
  }
  return (
    <GatewayNotReady
      reason={gateway.data.unavailableReason}
      canManage={canManage}
      retry={retry}
    />
  );
}

function GatewayNotReady({
  reason,
  canManage,
  retry,
}: {
  reason: OrganizationGateway["unavailableReason"];
  canManage: boolean;
  retry: JSX.Element;
}): JSX.Element {
  switch (reason) {
    case "turned_off":
      return (
        <Muted>
          The Organization Gateway is off.{" "}
          {canManage
            ? "Review MCP assignments and turn it on from the "
            : "An organization admin must review MCP assignments and turn it on from the "}
          <GatewayPageLink /> page. Its URL shows here after that.
        </Muted>
      );
    // The shared message knows where each sign-in problem is fixed and who
    // can fix it.
    case "no_organization_issuer":
    case "multiple_organization_issuers":
      return (
        <div className="space-y-3">
          <GatewayNotServed reason={reason} />
          {retry}
        </div>
      );
    case "custom_domain_only":
      return <GatewayNotServed reason={reason} />;
    case "organization_disabled":
    case undefined:
      break;
  }
  // A disabled organization, a reason this dashboard doesn't know yet, or none
  // at all: say only that it isn't available, never that it is on.
  return (
    <WithAction action={retry}>
      The Organization Gateway isn&apos;t available for your organization right
      now.
    </WithAction>
  );
}

function ConnectorSteps({
  name,
  url,
  footer,
}: {
  name: string;
  url: string;
  footer: JSX.Element;
}): JSX.Element | null {
  const platform = useMemo(
    () =>
      organizationGatewayInstallPlatforms(name, url).find(
        (p) => p.id === "claude-cowork",
      ),
    [name, url],
  );
  // The plan check decides between the organization connector (Team or
  // Enterprise) and each person's own custom connector (Pro or Max).
  const [eligible, setEligible] = useState<boolean | null>(null);
  if (!platform) return null;
  const steps = platformSteps(platform, eligible);
  const unanswered = !!steps[0]?.eligibility && eligible === null;
  const visibleSteps = unanswered ? steps.slice(0, 1) : steps;
  return (
    <div className="space-y-8">
      {visibleSteps.map((step, index) => (
        <PlatformSetupStepBody
          key={step.title}
          step={step}
          eyebrow={`Step ${index + 1}`}
          onRetryApiKey={() => {}}
          eligibleAnswer={eligible}
          onEligibilityAnswer={setEligible}
        />
      ))}
      {footer}
    </div>
  );
}

function GatewayPageLink(): JSX.Element {
  const orgRoutes = useOrgRoutes();
  return (
    <Link to={orgRoutes.gateway.href()} className="underline">
      Organization Gateway
    </Link>
  );
}

function WithAction({
  action,
  children,
}: {
  action: JSX.Element;
  children: ReactNode;
}): JSX.Element {
  return (
    <div className="flex items-center gap-3">
      <Muted>{children}</Muted>
      {action}
    </div>
  );
}

function Muted({ children }: { children: ReactNode }): JSX.Element {
  return <p className="text-muted-foreground text-sm">{children}</p>;
}
