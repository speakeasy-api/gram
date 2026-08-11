import type { UserSession } from "@gram/client/models/components/usersession.js";
import type { UserSessionClient } from "@gram/client/models/components/usersessionclient.js";

import { Badge } from "@/components/ui/Badge";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import {
  clientDocumentOrigin,
  SOURCE_PRESENTATION,
  userSessionClientSource,
} from "@/lib/user-session-client-source";

// How the client registered with Gram: CIMD or DCR. Rendered on the clients
// listing and on both session listings, so the distinction reads the same
// everywhere. Each mode carries its own color so the two are separable at a
// glance in a long list; the colors say "different", not "better" or "worse".
export function ClientSourceBadge({
  client,
}: {
  client: Pick<UserSessionClient | UserSession, "clientIdMetadataUri">;
}): JSX.Element {
  const presentation = SOURCE_PRESENTATION[userSessionClientSource(client)];
  // The session listings show only the client-chosen client_name, so the
  // document origin is appended here to give every surface a label the client
  // cannot forge -- not just the clients table, which renders it inline.
  const origin = clientDocumentOrigin(client);
  const tooltip = origin
    ? `${presentation.tooltip} Document origin: ${origin}`
    : presentation.tooltip;
  return (
    <SimpleTooltip tooltip={tooltip}>
      <Badge
        size="sm"
        variant={presentation.badgeVariant}
        background
        className="shrink-0"
      >
        <Badge.Text>{presentation.label}</Badge.Text>
      </Badge>
    </SimpleTooltip>
  );
}
