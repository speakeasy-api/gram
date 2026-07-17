import { Type } from "@/components/ui/type";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { Role } from "@gram/client/models/components/role.js";
import {
  describePrincipal,
  principalIcon,
  type PrincipalKind,
} from "./principals";

// Secondary line under each assignment, giving the principal meaning beyond its
// name (who it reaches / how it's identified) so the section reads as content
// rather than a bare chip.
function principalDescription(
  urn: string,
  kind: PrincipalKind,
  roleByUrn: Map<string, Role>,
  memberByUrn: Map<string, AccessMember>,
): string {
  switch (kind) {
    case "everyone":
      return "All members of this organization";
    case "email":
      return "Assigned by email address";
    case "role": {
      const role = roleByUrn.get(urn);
      if (!role) return "Role";
      return `${role.memberCount} ${role.memberCount === 1 ? "member" : "members"}`;
    }
    case "user":
      return memberByUrn.get(urn)?.email ?? "Organization member";
    case "unknown":
      return "";
  }
}

// PluginAssignmentRow renders one of a plugin's current assignments as an
// icon-tile list row with a resolved name and a describing subtitle.
export function PluginAssignmentRow({
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
  const description = principalDescription(urn, kind, roleByUrn, memberByUrn);

  return (
    <div className="flex items-center gap-3 py-3">
      <div className="bg-muted text-muted-foreground flex h-9 w-9 shrink-0 items-center justify-center rounded-lg">
        <IconComponent className="h-4 w-4" />
      </div>
      <div className="min-w-0">
        <Type as="div" className="truncate font-medium">
          {label}
        </Type>
        {description && (
          <Type as="div" small muted className="truncate">
            {description}
          </Type>
        )}
      </div>
    </div>
  );
}
