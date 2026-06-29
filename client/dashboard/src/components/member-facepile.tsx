import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { getGradientColors } from "@/components/gradient-colors";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { motion } from "motion/react";
import * as React from "react";

export type FacepileMember = {
  id: string;
  name: string;
  email: string;
  photoUrl?: string;
};

// Sentinel hover id for the "+N" overflow chip (no real member id collides).
const OVERFLOW_ID = "__overflow__";

/** Two-letter initials for the avatar fallback. */
function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return "?";
  if (parts.length === 1) return parts[0]!.slice(0, 2).toUpperCase();
  return (parts[0]![0]! + parts[parts.length - 1]![0]!).toUpperCase();
}

function MemberAvatar({
  member,
  className,
}: {
  member: FacepileMember;
  className?: string;
}): React.JSX.Element {
  // Deterministic per-member gradient so each fallback face is unique.
  const gradient = getGradientColors(member.id || member.name);
  return (
    <Avatar className={className}>
      {member.photoUrl && (
        <AvatarImage src={member.photoUrl} alt={member.name} />
      )}
      <AvatarFallback
        className="text-[10px] font-semibold text-white"
        style={{
          backgroundImage: `linear-gradient(${gradient.angle}deg, ${gradient.from}, ${gradient.to})`,
        }}
      >
        {initials(member.name)}
      </AvatarFallback>
    </Avatar>
  );
}

/**
 * Compact, overlapping avatar stack that reveals the full member list in a
 * popover on click. The popover is portaled, so it is never clipped by the
 * surrounding table row's overflow.
 */
export function MemberFacepile({
  members,
  maxFaces = 10,
}: {
  members: FacepileMember[];
  maxFaces?: number;
}): React.JSX.Element {
  const [hoveredId, setHoveredId] = React.useState<string | null>(null);

  if (members.length === 0) {
    return <span className="text-muted-foreground">—</span>;
  }

  const sorted = [...members].sort((a, b) => a.name.localeCompare(b.name));
  const shown = sorted.slice(0, maxFaces);
  const overflow = sorted.length - shown.length;
  const label = `${members.length} member${members.length === 1 ? "" : "s"}`;

  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          aria-label={label}
          // Stop the row's onRowClick from firing when opening the popover.
          onClick={(e) => e.stopPropagation()}
          className="hover:bg-accent/40 -ml-1 flex w-fit cursor-pointer items-center rounded-full p-1 transition-colors"
        >
          {/* Grid overlap: each face sits in a track narrower than itself
              (auto-cols < avatar width), so faces overlap by a fixed amount and
              the pile's total width is deterministic — no negative-margin growth
              that would overflow the table column. */}
          <div
            className="grid grid-flow-col items-center justify-start [grid-auto-columns:1.15rem]"
            // Clear only when leaving the whole pile. Moving between overlapping
            // faces just updates which is active, avoiding the flicker from
            // racing per-face enter/leave events.
            onPointerLeave={() => setHoveredId(null)}
          >
            {shown.map((m, i) => {
              const isHovered = hoveredId === m.id;
              const dimmed = hoveredId !== null && !isHovered;
              return (
                <motion.div
                  key={m.id}
                  style={{ gridColumnStart: i + 1 }}
                  className="relative row-start-1 cursor-pointer"
                  onPointerEnter={() => setHoveredId(m.id)}
                  animate={{
                    scale: isHovered ? 1.25 : dimmed ? 0.92 : 1,
                    // Dim via filter, not opacity: opacity would make the
                    // overlapped faces behind this one show through.
                    filter: dimmed
                      ? "saturate(0.65) brightness(0.98)"
                      : "saturate(1) brightness(1)",
                    zIndex: isHovered ? 30 : 0,
                  }}
                  // Snap in on hover, but relax back slowly so flicking the
                  // mouse across faces leaves a gentle settle rather than a
                  // jittery snap.
                  transition={
                    isHovered
                      ? { type: "spring", stiffness: 400, damping: 20 }
                      : { type: "tween", duration: 0.3, ease: "easeOut" }
                  }
                >
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <div>
                        <MemberAvatar
                          member={m}
                          className="ring-background size-7 ring-2"
                        />
                      </div>
                    </TooltipTrigger>
                    <TooltipContent side="top">{m.email}</TooltipContent>
                  </Tooltip>
                </motion.div>
              );
            })}
            {overflow > 0 &&
              (() => {
                const isHovered = hoveredId === OVERFLOW_ID;
                const dimmed = hoveredId !== null && !isHovered;
                return (
                  <motion.div
                    style={{ gridColumnStart: shown.length + 1 }}
                    className="row-start-1 cursor-pointer"
                    onPointerEnter={() => setHoveredId(OVERFLOW_ID)}
                    animate={{
                      scale: isHovered ? 1.25 : dimmed ? 0.92 : 1,
                      filter: dimmed
                        ? "saturate(0.65) brightness(0.98)"
                        : "saturate(1) brightness(1)",
                      zIndex: isHovered ? 30 : 10,
                    }}
                    transition={
                      isHovered
                        ? { type: "spring", stiffness: 400, damping: 20 }
                        : { type: "tween", duration: 0.3, ease: "easeOut" }
                    }
                  >
                    <div className="ring-background bg-muted text-muted-foreground flex h-7 items-center justify-center rounded-full px-2.5 text-[11px] font-medium whitespace-nowrap ring-2">
                      View all
                    </div>
                  </motion.div>
                );
              })()}
          </div>
        </button>
      </PopoverTrigger>
      <PopoverContent
        align="start"
        onClick={(e) => e.stopPropagation()}
        className="w-64 overflow-hidden p-0"
      >
        <div className="border-border border-b px-3 py-2">
          <Type small className="font-medium">
            {label}
          </Type>
        </div>
        <div className="max-h-64 overflow-y-auto py-1">
          {sorted.map((m) => (
            <div key={m.id} className="flex items-center gap-2.5 px-3 py-1.5">
              <MemberAvatar member={m} className="size-6" />
              <div className="min-w-0">
                <Type small className="truncate font-medium">
                  {m.name}
                </Type>
                <Type muted small className="truncate text-xs">
                  {m.email}
                </Type>
              </div>
            </div>
          ))}
        </div>
      </PopoverContent>
    </Popover>
  );
}
