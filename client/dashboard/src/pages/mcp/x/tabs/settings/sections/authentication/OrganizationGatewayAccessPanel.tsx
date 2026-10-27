import { SettingsSection } from "@/components/detail/settings-section";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { handleError } from "@/lib/errors";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import { useAttachUserSessionIssuerMutation } from "@gram/client/react-query/attachUserSessionIssuer.js";
import { useDetachUserSessionIssuerMutation } from "@gram/client/react-query/detachUserSessionIssuer.js";
import { useOrganizationGateway } from "@gram/client/react-query/organizationGateway.js";
import { invalidateAllRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients.js";
import { invalidateAllServerClientAssociations } from "@gram/client/react-query/serverClientAssociations.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { AuthRow } from "./AuthRow";

/**
 * Whether people can reach this server through the organization gateway.
 *
 * A server that signs people in upstream is offered on the gateway only when
 * its sign-in client is attached to the gateway's own issuer; otherwise the
 * gateway leaves it out entirely. The switch makes that attachment, so it
 * applies to every server using the same client.
 *
 * It saves as soon as it is flipped, so it sits in its own panel rather than
 * beside the identity fields that wait for Save.
 */
export function OrganizationGatewayAccessPanel({
  serverUserSessionIssuerId,
  clients,
  otherServerCounts,
  canWrite,
}: {
  /** The issuer this server signs people in through. */
  serverUserSessionIssuerId: string;
  /** The per-person sign-in clients attached to this server. */
  clients: RemoteSessionClient[];
  /** For each client, how many other servers share it and so change with it. */
  otherServerCounts: number[];
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

  // Counts are per client, without the servers behind them, so servers shared
  // by two clients cannot be told apart: with more than one client sharing,
  // the largest single count is the most that can be stated.
  const sharingCounts = otherServerCounts.filter((count) => count > 0);
  const otherServers = Math.max(0, ...sharingCounts);
  const otherServersLabel = `${sharingCounts.length > 1 ? "at least " : ""}${
    otherServers === 1
      ? "1 other MCP server"
      : `${otherServers} other MCP servers`
  }`;

  const offered = clients.every((client) =>
    client.userSessionIssuerIds.includes(gatewayIssuerId),
  );

  const setOffered = async (next: boolean): Promise<void> => {
    setPending(true);
    try {
      for (const client of clients) {
        const attached = client.userSessionIssuerIds.includes(gatewayIssuerId);
        if (next === attached) continue;
        const request = {
          attachUserSessionIssuerForm: {
            id: client.id,
            userSessionIssuerId: gatewayIssuerId,
          },
        };
        if (next) await attach.mutateAsync({ request });
        else await detach.mutateAsync({ request });
      }
    } catch (error) {
      handleError(error, {
        title: next
          ? "Couldn't add this server to the organization gateway"
          : "Couldn't remove this server from the organization gateway",
      });
    } finally {
      await Promise.all([
        invalidateAllRemoteSessionClients(queryClient),
        invalidateAllServerClientAssociations(queryClient),
      ]);
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
