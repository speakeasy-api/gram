import { IdentityLink } from "@/components/identity-link";
import { Text } from "@/components/ui/Text";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { PluginAudience } from "@gram/client/models/components/pluginaudience.js";
import type { Role } from "@gram/client/models/components/role.js";
import {
  describePrincipal,
  memberCountDescription,
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
  audienceByUrn: Map<string, PluginAudience>,
): string {
  switch (kind) {
    case "everyone":
      return "All members of this organization";
    case "email":
      return "Assigned by email address";
    case "role": {
      const role = roleByUrn.get(urn);
      if (!role) return "Role";
      return memberCountDescription(role.memberCount) ?? "Role";
    }
    case "user":
      return memberByUrn.get(urn)?.email ?? "Organization member";
    case "directory_group": {
      const memberCount = audienceByUrn.get(urn)?.memberCount;
      return memberCountDescription(memberCount) ?? "Directory group";
    }
    case "directory_attribute": {
      const memberCount = audienceByUrn.get(urn)?.memberCount;
      return memberCountDescription(memberCount) ?? "Directory attribute";
    }
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
  audienceByUrn,
}: {
  urn: string;
  roleByUrn: Map<string, Role>;
  memberByUrn: Map<string, AccessMember>;
  audienceByUrn: Map<string, PluginAudience>;
}): JSX.Element {
  const { kind, label } = describePrincipal(
    urn,
    roleByUrn,
    memberByUrn,
    audienceByUrn,
  );
  const IconComponent = principalIcon(kind);
  const description = principalDescription(
    urn,
    kind,
    roleByUrn,
    memberByUrn,
    audienceByUrn,
  );

  return (
    <div className="flex items-center gap-3 py-3">
      <div className="bg-muted text-muted-foreground flex h-9 w-9 shrink-0 items-center justify-center">
        <IconComponent className="h-4 w-4" />
      </div>
      <div className="min-w-0">
        {/* Only the principals that name one person reach an identity page;
            roles, groups and attributes name a set. */}
        <IdentityLink
          identifier={kind === "user" || kind === "email" ? { urn } : null}
        >
          {/* Block-level span, not a div: IdentityLink falls back to a
              <span> wrapper for principals that name no one person. */}
          <Text as="span" className="block truncate font-medium">
            {label}
          </Text>
        </IdentityLink>
        {description && (
          <Text as="div" small muted className="truncate">
            {description}
          </Text>
        )}
      </div>
    </div>
  );
}
