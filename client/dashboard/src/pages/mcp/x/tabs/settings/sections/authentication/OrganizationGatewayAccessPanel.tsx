import { SettingsSection } from "@/components/detail/settings-section";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { handleError } from "@/lib/errors";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import { useAttachUserSessionIssuerMutation } from "@gram/client/react-query/attachUserSessionIssuer.js";
import { useDetachUserSessionIssuerMutation } from "@gram/client/react-query/detachUserSessionIssuer.js";
import { useOrganizationGateway } from "@gram/client/react-query/organizationGateway.js";
import { type QueryClient, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { AuthRow } from "./AuthRow";

type ClientListPage = { result: { items: RemoteSessionClient[] } };

/**
 * Writes an updated client into every cached client list, and marks the other
 * cached copies stale. Refetching the lists instead would put the whole
 * identity section back into its loading state for a change to one field.
 */
function storeClient(queryClient: QueryClient, client: RemoteSessionClient) {
  const replace = (page: ClientListPage): ClientListPage => ({
    ...page,
    result: {
      ...page.result,
      items: page.result.items.map((item) =>
        item.id === client.id ? client : item,
      ),
    },
  });
  queryClient.setQueriesData<ClientListPage | { pages: ClientListPage[] }>(
    { queryKey: ["@gram/client", "remoteSessionClients", "list"] },
    (data) => {
      if (!data) return data;
      if ("pages" in data) return { ...data, pages: data.pages.map(replace) };
      return replace(data);
    },
  );
  void queryClient.invalidateQueries({
    predicate: ({ queryKey }) =>
      queryKey[1] === "remoteSessionClients" && queryKey[2] !== "list",
  });
}

export type GatewaySignInClient = {
  client: RemoteSessionClient;
  /** How many other servers share this client and so change with it. */
  otherServers: number;
};

/**
 * Whether people can reach this server through the organization gateway.
 *
 * A server that signs people in upstream is offered on the gateway when one of
 * its sign-in clients is attached to the gateway's own issuer; otherwise the
 * gateway leaves it out entirely. The gateway's issuer holds one client per
 * upstream, so offering attaches a single client, and withdrawing detaches
 * every one that is attached. An attachment applies to every server using
 * that client.
 *
 * It saves as soon as it is flipped, so it sits in its own panel rather than
 * beside the identity fields that wait for Save.
 */
export function OrganizationGatewayAccessPanel({
  serverUserSessionIssuerId,
  clients,
  preferredClientId,
  canWrite,
}: {
  /** The issuer this server signs people in through. */
  serverUserSessionIssuerId: string;
  /** The per-person sign-in clients attached to this server. */
  clients: GatewaySignInClient[];
  /** The client to offer when there is a choice: the one people connect with. */
  preferredClientId: string | null;
  canWrite: boolean;
}): JSX.Element | null {
  const queryClient = useQueryClient();
  const gateway = useOrganizationGateway(undefined, undefined, {
    throwOnError: false,
  });
  const attach = useAttachUserSessionIssuerMutation();
  const detach = useDetachUserSessionIssuerMutation();
  const [pending, setPending] = useState(false);

  const gatewayIssuerId = gateway.data?.userSessionIssuerId;
  // A server already signing in through the gateway's issuer is offered on it
  // with nothing to change, and an unavailable gateway has nothing to offer.
  if (
    !gateway.data?.available ||
    !gatewayIssuerId ||
    gatewayIssuerId === serverUserSessionIssuerId ||
    clients.length === 0
  ) {
    return null;
  }

  const attached = clients.filter(({ client }) =>
    client.userSessionIssuerIds.includes(gatewayIssuerId),
  );
  const offered = attached.length > 0;
  const toOffer =
    clients.find(({ client }) => client.id === preferredClientId) ??
    clients[0]!;
  // The clients the next flip changes, and so the servers it reaches.
  const changing = offered ? attached : [toOffer];

  // Counts are per client, without the servers behind them, so servers shared
  // by two clients cannot be told apart: with more than one client sharing,
  // the largest single count is the most that can be stated.
  const sharingCounts = changing
    .map(({ otherServers }) => otherServers)
    .filter((count) => count > 0);
  const otherServers = Math.max(0, ...sharingCounts);
  const otherServersLabel = `${sharingCounts.length > 1 ? "at least " : ""}${
    otherServers === 1
      ? "1 other MCP server"
      : `${otherServers} other MCP servers`
  }`;

  const setOffered = async (next: boolean): Promise<void> => {
    setPending(true);
    try {
      for (const { client } of next ? [toOffer] : attached) {
        const request = {
          attachUserSessionIssuerForm: {
            id: client.id,
            userSessionIssuerId: gatewayIssuerId,
          },
        };
        const updated = next
          ? await attach.mutateAsync({ request })
          : await detach.mutateAsync({ request });
        storeClient(queryClient, updated);
      }
    } catch (error) {
      handleError(error, {
        title: next
          ? "Couldn't add this server to the organization gateway"
          : "Couldn't remove this server from the organization gateway",
      });
    } finally {
      setPending(false);
    }
  };

  return (
    <SettingsSection.Panel className="mt-4">
      <AuthRow
        label="Organization gateway"
        hint="Let people sign in to this server through the organization gateway."
      >
        <div className="space-y-2">
          <div className="flex items-center gap-3">
            <Switch
              aria-label="Offer on the organization gateway"
              checked={offered}
              disabled={!canWrite || pending}
              onCheckedChange={(next) => void setOffered(next)}
            />
            <Text small>
              {offered
                ? "Offered on the organization gateway"
                : "Not offered on the organization gateway"}
            </Text>
          </div>
          {otherServers > 0 ? (
            <Text muted small className="block">
              This sign-in is shared, so the change also applies to{" "}
              {otherServersLabel}.
            </Text>
          ) : null}
          {!canWrite ? (
            <Text muted small className="block">
              Only people who can edit this server can change this.
            </Text>
          ) : null}
        </div>
      </AuthRow>
    </SettingsSection.Panel>
  );
}
