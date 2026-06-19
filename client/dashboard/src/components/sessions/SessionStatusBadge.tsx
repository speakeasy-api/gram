import type { UserSession } from "@gram/client/models/components";

import { Badge } from "@/components/ui/badge";
import { sessionStatus, STATUS_PRESENTATION } from "@/lib/user-session-status";

export function SessionStatusBadge({
  session,
}: {
  session: UserSession;
}): JSX.Element {
  const p = STATUS_PRESENTATION[sessionStatus(session)];
  return <Badge variant={p.badgeVariant}>{p.label}</Badge>;
}
