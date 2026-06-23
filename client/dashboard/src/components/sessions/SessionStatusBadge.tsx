import { Badge } from "@speakeasy-api/moonshine";
import type { UserSession } from "@gram/client/models/components";

import { sessionStatus, STATUS_PRESENTATION } from "@/lib/user-session-status";

export function SessionStatusBadge({
  session,
}: {
  session: UserSession;
}): JSX.Element {
  const p = STATUS_PRESENTATION[sessionStatus(session)];
  return (
    <Badge size="sm" variant={p.badgeVariant} background>
      <Badge.Text>{p.label}</Badge.Text>
    </Badge>
  );
}
