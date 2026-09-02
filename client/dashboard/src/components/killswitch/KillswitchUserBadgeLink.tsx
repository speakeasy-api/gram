import { Link } from "react-router";

import { Badge } from "@/components/ui/Badge";
import type { KillswitchUserBadge } from "@gram/client/models/components/killswitchuserbadge.js";

export function KillswitchUserBadgeLink({
  badge,
  href,
  unavailable = false,
}: {
  badge?: KillswitchUserBadge;
  href: string;
  unavailable?: boolean;
}): JSX.Element | null {
  if (!unavailable && !badge?.affectedNow && !badge?.scheduled) return null;

  const affectedNow = badge?.affectedNow === true;
  const label = unavailable
    ? "Killswitch status unavailable"
    : affectedNow
      ? "Killswitched"
      : "Scheduled killswitch";
  return (
    <Badge
      asChild
      size="sm"
      variant={
        unavailable ? "neutral" : affectedNow ? "destructive" : "warning"
      }
    >
      <Link
        className="min-h-6 min-w-6"
        to={href}
        aria-label={`${label}; view filtered killswitches`}
      >
        {label}
      </Link>
    </Badge>
  );
}
