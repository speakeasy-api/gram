import { Icon } from "@/components/ui/Icon";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { encodeIdentityUrn } from "@/lib/identity-urn";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import type { ToolUsageTraceSummary } from "@gram/client/models/components/toolusagetracesummary.js";
import type { ReactNode } from "react";
import { Link } from "react-router";

type GatewayTrace = Pick<
  ToolUsageTraceSummary,
  | "viaMetaMcpServerId"
  | "viaMetaMcpServerName"
  | "viaMetaMcpServerKind"
  | "viaMetaMcpServerProjectSlug"
  | "viaMetaMcpServerAgentId"
>;

type GatewayTargetTrace = Pick<
  ToolUsageTraceSummary,
  | "targetId"
  | "targetMetaMcpServerKind"
  | "targetMetaMcpServerProjectSlug"
  | "targetMetaMcpServerAgentId"
>;

/** Where a gateway's page is found, as a trace reports it. */
type GatewayPlace = {
  id?: string;
  kind?: string;
  projectSlug?: string;
  agentId?: string;
};

const iconClassName =
  "flex shrink-0 items-center text-[var(--color-feedback-blue-700)] dark:text-[var(--color-feedback-blue-500)]";

/**
 * The gateway that dispatched a member call, linked to the page that shows
 * the gateway and the servers behind it. Each kind of gateway has its own
 * page: a stored gateway's page lives in its own project, the organization
 * gateway's under MCP, and an agent gateway's servers are what the agent's
 * permissions reach. A gateway the API could not place stays a plain icon.
 */
export function GatewayDispatchLink({
  trace,
}: {
  trace: GatewayTrace;
}): JSX.Element | null {
  const href = useGatewayHref({
    id: trace.viaMetaMcpServerId,
    kind: trace.viaMetaMcpServerKind,
    projectSlug: trace.viaMetaMcpServerProjectSlug,
    agentId: trace.viaMetaMcpServerAgentId,
  });
  if (!trace.viaMetaMcpServerId) return null;

  const name = trace.viaMetaMcpServerName ?? trace.viaMetaMcpServerId;
  const tooltip = `Dispatched through gateway "${name}"`;

  if (!href) {
    return (
      <SimpleTooltip tooltip={tooltip}>
        <span role="img" aria-label={tooltip} className={iconClassName}>
          <Icon name="network" className="size-4" />
        </span>
      </SimpleTooltip>
    );
  }

  return (
    <SimpleTooltip tooltip={tooltip}>
      <Link
        to={href}
        onClick={(event) => event.stopPropagation()}
        aria-label={`Open gateway "${name}"`}
        className={cn(
          iconClassName,
          "hover:text-[var(--color-feedback-blue-500)] dark:hover:text-[var(--color-feedback-blue-400)]",
        )}
      >
        <Icon name="network" className="size-4" />
      </Link>
    </SimpleTooltip>
  );
}

/**
 * The name of the gateway a call reached, linked to that gateway's page the
 * same way GatewayDispatchLink links a dispatching gateway. A gateway the API
 * could not place keeps its name as plain text.
 */
export function GatewayTargetLink({
  trace,
  children,
}: {
  trace: GatewayTargetTrace;
  children: ReactNode;
}): JSX.Element {
  const href = useGatewayHref({
    id: trace.targetId,
    kind: trace.targetMetaMcpServerKind,
    projectSlug: trace.targetMetaMcpServerProjectSlug,
    agentId: trace.targetMetaMcpServerAgentId,
  });
  if (!href) return <>{children}</>;

  return (
    <Link
      to={href}
      onClick={(event) => event.stopPropagation()}
      className="hover:text-foreground hover:underline"
    >
      {children}
    </Link>
  );
}

function useGatewayHref(gateway: GatewayPlace): string | undefined {
  // The gateway's project, which need not be the one being viewed.
  const routes = useRoutes({ projectSlug: gateway.projectSlug || undefined });
  return gatewayHref(gateway, routes);
}

function gatewayHref(
  gateway: GatewayPlace,
  routes: ReturnType<typeof useRoutes>,
): string | undefined {
  switch (gateway.kind) {
    case "project":
      // Without its project the gateway, or the project, is deleted.
      if (!gateway.projectSlug || !gateway.id) return undefined;
      return routes.mcp.gateway.overview.href(gateway.id);
    case "organization":
      return routes.mcp.organizationGateway.href();
    case "agent":
      if (!gateway.agentId) return undefined;
      return routes.identities.detail.permissions.href(
        encodeIdentityUrn(`agent:${gateway.agentId}`),
      );
    // Unresolved, or a kind this dashboard does not know yet.
    case undefined:
    default:
      return undefined;
  }
}
