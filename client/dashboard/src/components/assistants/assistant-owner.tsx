import { IdentityCell } from "@/components/ui/identity-cell";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { getInitials } from "@/lib/initials";
import { cn } from "@/lib/utils";
import { useMembers } from "@gram/client/react-query/members.js";
import { User, UserX } from "lucide-react";
import { useMemo } from "react";

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

  // Resolved owner: avatar + name, full name on hover.
  if (member) {
    const display = member.name || member.email;
    return (
      <SimpleTooltip tooltip={display}>
        <IdentityCell
          name={variant === "card" ? `Created by ${display}` : display}
          imageUrl={member.photoUrl}
          fallbackIcon={getInitials(member.email, "email")}
          size="sm"
          className={cn(
            variant === "card" && "text-muted-foreground",
            className,
          )}
        />
      </SimpleTooltip>
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
    <IdentityCell
      name={label}
      fallbackIcon={<Icon className="size-3" />}
      size="sm"
      className={cn("text-muted-foreground", className)}
    />
  );
}
