import { RemoteIdentityProvidersField } from "@/pages/mcp/x/tabs/settings/sections/authentication/RemoteIdentityProvidersField";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { buildServerClientAssociationsQuery } from "@gram/client/react-query/serverClientAssociations.core.js";
import { useQueries } from "@tanstack/react-query";
import { type ComponentProps, useMemo } from "react";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useMcpServerAuthentication } from "@gram/client/react-query/mcpServerAuthentication.js";
import { Link } from "react-router";
import { useGatewayMemberRows } from "./useGatewayMemberRows";

export function GatewayAuthorizationServersField({
  metaMcpServerId,
  ...field
}: Omit<
  ComponentProps<typeof RemoteIdentityProvidersField>,
  "members" | "hiddenIssuerIds"
> & { metaMcpServerId: string }): JSX.Element {
  const { rows, isLoading, isError, refetch } =
    useGatewayMemberRows(metaMcpServerId);
  const servers = rows.flatMap((row) =>
    row.classification === "proxied" && row.server ? [row.server] : [],
  );
  const legacy = useLegacyProviderVisibility(servers);
  return (
    <RemoteIdentityProvidersField
      {...field}
      isLoading={field.isLoading || isLoading || legacy.isLoading}
      hiddenIssuerIds={legacy.issuerIds}
      members={
        <GatewayMemberSignInField
          servers={servers}
          isLoading={isLoading}
          isError={isError}
          refetch={refetch}
        />
      }
    />
  );
}

/** Preserve the legacy provider controls' visibility until hosted migration. */
function useLegacyProviderVisibility(servers: McpServer[]) {
  const client = useGramContext();
  const results = useQueries({
    queries: servers.map((server) => ({
      ...buildServerClientAssociationsQuery(client, {
        mcpServerId: server.id,
      }),
      throwOnError: false,
    })),
  });
  const ids = results.flatMap(
    (result) =>
      result.data?.associations.map((a) => a.remoteSessionIssuerId) ?? [],
  );
  const key = ids.join(",");
  // oxlint-disable-next-line react-hooks/exhaustive-deps -- keyed by content, not by the per-render array
  const issuerIds = useMemo(() => new Set(ids), [key]);
  return {
    issuerIds,
    isLoading: results.some((result) => result.isLoading),
  };
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
  const status = query.isError ? undefined : query.data?.status;
  const description = query.isLoading
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
        {query.isError && (
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
