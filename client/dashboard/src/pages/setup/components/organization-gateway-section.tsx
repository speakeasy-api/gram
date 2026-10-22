import { type ReactNode, useMemo, useState } from "react";
import { Link } from "react-router";
import type { OrganizationGateway } from "@gram/client/models/components/organizationgateway.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useOrganizationGateway } from "@gram/client/react-query/organizationGateway.js";
import { useQuery } from "@tanstack/react-query";
import { buildOrganizationSetupTasksQuery } from "@/hooks/useOrganizationSetupTasks";
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
// server says the gateway is served. The organization-wide sign-in it needs is
// created when an organization admin opens this card, so the only thing an admin does here
// is turn the gateway on, and that stays behind the MCP assignment review on
// the Organization Gateway page. This section never turns it on itself.
export function OrganizationGatewayConnectorSection(): JSX.Element {
  const organization = useOrganization();
  const { hasScope, isLoading: permissionsLoading } = useRBAC();
  const canRead = hasScope("org:read", organization.id);
  const canManage = hasScope("org:admin", organization.id);
  // Listing setup tasks as an organization admin creates the sign-in before
  // the response returns. Share the wizard's query (same key, no extra fetch
  // on mount) and read the gateway only once that list has settled, so a
  // direct link to this card can't race the sign-in being created.
  const client = useGramContext();
  const setupTasks = useQuery({
    ...buildOrganizationSetupTasksQuery(client, organization.id, false, {
      retry: false,
    }),
    refetchOnMount: false,
  });
  const setupSettled = setupTasks.isFetched && !setupTasks.isFetching;
  const gateway = useOrganizationGateway(undefined, undefined, {
    throwOnError: false,
    enabled: canRead && setupSettled,
    // An answer cached elsewhere (the MCP list, the gateway page) may predate
    // the sign-in, so this card always asks again.
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
  if (!setupSettled || gateway.isLoading || !gateway.isFetchedAfterMount) {
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
    // Listing setup tasks as an organization admin creates the sign-in, so an
    // admin on this card only waits; anyone else needs an admin to open it.
    case "no_organization_issuer":
      return canManage ? (
        <WithAction action={retry}>
          The organization-wide sign-in the gateway uses isn&apos;t ready yet.
          Check again in a moment, or contact support if this persists.
        </WithAction>
      ) : (
        <WithAction action={retry}>
          The gateway needs an organization-wide sign-in. An organization admin
          creates it by opening this setup card.
        </WithAction>
      );
    case "multiple_organization_issuers":
      return (
        <Muted>
          The gateway needs exactly one organization-wide sign-in, and your
          organization has more than one. Contact support to resolve this.
        </Muted>
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
