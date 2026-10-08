import { Badge } from "@/components/ui/Badge";
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@/components/ui/HoverCard";
import { Label } from "@/components/ui/Label";
import { Text } from "@/components/ui/Text";
import { issuerDisplayName } from "@/lib/remote-identity";
import type { RemoteMcpServerClientScopes } from "@gram/client/models/components/remotemcpserverclientscopes.js";
import type { RemoteMcpServerScopes } from "@gram/client/models/components/remotemcpserverscopes.js";
import { Info } from "lucide-react";
import type { ReactElement } from "react";

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
  resource_pin: "Pinned for this MCP server's URL.",
  client_scope: "Set on this connection.",
  challenge_scope: "From the MCP server's last sign-in challenge.",
  live_resource: "Advertised by the MCP server.",
  cached_resource: "Advertised by the MCP server.",
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

function issuerLabel(client: RemoteMcpServerClientScopes): string {
  return (
    issuerDisplayName({
      name: client.issuerName,
      issuer: client.issuerUrl ?? "",
    }) || "the identity provider"
  );
}

/** Read-only: what the connected client's sign-ins ask the identity provider for. */
export function RequestedScopesSummary({
  scopes,
  connectedClientId,
}: {
  scopes: RemoteMcpServerScopes;
  connectedClientId: string | null;
}): ReactElement | null {
  const client = scopes.clients.find(
    (candidate) => candidate.clientId === connectedClientId,
  );
  if (!client || !worthShowing(client)) return null;
  return (
    <div className="max-w-md">
      <ClientScopes client={client} scopes={scopes} />
    </div>
  );
}

function sourceLine(
  client: RemoteMcpServerClientScopes,
  scopes: RemoteMcpServerScopes,
): string | null {
  const parts = [SOURCE_LINES[client.scopeSource]];
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
  scopes,
}: {
  client: RemoteMcpServerClientScopes;
  scopes: RemoteMcpServerScopes;
}): ReactElement {
  const name = issuerLabel(client);
  const label = "Requested at sign-in";
  if (requestsNothing(client)) {
    return (
      <div className="space-y-1.5">
        <Label className="block leading-normal">{label}</Label>
        <Text muted small className="block">
          {`No scopes are requested; ${name} applies its defaults.`}
        </Text>
      </div>
    );
  }
  const source = sourceLine(client, scopes);
  return (
    <div className="space-y-1.5">
      <div className="flex items-center gap-1.5">
        <Label className="block leading-normal">{label}</Label>
        <HoverCard openDelay={150}>
          <HoverCardTrigger asChild>
            <button
              type="button"
              aria-label="About requested scopes"
              className="text-muted-foreground hover:text-foreground"
            >
              <Info className="size-3.5" />
            </button>
          </HoverCardTrigger>
          <HoverCardContent align="start" className="w-80">
            <Text small className="block">
              {`Speakeasy requests these scopes; ${name} grants and enforces them. Existing connections keep their scopes until the next sign-in.`}
            </Text>
          </HoverCardContent>
        </HoverCard>
      </div>
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
        <Text muted small className="block">
          {source}
        </Text>
      ) : null}
    </div>
  );
}
