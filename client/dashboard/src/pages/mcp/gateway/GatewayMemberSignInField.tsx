import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { StatusDot } from "@/components/ui/StatusDot";
import { Text } from "@/components/ui/Text";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { ServerClientAssociation } from "@gram/client/models/components/serverclientassociation.js";
import { useAttachUserSessionIssuerMutation } from "@gram/client/react-query/attachUserSessionIssuer.js";
import { useDetachUserSessionIssuerMutation } from "@gram/client/react-query/detachUserSessionIssuer.js";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { SourceMcpIcon } from "@/components/sources/SourceCard";
import { X } from "lucide-react";
import { type ComponentProps, useMemo, useState } from "react";
import { RemoteIdentityProvidersField } from "@/pages/mcp/x/tabs/settings/sections/authentication/RemoteIdentityProvidersField";
import { invalidateAllRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients.js";
import {
  invalidateAllServerClientAssociations,
  useServerClientAssociations,
} from "@gram/client/react-query/serverClientAssociations.js";
import { useQueries, useQueryClient } from "@tanstack/react-query";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { buildServerClientAssociationsQuery } from "@gram/client/react-query/serverClientAssociations.core.js";
import { Link } from "react-router";
import { toast } from "sonner";
import { useAllRemoteSessionClients } from "@/lib/remote-identity/queries/useAllRemoteSessionClients";
import { useGatewayMemberRows } from "./useGatewayMemberRows";

/**
 * A gateway's Authorization servers row: one row per member that signs people
 * in, each linked or not, and any other linked server once below them.
 */
export function GatewayAuthorizationServersField({
  metaMcpServerId,
  userSessionIssuerId,
  linkedClientIds,
  ...field
}: Omit<
  ComponentProps<typeof RemoteIdentityProvidersField>,
  "members" | "hiddenIssuerIds"
> & {
  metaMcpServerId: string;
  userSessionIssuerId: string;
  linkedClientIds: ReadonlySet<string>;
}): JSX.Element {
  const signing = useSigningMembers(metaMcpServerId);
  return (
    <RemoteIdentityProvidersField
      {...field}
      hiddenIssuerIds={signing.issuerIds}
      members={
        <GatewayMemberSignInList
          servers={signing.servers}
          isLoading={signing.isLoading}
          userSessionIssuerId={userSessionIssuerId}
          linkedClientIds={linkedClientIds}
          isLoadingLinked={field.isLoading}
          projectId={field.projectId}
          readOnly={field.readOnly ?? false}
        />
      }
    />
  );
}

/** The gateway's proxied members: the ones that can sign people in upstream. */
function useProxiedMembers(metaMcpServerId: string): {
  servers: McpServer[];
  isLoading: boolean;
} {
  const { rows, isLoading } = useGatewayMemberRows(metaMcpServerId);
  return {
    servers: rows.flatMap((row) =>
      row.classification === "proxied" && row.server ? [row.server] : [],
    ),
    isLoading,
  };
}

/**
 * The gateway's members that sign people in through their own service (those
 * with an upstream client), and the authorization servers they use, so the
 * row lists each member once and each server once.
 */
function useSigningMembers(metaMcpServerId: string): {
  servers: McpServer[];
  issuerIds: ReadonlySet<string>;
  isLoading: boolean;
} {
  const client = useGramContext();
  const { servers, isLoading } = useProxiedMembers(metaMcpServerId);
  const results = useQueries({
    queries: servers.map((server) => ({
      ...buildServerClientAssociationsQuery(client, {
        mcpServerId: server.id,
      }),
      throwOnError: false,
    })),
  });
  const signing = servers.filter(
    // A failed read keeps its row, which offers Try again.
    (_, index) =>
      results[index]?.isError ||
      (results[index]?.data?.associations.length ?? 0) > 0,
  );
  const ids = results.flatMap(
    (result) =>
      result.data?.associations.map((a) => a.remoteSessionIssuerId) ?? [],
  );
  const key = ids.join(",");
  // oxlint-disable-next-line react-hooks/exhaustive-deps -- keyed by content, not by the per-render array
  const issuerIds = useMemo(() => new Set(ids), [key]);
  return {
    servers: signing,
    issuerIds,
    isLoading: isLoading || results.some((result) => result.isLoading),
  };
}

/**
 * One row per member that signs people in through its own service, with the
 * one action that makes it work through this gateway: link its sign-in, or
 * unlink it. Members that need no sign-in are left out; with none, one plain
 * line says so.
 */
function GatewayMemberSignInList({
  servers,
  isLoading,
  userSessionIssuerId,
  linkedClientIds,
  isLoadingLinked,
  projectId,
  readOnly,
}: {
  servers: McpServer[];
  isLoading: boolean;
  userSessionIssuerId: string;
  /** Upstream clients already linked to the gateway's sign-in. */
  linkedClientIds: ReadonlySet<string>;
  isLoadingLinked: boolean;
  projectId: string;
  readOnly: boolean;
}): JSX.Element {
  if (isLoading)
    return <Skeleton label="Loading members" className="h-9 w-64" />;
  if (servers.length === 0)
    return (
      <Text muted small className="block">
        No member signs people in through its own service, so there is nothing
        to link.
      </Text>
    );
  return (
    <ul className="divide-y border">
      {servers.map((server) => (
        <MemberSignInRow
          key={server.id}
          server={server}
          userSessionIssuerId={userSessionIssuerId}
          linkedClientIds={linkedClientIds}
          isLoadingLinked={isLoadingLinked}
          projectId={projectId}
          readOnly={readOnly}
        />
      ))}
    </ul>
  );
}

function MemberSignInRow({
  server,
  userSessionIssuerId,
  linkedClientIds,
  isLoadingLinked,
  projectId,
  readOnly,
}: {
  server: McpServer;
  userSessionIssuerId: string;
  linkedClientIds: ReadonlySet<string>;
  isLoadingLinked: boolean;
  projectId: string;
  readOnly: boolean;
}): JSX.Element | null {
  const routes = useRoutes();
  const queryClient = useQueryClient();
  const associations = useServerClientAssociations(
    { mcpServerId: server.id },
    undefined,
    { throwOnError: false },
  );
  const attach = useAttachUserSessionIssuerMutation();
  const detach = useDetachUserSessionIssuerMutation();
  const [confirmUnlink, setConfirmUnlink] = useState(false);
  // The client the member signs people in with on its own. A member can hold
  // several clients for one upstream; linking more than one would leave the
  // gateway unable to choose, so it links only this one.
  const own = useAllRemoteSessionClients(
    { userSessionIssuerId: server.userSessionIssuerId ?? undefined },
    { enabled: !!server.userSessionIssuerId },
  );

  const clients = associations.data?.associations ?? [];
  // A member with no upstream client signs nobody in: nothing to link.
  if (associations.isSuccess && clients.length === 0) return null;

  const linked = clients.find((client) =>
    linkedClientIds.has(client.remoteSessionClientId),
  );
  const name = server.name || server.slug;
  const ownIds = new Set(own.items.map((client) => client.id));
  const choice =
    clients.find((client) => ownIds.has(client.remoteSessionClientId)) ??
    (clients.length === 1 ? clients[0] : undefined);
  const memberSignInHref = `${routes.mcp.details.settings.href(mcpServerRouteParam(server))}#settings-sign-in`;

  const link = async (client: ServerClientAssociation) => {
    try {
      await attach.mutateAsync({
        request: {
          attachUserSessionIssuerForm: {
            id: client.remoteSessionClientId,
            userSessionIssuerId,
          },
        },
      });
      await Promise.all([
        invalidateAllRemoteSessionClients(queryClient),
        invalidateAllServerClientAssociations(queryClient),
      ]);
      toast.success(`${name}'s sign-in is linked to this gateway`);
    } catch (error) {
      toast.error(
        error instanceof Error
          ? `Couldn't link ${name}'s sign-in. ${error.message}`
          : `Couldn't link ${name}'s sign-in.`,
      );
    }
  };

  const unlink = async (client: ServerClientAssociation) => {
    try {
      // The generated detach request reuses the attach form's field name.
      await detach.mutateAsync({
        request: {
          attachUserSessionIssuerForm: {
            id: client.remoteSessionClientId,
            userSessionIssuerId,
          },
        },
      });
      await Promise.all([
        invalidateAllRemoteSessionClients(queryClient),
        invalidateAllServerClientAssociations(queryClient),
      ]);
      setConfirmUnlink(false);
      toast.success(`${name}'s sign-in is no longer linked to this gateway`);
    } catch (error) {
      toast.error(
        error instanceof Error
          ? `Couldn't unlink ${name}'s sign-in. ${error.message}`
          : `Couldn't unlink ${name}'s sign-in.`,
      );
    }
  };

  let state: JSX.Element;
  if (associations.isLoading || isLoadingLinked || own.isLoading) {
    state = <Skeleton label={`Checking ${name}`} className="h-8 w-32" />;
  } else if (associations.isError) {
    state = (
      <button
        type="button"
        onClick={() => void associations.refetch()}
        className="text-foreground text-sm underline underline-offset-2"
      >
        Try again
      </button>
    );
  } else if (linked) {
    state = (
      <span className="inline-flex items-center gap-3">
        <span className="inline-flex items-center gap-2">
          <StatusDot tone="success" />
          <Text as="span" muted small>
            Linked
          </Text>
        </span>
        {readOnly ? null : (
          <RequireScope
            scope="mcp:write"
            resourceId={projectId}
            level="component"
          >
            <Button
              variant="tertiary"
              size="sm"
              aria-label={`Unlink ${name}'s sign-in`}
              onClick={() => setConfirmUnlink(true)}
            >
              <Button.Icon>
                <X className="size-4" />
              </Button.Icon>
            </Button>
          </RequireScope>
        )}
      </span>
    );
  } else if (readOnly) {
    state = (
      <span className="inline-flex items-center gap-2">
        <StatusDot tone="destructive" />
        <Text as="span" muted small>
          Not linked
        </Text>
      </span>
    );
  } else if (!choice) {
    // Several clients and none the member itself uses: the member's own
    // settings decide which one is its sign-in.
    state = (
      <Link
        to={memberSignInHref}
        className="text-foreground text-sm underline underline-offset-2"
      >
        Choose {name}&apos;s sign-in first
      </Link>
    );
  } else {
    state = (
      <RequireScope scope="mcp:write" resourceId={projectId} level="component">
        <Button
          variant="secondary"
          size="sm"
          disabled={attach.isPending}
          onClick={() => void link(choice)}
        >
          <Button.Text>Link sign-in</Button.Text>
        </Button>
      </RequireScope>
    );
  }

  const via = linked ?? choice ?? clients[0];
  return (
    <li className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 px-4 py-3">
      <span className="flex min-w-0 items-center gap-3">
        <SourceMcpIcon
          mcpServerId={server.id}
          className="size-6 shrink-0 object-contain"
        />
        <span className="flex min-w-0 flex-col">
          <Link
            to={routes.mcp.details.overview.href(mcpServerRouteParam(server))}
            className="text-foreground text-sm"
          >
            {name}
          </Link>
          {via ? (
            <Text as="span" muted small className="truncate">
              Signs in with {via.issuerName || via.issuerSlug} ·{" "}
              {hostOf(via.issuerUrl)}
            </Text>
          ) : null}
        </span>
      </span>
      <span className="shrink-0">{state}</span>
      {linked ? (
        <ConfirmDialog
          open={confirmUnlink}
          onOpenChange={setConfirmUnlink}
          title={`Unlink ${name}'s sign-in?`}
          description={`People connecting through this gateway can no longer use ${name} until it is linked again. ${name} itself keeps working.`}
          confirmLabel="Unlink"
          pendingLabel="Unlinking…"
          isPending={detach.isPending}
          onConfirm={() => void unlink(linked)}
        />
      ) : null}
    </li>
  );
}

function hostOf(url: string): string {
  try {
    return new URL(url).host;
  } catch {
    return url;
  }
}
