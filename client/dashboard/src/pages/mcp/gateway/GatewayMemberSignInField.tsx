import { AuthRow } from "@/pages/mcp/x/tabs/settings/sections/authentication/AuthRow";
import { AUTHORIZATION_SERVERS_FIELD_ID } from "@/pages/mcp/x/tabs/settings/sections/authentication/RemoteIdentityProvidersField";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { isForbiddenError } from "@/lib/errors";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useMcpServerAuthentication } from "@gram/client/react-query/mcpServerAuthentication.js";
import { Link } from "react-router";
import { useGatewayMemberRows } from "./useGatewayMemberRows";

export function GatewayAuthorizationServersField({
  metaMcpServerId,
}: {
  metaMcpServerId: string;
}): JSX.Element {
  const { rows, isLoading, isError, refetch } =
    useGatewayMemberRows(metaMcpServerId);
  const servers = rows.flatMap((row) =>
    row.classification === "proxied" && row.server ? [row.server] : [],
  );
  return (
    <AuthRow
      id={AUTHORIZATION_SERVERS_FIELD_ID}
      label="Authorization servers"
      hint="The gateway uses each member’s own upstream configuration."
    >
      <GatewayMemberSignInField
        servers={servers}
        isLoading={isLoading}
        isError={isError}
        refetch={refetch}
      />
    </AuthRow>
  );
}

/** A proxied member brings its own upstream offers to the gateway. */
function GatewayMemberSignInField({
  servers,
  isLoading,
  isError,
  refetch,
}: {
  servers: McpServer[];
  isLoading: boolean;
  isError: boolean;
  refetch: () => Promise<unknown>;
}): JSX.Element {
  return (
    <>
      {isLoading ? (
        <Skeleton label="Loading members" className="h-9 w-64" />
      ) : isError ? (
        <div className="flex items-center gap-2">
          <Text muted small>
            Could not load gateway members.
          </Text>
          <Button variant="secondary" size="sm" onClick={() => void refetch()}>
            <Button.Text>Retry</Button.Text>
          </Button>
        </div>
      ) : servers.length === 0 ? (
        <Text muted small>
          No proxied members.
        </Text>
      ) : (
        <ul className="divide-y border">
          {servers.map((server) => (
            <MemberSignInRow key={server.id} server={server} />
          ))}
        </ul>
      )}
    </>
  );
}

function MemberSignInRow({ server }: { server: McpServer }): JSX.Element {
  const routes = useRoutes();
  const query = useMcpServerAuthentication(
    { mcpServerId: server.id },
    undefined,
    { throwOnError: false },
  );
  const href = `${routes.mcp.details.settings.href(mcpServerRouteParam(server))}#upstream-credentials`;
  const forbidden = isForbiddenError(query.error);
  const status = query.isError ? undefined : query.data?.status;
  const description = forbidden
    ? "Organization read access is required to view upstream sign-in status."
    : query.isLoading
      ? "Checking upstream sign-in…"
      : status === "configured"
        ? "Upstream credentials configured"
        : status === "no_auth_required"
          ? "No upstream credentials required"
          : status === "setup_incomplete"
            ? "Upstream credential setup incomplete"
            : "Upstream sign-in status unavailable";

  return (
    <li className="flex flex-wrap items-center justify-between gap-2 p-3">
      <div>
        <Text>{server.name || server.slug}</Text>
        <Text muted small className="block">
          {description}
        </Text>
      </div>
      <div className="flex items-center gap-2">
        {query.isError && !forbidden && (
          <Button
            variant="secondary"
            size="sm"
            disabled={query.isFetching}
            onClick={() => void query.refetch()}
          >
            <Button.Text>Retry</Button.Text>
          </Button>
        )}
        <RequireScope scope="org:admin" level="section">
          <Link className="text-sm underline" to={href}>
            {status === "setup_incomplete"
              ? "Repair upstream credentials"
              : "Manage upstream credentials"}
          </Link>
        </RequireScope>
      </div>
    </li>
  );
}
