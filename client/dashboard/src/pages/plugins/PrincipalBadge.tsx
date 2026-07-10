import { Badge } from "@/components/ui/badge";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { Role } from "@gram/client/models/components/role.js";
import { describePrincipal, principalIcon } from "./principals";

// PrincipalBadge renders a principal URN as an icon + resolved label chip, used
// wherever a plugin's current assignments are summarized.
export function PrincipalBadge({
  urn,
  roleByUrn,
  memberByUrn,
}: {
  urn: string;
  roleByUrn: Map<string, Role>;
  memberByUrn: Map<string, AccessMember>;
}): JSX.Element {
  const { kind, label } = describePrincipal(urn, roleByUrn, memberByUrn);
  const IconComponent = principalIcon(kind);
  return (
    <Badge variant="secondary" className="gap-1">
      <IconComponent className="h-3 w-3" />
      {label}
    </Badge>
  );
}
