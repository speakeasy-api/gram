import { InlineEmptyState } from "@/components/inline-empty-state";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { useOrgRoutes, useRoutes } from "@/routes";
import { Link } from "react-router";

export function RiskSetupEmptyState({
  heading = "No risk events found",
  filtered = false,
}: { heading?: string; filtered?: boolean } = {}): JSX.Element {
  const routes = useRoutes();
  if (filtered) {
    return (
      <InlineEmptyState
        icon="shield"
        heading="No risk events match these filters"
        description="Widen the time range or clear filters to see more findings."
      />
    );
  }
  return (
    <InlineEmptyState
      icon="shield"
      heading={heading}
      description="Risk policies scan AI activity for secrets, sensitive data, and policy violations. Create a risk policy in Guardrails to start detecting risks. If you already have policies, try widening the time range or clearing filters."
      action={
        <RequireScope scope="org:admin" level="component">
          <Button asChild>
            <Link to={routes.policyCenter.href()}>Create risk policy</Link>
          </Button>
        </RequireScope>
      }
    />
  );
}

export function ShadowAISetupEmptyState({
  heading,
  description = "Shadow AI reveals AI tools and MCP servers used on your organization's devices. Install and run the device agent to discover them. Results appear after enrolled devices report a scan.",
}: {
  heading: string;
  description?: string;
}): JSX.Element {
  const routes = useOrgRoutes();
  return (
    <InlineEmptyState
      icon="laptop"
      heading={heading}
      description={description}
      action={
        <RequireScope scope="org:admin" level="component">
          <Button asChild>
            <Link to={routes.deviceAgent.href()}>Set up device agent</Link>
          </Button>
        </RequireScope>
      }
    />
  );
}

export function CostsSetupEmptyState({
  filtered = false,
}: { filtered?: boolean } = {}): JSX.Element {
  const routes = useRoutes();
  if (filtered) {
    return (
      <InlineEmptyState
        icon="chart-column"
        heading="No cost data matches these filters"
        description="Widen the time range or clear filters to see more usage."
      />
    );
  }
  return (
    <InlineEmptyState
      icon="chart-column"
      heading="No cost data yet"
      description="Connect your AI agents with OpenTelemetry to track token usage and costs. Follow the agent setup guide to configure telemetry. If your agents are already connected, try widening the time range or clearing filters."
      action={
        <Button asChild>
          <Link to={routes.plugins.href()}>Set up OpenTelemetry</Link>
        </Button>
      }
    />
  );
}

export function IdentitySyncCallout(): JSX.Element {
  const routes = useOrgRoutes();
  return (
    <InlineEmptyState
      icon="users"
      heading="Bring your team into view"
      description="Connect your identity provider and configure directory sync to see your team's identities and AI activity."
      action={
        <RequireScope scope="org:admin" level="component">
          <Button asChild>
            <Link to={routes.identity.href()}>Configure IDP sync</Link>
          </Button>
        </RequireScope>
      }
    />
  );
}

export function MCPSessionsEmptyState(): JSX.Element {
  const routes = useRoutes();
  return (
    <InlineEmptyState
      icon="plug"
      heading="No connections yet"
      description="Add an MCP server, then connect an AI agent. Connections agents establish with your MCP servers will appear here."
      action={
        <RequireScope scope="mcp:write" level="component">
          <Button asChild>
            <Link to={routes.mcp.add.href()}>Add MCP server</Link>
          </Button>
        </RequireScope>
      }
    />
  );
}
