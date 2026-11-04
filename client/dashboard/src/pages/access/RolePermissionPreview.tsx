import { Badge } from "@/components/ui/Badge";
import { Text } from "@/components/ui/Text";

import type { PermissionSummary } from "./reviewGrants";

/**
 * One line that says what an assignment gives before it is made, naming the
 * sensitive permissions so an admin does not hand them out by accident.
 */
export function RolePermissionPreview({
  lead,
  summary,
  labelFor,
}: {
  /** "This role gives" or "These roles add". */
  lead: string;
  summary: PermissionSummary;
  labelFor: (scope: string) => string;
}): JSX.Element | null {
  const total = summary.scopes.length;
  if (total === 0) return null;
  const sensitive = summary.sensitive.length;
  const noun = total === 1 ? "permission" : "permissions";
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <Text muted small>
        {lead} {total} {noun}
        {sensitive > 0
          ? `, ${sensitive} ${sensitive === 1 ? "is" : "are"} sensitive:`
          : "."}
      </Text>
      {summary.sensitive.map((scope) => (
        <Badge key={scope} variant="warning" size="sm">
          {labelFor(scope)}
        </Badge>
      ))}
    </div>
  );
}
