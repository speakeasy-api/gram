import { Avatar, AvatarFallback } from "@/components/ui/Avatar";
import { cn } from "@/lib/utils";

/**
 * Initials for a person, taken from the part of an address that names them.
 *
 * The domain is shared by everyone in an organization, so folding it in would
 * give a column of identical marks.
 */
function initialsFor(label: string): string {
  const local = label.split("@")[0] ?? label;
  const words = local.split(/[.\-_+\s]+/).filter(Boolean);
  const letters =
    words.length > 1
      ? `${words[0]?.[0] ?? ""}${words[1]?.[0] ?? ""}`
      : local.slice(0, 2);
  return letters.replace(/[^a-zA-Z0-9]/g, "").toUpperCase() || "?";
}

/**
 * A person, as a small initialled disc.
 *
 * Wherever a list repeats one identity down a column, a generic person glyph
 * is the same shape on every row and so distinguishes nobody; initials at
 * least separate one colleague from another at a glance.
 */
export function IdentityAvatar({
  label,
  className,
  textClassName,
}: {
  label: string;
  className?: string;
  textClassName?: string;
}): JSX.Element {
  return (
    <Avatar className={cn("size-6 shrink-0", className)}>
      <AvatarFallback
        className={cn(
          "text-muted-foreground text-[10px] leading-none font-medium tracking-tight",
          textClassName,
        )}
      >
        {initialsFor(label)}
      </AvatarFallback>
    </Avatar>
  );
}
