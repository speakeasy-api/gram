import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/Avatar";
import { getIdentityTint } from "@/components/gradient-colors";
import { Badge } from "@/components/ui/Badge";
import { Text } from "@/components/ui/Text";
import type { SlackDirectoryMember } from "@gram/client/models/components/slackdirectorymember.js";

const findings: Record<string, string> = {
  member_became_bot: "Slack account became a bot or app",
  member_type_unknown: "Slack account type became unknown",
  member_deactivated: "Slack account was deactivated",
  member_unknown: "Slack account state became unknown",
  member_absent: "Not seen in a complete sync",
  email_changed: "Slack email changed after confirmation",
};
function mappingFinding(member: SlackDirectoryMember): string | undefined {
  if (member.mappingConflictReason)
    return (
      findings[member.mappingConflictReason] ??
      "Directory evidence changed after confirmation"
    );
  if (member.mapping && !member.mapping.active)
    return "Mapped person is no longer active in this organization";
  return undefined;
}
export function MappingStatus({
  member,
}: {
  member: SlackDirectoryMember;
}): JSX.Element {
  const finding = mappingFinding(member);
  let label = "Not mapped";
  if (member.mapping) label = finding ? "Needs review" : "Mapped";
  return (
    <div className="space-y-1">
      <Badge variant={finding ? "warning" : "neutral"}>{label}</Badge>
      {finding && (
        <Text muted small>
          {finding}
        </Text>
      )}
    </div>
  );
}
export function PersonnelAvatar({
  name,
  email,
  photoUrl,
}: {
  name: string;
  email: string;
  photoUrl?: string;
}): JSX.Element {
  const label = name || email;
  const initials = label
    .trim()
    .split(/\s+/)
    .map((part) => part[0])
    .slice(0, 2)
    .join("")
    .toUpperCase();
  return (
    <Avatar className="size-6">
      {photoUrl && <AvatarImage src={photoUrl} alt="" />}
      <AvatarFallback className="text-xs" style={getIdentityTint(label)}>
        {initials || "?"}
      </AvatarFallback>
    </Avatar>
  );
}
