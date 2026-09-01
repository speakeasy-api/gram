import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/Avatar";
import { useIdentityTint } from "@/components/gradient-colors";
import { IdentityLink } from "@/components/identity-link";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/Popover";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/Tooltip";
import { Text } from "@/components/ui/Text";
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
  // Deterministic per-member flat tint so each fallback face is unique.
  const tint = useIdentityTint(member.id || member.name);
  return (
    <Avatar className={className}>
      {member.photoUrl && (
        <AvatarImage src={member.photoUrl} alt={member.name} />
      )}
      <AvatarFallback className="text-[10px] font-semibold" style={tint}>
        {initials(member.name)}
      </AvatarFallback>
    </Avatar>
  );
}

function OverflowAvatar({
  count,
  hoveredId,
  onHover,
}: {
  count: number;
  hoveredId: string | null;
  onHover: () => void;
}): React.JSX.Element {
  const isHovered = hoveredId === OVERFLOW_ID;
  const dimmed = hoveredId !== null && !isHovered;

  return (
    <motion.div
      className="row-start-1 cursor-pointer"
      onPointerEnter={onHover}
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
      <div className="ring-background bg-muted text-muted-foreground flex size-7 items-center justify-center rounded-full text-[11px] font-medium ring-2">
        +{count}
      </div>
    </motion.div>
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
  renderTrigger,
}: {
  members: FacepileMember[];
  maxFaces?: number;
  renderTrigger?: (props: {
    label: string;
    onClick: (event: React.MouseEvent) => void;
    onKeyDown: (event: React.KeyboardEvent<HTMLElement>) => void;
  }) => React.ReactElement;
}): React.JSX.Element {
  const [hoveredId, setHoveredId] = React.useState<string | null>(null);

  if (members.length === 0) {
    return <span className="text-muted-foreground">—</span>;
  }

  const sorted = [...members].sort((a, b) => a.name.localeCompare(b.name));
  const shown = sorted.slice(0, maxFaces);
  const overflow = sorted.length - shown.length;
  const label = `${members.length} member${members.length === 1 ? "" : "s"}`;
  const stopParentClick = (event: React.MouseEvent) => event.stopPropagation();
  const activateTrigger = (event: React.KeyboardEvent<HTMLElement>) => {
    if (event.key !== "Enter" && event.key !== " ") return;
    event.preventDefault();
    event.stopPropagation();
    event.currentTarget.click();
  };
  const trigger = renderTrigger ? (
    renderTrigger({
      label,
      onClick: stopParentClick,
      onKeyDown: activateTrigger,
    })
  ) : (
    <button
      type="button"
      aria-label={label}
      onClick={stopParentClick}
      className="hover:bg-accent/40 -ml-1 flex w-fit cursor-pointer items-center p-1 transition-colors"
    >
      {/* Grid overlap: each face sits in a track narrower than itself
          (auto-cols < avatar width), so faces overlap by a fixed amount and
          the pile's total width is deterministic — no negative-margin growth
          that would overflow the table column. */}
      <div
        className="grid grid-flow-col items-center justify-start [grid-auto-columns:1.4rem]"
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
                filter: dimmed
                  ? "saturate(0.65) brightness(0.98)"
                  : "saturate(1) brightness(1)",
                zIndex: isHovered ? 30 : 0,
              }}
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
        {overflow > 0 && (
          <OverflowAvatar
            count={overflow}
            hoveredId={hoveredId}
            onHover={() => setHoveredId(OVERFLOW_ID)}
          />
        )}
      </div>
    </button>
  );

  return (
    <Popover>
      <PopoverTrigger asChild>{trigger}</PopoverTrigger>
      <PopoverContent
        align="start"
        onClick={(e) => e.stopPropagation()}
        className="w-64 overflow-hidden p-0"
      >
        <div className="border-border border-b px-3 py-2">
          <Text small className="font-medium">
            {label}
          </Text>
        </div>
        <div className="max-h-64 overflow-y-auto py-1">
          {sorted.map((m) => (
            // The popover list is where a face becomes clickable: the pile
            // itself is a single trigger, and a row is big enough to aim at.
            <IdentityLink
              key={m.id}
              identifier={{ userId: m.id }}
              className="hover:bg-accent/40 flex items-center gap-2.5 px-3 py-1.5 no-underline hover:no-underline"
            >
              <MemberAvatar member={m} className="size-6" />
              <div className="min-w-0">
                <Text small className="truncate font-medium">
                  {m.name}
                </Text>
                <Text muted small className="truncate text-xs">
                  {m.email}
                </Text>
              </div>
            </IdentityLink>
          ))}
        </div>
      </PopoverContent>
    </Popover>
  );
}
