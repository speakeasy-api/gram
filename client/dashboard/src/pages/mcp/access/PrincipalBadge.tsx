import { Badge } from "@/components/ui/Badge";
import type { ComponentProps, JSX } from "react";

/** What kind of thing a rule names, so a role does not read as a person. */
const PRINCIPAL_BADGE: Record<
  string,
  { label: string; variant: ComponentProps<typeof Badge>["variant"] }
> = {
  role: { label: "Role", variant: "warning" },
  user: { label: "Person", variant: "information" },
  agent: { label: "Agent", variant: "information" },
  everyone: { label: "Everyone", variant: "neutral" },
  directory_group: { label: "Group", variant: "neutral" },
  directory_attribute: { label: "Attribute", variant: "neutral" },
};

export function PrincipalBadge({ kind }: { kind: string }): JSX.Element | null {
  const badge = PRINCIPAL_BADGE[kind];
  if (!badge) return null;
  return (
    <Badge variant={badge.variant} size="sm">
      {badge.label}
    </Badge>
  );
}
