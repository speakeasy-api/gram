import { Link } from "react-router";
import { ShieldAlert, ShieldOff, ShieldQuestion } from "lucide-react";

import { SimpleTooltip } from "@/components/ui/Tooltip";
import { cn } from "@/lib/utils";
import type { KillswitchUserBadge } from "@gram/client/models/components/killswitchuserbadge.js";

type KillswitchUserState = "affected" | "scheduled" | "unavailable";

/**
 * What each state means for the person the row names, and what the reader can
 * do about it. A mark in a roster has no room to say either, so the icon
 * carries the state and the hover text carries the meaning — the label alone
 * ("Killswitched") named the state without saying what it stops.
 */
const STATE_COPY: Record<
  KillswitchUserState,
  { label: string; explanation: string }
> = {
  affected: {
    label: "Killswitch active",
    explanation:
      "A capability is turned off for this person right now. Open their access to see the scope and lift it.",
  },
  scheduled: {
    label: "Killswitch scheduled",
    explanation:
      "A capability is set to turn off for this person later. Nothing is blocked yet. Open their access to see when it starts.",
  },
  unavailable: {
    label: "Killswitch status unavailable",
    explanation:
      "This person's killswitches could not be loaded, so any restriction on them is unknown here. Open their access to check.",
  },
};

const STATE_ICON: Record<
  KillswitchUserState,
  { Glyph: typeof ShieldOff; className: string }
> = {
  affected: { Glyph: ShieldOff, className: "text-destructive" },
  scheduled: { Glyph: ShieldAlert, className: "text-warning" },
  unavailable: { Glyph: ShieldQuestion, className: "text-muted-foreground" },
};

function killswitchUserState(
  badge: KillswitchUserBadge | undefined,
  unavailable: boolean,
): KillswitchUserState | null {
  if (unavailable) return "unavailable";
  if (badge?.affectedNow) return "affected";
  if (badge?.scheduled) return "scheduled";
  return null;
}

/**
 * The mark a roster row carries when someone is under a killswitch, linking to
 * the access tab of their identity — the one place killswitches are managed.
 */
export function KillswitchUserStatusIcon({
  badge,
  href,
  unavailable = false,
}: {
  badge?: KillswitchUserBadge;
  /** Null for a reader who cannot open the identity page; the mark still shows. */
  href: string | null;
  unavailable?: boolean;
}): JSX.Element | null {
  const state = killswitchUserState(badge, unavailable);
  if (!state) return null;

  const { label, explanation } = STATE_COPY[state];
  const { Glyph, className } = STATE_ICON[state];
  // The hit area is the icon's own box rather than the glyph, so the mark
  // stays reachable at the size a roster row allows for it.
  const shared = cn(
    "inline-flex size-6 shrink-0 items-center justify-center",
    className,
  );
  const glyph = <Glyph aria-hidden="true" className="size-4" />;
  const tooltip = (
    <span className="block max-w-56">
      <span className="block font-medium">{label}</span>
      <span className="text-muted-foreground block">{explanation}</span>
    </span>
  );

  if (!href) {
    return (
      <SimpleTooltip tooltip={tooltip}>
        <span role="img" aria-label={label} tabIndex={0} className={shared}>
          {glyph}
        </span>
      </SimpleTooltip>
    );
  }

  return (
    <SimpleTooltip tooltip={tooltip}>
      <Link
        to={href}
        aria-label={`${label}; open this person's access`}
        onClick={(event) => event.stopPropagation()}
        className={cn(shared, "hover:text-foreground transition-colors")}
      >
        {glyph}
      </Link>
    </SimpleTooltip>
  );
}
