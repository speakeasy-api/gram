import { Dialog } from "@/components/ui/Dialog";
import { Icon } from "@/components/ui/Icon";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import { useOrganizationUserSessionIssuerRemoteSessionClientBindings } from "@gram/client/react-query/organizationUserSessionIssuerRemoteSessionClientBindings.js";
import { useMemo } from "react";
import { compareClients, drifts, type ClientUse } from "./clientDrift";

/**
 * A warning by a provider's MCP servers when the clients it binds and the
 * clients its servers use don't match. Nothing shows when they agree, or
 * while the comparison can't be loaded.
 */
export function ClientDriftWarning({
  issuerId,
}: {
  issuerId: string;
}): JSX.Element | null {
  const query = useOrganizationUserSessionIssuerRemoteSessionClientBindings(
    { userSessionIssuerId: issuerId },
    undefined,
    { throwOnError: false },
  );
  const drift = useMemo(
    () => (query.data ? compareClients(query.data) : null),
    [query.data],
  );
  if (!drift || drift.unused + drift.unbound === 0) return null;

  const mismatched = drift.unused + drift.unbound;
  const label = `${mismatched} ${mismatched === 1 ? "client doesn't" : "clients don't"} match`;

  return (
    <Dialog>
      <Dialog.Trigger asChild>
        <button
          type="button"
          aria-label={label}
          title={label}
          className="text-warning hover:bg-warning-softest inline-flex size-7 cursor-pointer items-center justify-center"
        >
          <Icon name="triangle-alert" className="size-4" />
        </button>
      </Dialog.Trigger>
      <Dialog.Content className="max-w-3xl">
        <Dialog.Header>
          <Dialog.Title>Clients don&apos;t match</Dialog.Title>
          <Dialog.Description>
            This provider binds one set of clients and its MCP servers use
            another. {summary(drift.unused, drift.unbound)}
          </Dialog.Description>
        </Dialog.Header>
        <div className="max-h-[60vh] overflow-y-auto border">
          <table className="w-full text-left text-sm">
            <thead className="bg-muted/40 sticky top-0">
              <tr>
                <th className="text-eyebrow px-4 py-2 font-normal">Client</th>
                <th className="text-eyebrow px-4 py-2 font-normal">
                  Bound to this provider
                </th>
                <th className="text-eyebrow px-4 py-2 font-normal">
                  Used by MCP servers
                </th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {drift.clients.map((use) => (
                <ClientRow key={use.client.id} use={use} />
              ))}
            </tbody>
          </table>
        </div>
      </Dialog.Content>
    </Dialog>
  );
}

function summary(unused: number, unbound: number): string {
  const parts: string[] = [];
  if (unbound > 0) {
    parts.push(
      `${unbound} used by its servers ${unbound === 1 ? "isn't" : "aren't"} bound here`,
    );
  }
  if (unused > 0) {
    parts.push(
      `${unused} bound here ${unused === 1 ? "isn't" : "aren't"} used by any of its servers`,
    );
  }
  return `${parts.join(", and ")}.`.replace(/^./, (c) => c.toUpperCase());
}

function ClientRow({ use }: { use: ClientUse }): JSX.Element {
  const drifted = drifts(use);
  return (
    <tr className={cn(drifted && "bg-warning-softest")}>
      <td className="px-4 py-3 align-top">
        <div className="flex items-start gap-2">
          {drifted ? (
            <Icon
              name="triangle-alert"
              className="text-warning mt-0.5 size-3.5 shrink-0"
            />
          ) : (
            <span className="size-3.5 shrink-0" aria-hidden="true" />
          )}
          <div className="flex min-w-0 flex-col">
            <Text small>{use.client.remoteSessionIssuerName}</Text>
            <code className="text-muted-foreground font-mono text-xs break-all">
              {use.client.clientId}
            </code>
          </div>
        </div>
      </td>
      <td className="px-4 py-3 align-top">
        {use.bound ? (
          <Text small>Bound</Text>
        ) : (
          <Text small className="text-warning">
            Not bound
          </Text>
        )}
      </td>
      <td className="px-4 py-3 align-top">
        {use.servers.length > 0 ? (
          <ul className="flex flex-col gap-0.5">
            {use.servers.map((server) => (
              <li key={server.id}>
                <Text small>{server.name}</Text>{" "}
                <Text small muted>
                  {server.projectName}
                </Text>
              </li>
            ))}
          </ul>
        ) : (
          <Text small className="text-warning">
            No server uses it
          </Text>
        )}
      </td>
    </tr>
  );
}
