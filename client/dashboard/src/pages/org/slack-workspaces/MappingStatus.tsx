import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/Avatar";
import { useIdentityTint } from "@/components/gradient-colors";
import { Badge } from "@/components/ui/Badge";
import { Text } from "@/components/ui/Text";
import type { SlackDirectoryMember } from "@gram/client/models/components/slackdirectorymember.js";
import { mappingFinding } from "./mappingFindings";

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
  className,
}: {
  name: string;
  email: string;
  photoUrl?: string;
  className?: string;
}): JSX.Element {
  const label = name || email;
  const tint = useIdentityTint(label);
  const initials = label
    .trim()
    .split(/\s+/)
    .map((part) => part[0])
    .slice(0, 2)
    .join("")
    .toUpperCase();
  return (
    <Avatar className={className ?? "size-6"}>
      {photoUrl && <AvatarImage src={photoUrl} alt="" />}
      <AvatarFallback className="text-xs" style={tint}>
        {initials || "?"}
      </AvatarFallback>
    </Avatar>
  );
}
