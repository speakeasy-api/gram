import { Badge } from "@/components/ui/Badge";
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@/components/ui/HoverCard";
import { Label } from "@/components/ui/Label";
import { Text } from "@/components/ui/Text";
import type { RemoteMcpServerClientScopes } from "@gram/client/models/components/remotemcpserverclientscopes.js";
import type { RemoteMcpServerScopes } from "@gram/client/models/components/remotemcpserverscopes.js";
import { Info } from "lucide-react";
import type { ReactElement } from "react";
import {
  issuerLabel,
  requestsNothing,
  shownScopes,
  sourceLine,
} from "./requestedScopes";

function worthShowing(client: RemoteMcpServerClientScopes): boolean {
  return requestsNothing(client) || shownScopes(client).length > 0;
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
