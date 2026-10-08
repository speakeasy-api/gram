import { Badge } from "@/components/ui/Badge";
import { Card } from "@/components/ui/Card";
import { Text } from "@/components/ui/Text";
import { displayUrl, issuerDisplayName } from "@/lib/remote-identity";
import type { RemoteMcpServerClientScopes } from "@gram/client/models/components/remotemcpserverclientscopes.js";
import type { RemoteMcpServerScopes } from "@gram/client/models/components/remotemcpserverscopes.js";
import { useGetRemoteMcpServerScopes } from "@gram/client/react-query/getRemoteMcpServerScopes.js";
import type { ReactElement } from "react";
import { Link } from "react-router";
import { sharedServerLine } from "./x/tabs/settings/sections/authentication/resourceScopePin";

// Identity scopes every login asks for; they say nothing about the server's access.
const FEATURE_SCOPES = new Set([
  "openid",
  "email",
  "profile",
  "offline_access",
]);

const SOURCE_LINES: Record<
  RemoteMcpServerClientScopes["scopeSource"],
  string | null
> = {
  resource_pin: "Pinned for this server's URL.",
  client_scope: "Set on this connection.",
  challenge_scope: "From the server's last sign-in challenge.",
  live_resource: "Advertised by the server.",
  cached_resource: "Advertised by the server.",
  issuer_override: "Set by the identity provider's override.",
  issuer_catalogue: "Every scope the identity provider advertises.",
  issuer_omitted: null,
  none: null,
};

// Sources a live read of the MCP server can outrank at the next sign-in.
const PROBE_CAN_CHANGE = new Set<RemoteMcpServerClientScopes["scopeSource"]>([
  "cached_resource",
  "issuer_override",
  "issuer_catalogue",
]);

function shownScopes(client: RemoteMcpServerClientScopes): string[] {
  return client.requestedScopes.filter((scope) => !FEATURE_SCOPES.has(scope));
}

function requestsNothing(client: RemoteMcpServerClientScopes): boolean {
  return (
    client.scopeSource === "issuer_omitted" ||
    client.scopeSource === "none" ||
    client.requestedScopes.length === 0
  );
}

function worthShowing(client: RemoteMcpServerClientScopes): boolean {
  return requestsNothing(client) || shownScopes(client).length > 0;
}

/** One card per identity provider and request; several connections can share both. */
function distinctRequests(
  clients: RemoteMcpServerClientScopes[],
): RemoteMcpServerClientScopes[] {
  const byKey = new Map<string, RemoteMcpServerClientScopes>();
  for (const client of clients) {
    const key = JSON.stringify([
      client.issuerUrl ?? "",
      client.issuerName ?? "",
      client.scopeSource,
      [...new Set(client.requestedScopes)].sort(),
    ]);
    const seen = byKey.get(key);
    if (!seen) byKey.set(key, client);
    else if (client.pinWouldDecide && !seen.pinWouldDecide)
      byKey.set(key, { ...seen, pinWouldDecide: true });
  }
  return [...byKey.values()];
}

function issuerLabel(client: RemoteMcpServerClientScopes): string {
  return (
    issuerDisplayName({
      name: client.issuerName,
      issuer: client.issuerUrl ?? "",
    }) || "the identity provider"
  );
}

/** Issuer labels, with the issuer URL appended where two would read the same. */
function issuerLabels(
  clients: RemoteMcpServerClientScopes[],
): Map<string, string> {
  const labels = clients.map(issuerLabel);
  return new Map(
    clients.map((client, i) => {
      const label = labels[i] ?? "";
      const clash = labels.filter((other) => other === label).length > 1;
      return [
        client.clientId,
        clash && client.issuerUrl
          ? `${label} (${displayUrl(client.issuerUrl)})`
          : label,
      ];
    }),
  );
}

function capitalize(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1);
}

/** What a sign-in to a remote MCP server asks its identity provider for. */
export function RequestedScopesCard({
  mcpServerId,
  editHref,
}: {
  mcpServerId: string;
  /** The server's Identity settings, linked for those who may change the pin. */
  editHref?: string;
}): ReactElement | null {
  const { data, error } = useGetRemoteMcpServerScopes(
    { mcpServerId },
    undefined,
    { throwOnError: false },
  );
  if (error) return null;
  const clients = distinctRequests(data?.clients.filter(worthShowing) ?? []);
  if (!data || clients.length === 0) return null;
  const labels = issuerLabels(clients);

  return (
    <div className="max-w-2xl space-y-3">
      {clients.map((client) => {
        const name = labels.get(client.clientId) ?? issuerLabel(client);
        const canEdit =
          editHref !== undefined && data.canPin && client.pinWouldDecide;
        return (
          <Card.Dashboard
            key={client.clientId}
            title={
              requestsNothing(client)
                ? `No scopes requested from ${name}`
                : `Scopes requested from ${name}`
            }
            className="h-auto"
            headerClassName="py-3"
            bodyClassName="py-3"
            tooltip={
              requestsNothing(client)
                ? undefined
                : `Speakeasy requests these scopes; ${name} grants and enforces them. Existing connections keep their scopes until the next sign-in.`
            }
            action={
              canEdit ? (
                <Link
                  to={editHref}
                  className="text-sm font-medium underline underline-offset-2"
                >
                  Edit scopes
                </Link>
              ) : undefined
            }
          >
            <ClientScopes client={client} name={name} scopes={data} />
          </Card.Dashboard>
        );
      })}
    </div>
  );
}

function sourceLine(
  client: RemoteMcpServerClientScopes,
  scopes: RemoteMcpServerScopes,
): string | null {
  const parts = [SOURCE_LINES[client.scopeSource]];
  if (client.scopeSource === "resource_pin") {
    parts.push(sharedServerLine(scopes.sharedServerCount));
  }
  if (
    PROBE_CAN_CHANGE.has(client.scopeSource) &&
    scopes.discoveryEnabled &&
    !scopes.advertisedScopesKnown
  ) {
    parts.push("This may change once the MCP server is next contacted.");
  }
  const line = parts.filter(Boolean).join(" ");
  return line || null;
}

function ClientScopes({
  client,
  name,
  scopes,
}: {
  client: RemoteMcpServerClientScopes;
  name: string;
  scopes: RemoteMcpServerScopes;
}): ReactElement {
  if (requestsNothing(client)) {
    return (
      <Text small className="block">
        {`${capitalize(name)} applies its defaults.`}
      </Text>
    );
  }
  const source = sourceLine(client, scopes);
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
      <ul className="flex flex-wrap gap-1.5" aria-label="Requested scopes">
        {shownScopes(client).map((scope) => (
          <li key={scope}>
            <Badge
              variant="neutral"
              size="sm"
              className="normal-case tracking-normal"
            >
              {scope}
            </Badge>
          </li>
        ))}
      </ul>
      {source ? (
        <Text muted small>
          {source}
        </Text>
      ) : null}
    </div>
  );
}
