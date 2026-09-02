import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/Avatar";
import { getIdentityTint } from "@/components/gradient-colors";
import { getInitials } from "@/lib/initials";
import { cn } from "@/lib/utils";
import { type Assignee, assigneeIdentity } from "./board-store";

/**
 * Two letters for the fallback face. Names go through the shared helper; an
 * email uses its local part so "ops-lead@example.com" reads "OL", not "O".
 */
function assigneeInitials(assignee: Assignee): string {
  if (assignee.kind === "user") return getInitials(assignee.name) || "?";
  const localPart = assignee.email.split("@")[0] ?? "";
  const words = localPart.split(/[^a-z0-9]+/i).filter(Boolean);
  const initials =
    words.length > 1
      ? words
          .slice(0, 2)
          .map((word) => word[0])
          .join("")
      : localPart.slice(0, 2);
  return initials.toUpperCase() || "?";
}

export function AssigneeAvatar({
  assignee,
  className,
}: {
  assignee: Assignee;
  className?: string;
}): JSX.Element {
  return (
    <Avatar className={cn("size-6", className)}>
      {assignee.kind === "user" && assignee.photoUrl && (
        <AvatarImage src={assignee.photoUrl} alt={assignee.name} />
      )}
      <AvatarFallback
        className="text-[10px] font-semibold"
        style={getIdentityTint(assigneeIdentity(assignee))}
      >
        {assigneeInitials(assignee)}
      </AvatarFallback>
    </Avatar>
  );
}
