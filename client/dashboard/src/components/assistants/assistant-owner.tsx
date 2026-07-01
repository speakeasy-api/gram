import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import { useMembers } from "@gram/client/react-query/members.js";
import { User, UserX } from "lucide-react";
import { useMemo } from "react";

/** Two-letter initials from a display name or email handle. */
function initialsOf(identifier: string): string {
  const handle = identifier.split("@")[0] ?? identifier;
  return handle.slice(0, 2).toUpperCase();
}

type OwnerVariant = "card" | "row";

/**
 * Displays the creator ("owner") of an assistant as a profile avatar, reusing
 * the org-home member avatar treatment. Resolves `createdByUserId` against the
 * current org members into one of three states:
 *
 *   - owner    — id resolves to a current member: avatar + name
 *   - none     — id absent (assistant was never attributed): "No owner"
 *   - orphaned — id present but not a current member (creator left): "Orphaned"
 *
 * The "card" variant prefixes the name with "Created by"; the "row" variant
 * shows just the avatar + name for use inside a labelled settings row.
 */
export function AssistantOwner({
  createdByUserId,
  variant,
  className,
}: {
  createdByUserId?: string | undefined;
  variant: OwnerVariant;
  className?: string;
}): React.JSX.Element {
  const { data: membersData } = useMembers();

  const member = useMemo(() => {
    if (!createdByUserId) return undefined;
    return (membersData?.members ?? []).find((m) => m.id === createdByUserId);
  }, [membersData, createdByUserId]);

  const container = cn("flex items-center gap-1.5", className);

  // Resolved owner: avatar + name, full name on hover.
  if (member) {
    const display = member.name || member.email;
    return (
      <div className={container}>
        <SimpleTooltip tooltip={display}>
          <Avatar className="size-5">
            {member.photoUrl ? (
              <AvatarImage src={member.photoUrl} alt={display} />
            ) : null}
            <AvatarFallback className="bg-muted text-muted-foreground text-[9px] font-medium">
              {initialsOf(member.email)}
            </AvatarFallback>
          </Avatar>
        </SimpleTooltip>
        <Type
          muted={variant === "card"}
          small
          className="truncate"
          title={display}
        >
          {variant === "card" ? `Created by ${display}` : display}
        </Type>
      </div>
    );
  }

  // A creator id is set but the member list hasn't resolved yet (still loading,
  // or the viewer can't list members). Show a neutral placeholder rather than
  // falsely flashing "orphaned" — that label is only honest once members have
  // actually loaded and the id genuinely isn't among them.
  const membersLoaded = membersData !== undefined;
  const resolving = !!createdByUserId && !membersLoaded;
  const orphaned = !!createdByUserId && membersLoaded && !member;

  const Icon = orphaned ? UserX : User;
  const label = resolving
    ? variant === "card"
      ? "Created by …"
      : "…"
    : orphaned
      ? "Orphaned, no owner"
      : "No owner";

  return (
    <div className={container}>
      <Avatar className="size-5">
        <AvatarFallback className="bg-muted text-muted-foreground/60">
          <Icon className="size-3" />
        </AvatarFallback>
      </Avatar>
      <Type muted small className="truncate" title={label}>
        {label}
      </Type>
    </div>
  );
}
