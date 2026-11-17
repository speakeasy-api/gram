import { ResourceListPage } from "@/components/page-templates";
import { MCPSessionsEmptyState } from "@/components/setup-empty-state";
import { SplashScreen } from "@/components/ui/LogoLoader";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { useRoutes } from "@/routes";
import { Navigate, useLocation, useParams } from "react-router";
import {
  initialProvider,
  useLastProvider,
  useProjectProviders,
} from "./providers";

const MCP_SESSIONS_TITLE = "MCP Sessions";

/** The filter the old single list used to narrow to one provider. */
const LEGACY_PROVIDER_PARAM = "issuerId";

/**
 * `/mcp-sessions`: opens a provider's Connections. Links from People and
 * killswitches carry their filters in the query, which the Connections tab
 * reads, so the query goes along.
 */
export default function McpSessionsIndex(): JSX.Element {
  const routes = useRoutes();
  const project = useProject();
  const { search } = useLocation();
  const { providers, isLoading, isError } = useProjectProviders();
  const [last] = useLastProvider();

  if (isLoading) return <SplashScreen />;

  const params = new URLSearchParams(search);
  const requested = params.get(LEGACY_PROVIDER_PARAM);
  params.delete(LEGACY_PROVIDER_PARAM);
  const provider = initialProvider(providers, { requested, last });

  if (!provider) {
    return (
      <ResourceListPage
        scope="project:read"
        resourceId={project.id}
        title={MCP_SESSIONS_TITLE}
      >
        {isError ? (
          <Text className="text-destructive">
            Couldn&apos;t load MCP Authentication Providers. Reload the page to
            try again.
          </Text>
        ) : (
          <MCPSessionsEmptyState />
        )}
      </ResourceListPage>
    );
  }

  const query = params.toString();
  return (
    <Navigate
      to={`${routes.mcpSessions.provider.connections.href(provider.id)}${query ? `?${query}` : ""}`}
      replace
    />
  );
}

/** `/mcp-sessions/configuration`, the old provider list. */
export function LegacyMcpSessionsConfiguration(): JSX.Element {
  const routes = useRoutes();
  return <Navigate to={routes.mcpSessions.href()} replace />;
}

/** `/mcp-sessions/configuration/:issuerId`, the old provider page. */
export function LegacyMcpSessionsProviderDetail(): JSX.Element {
  const routes = useRoutes();
  const { issuerId = "" } = useParams<{ issuerId: string }>();
  return (
    <Navigate
      to={routes.mcpSessions.provider.settings.href(issuerId)}
      replace
    />
  );
}
