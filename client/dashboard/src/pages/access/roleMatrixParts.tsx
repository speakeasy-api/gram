import { Badge } from "@/components/ui/Badge";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { cn } from "@/lib/utils";
import { Ban, Check, ChevronDown } from "lucide-react";
import type { JSX, ReactNode } from "react";

import type { RoleGrant } from "./types";

export type Grants = Record<string, RoleGrant>;
export type ProjectRef = { id: string; name: string; slug?: string };

/** Something the role editor is about to deny, as the deny dialog words it. */
export interface DenyTarget {
  /** What is denied, e.g. "MCP access" or "Environments". */
  what: string;
  /** Where the deny applies, e.g. "all servers" or "the whole organization". */
  where: string;
  apply: (grants: Grants) => Grants;
}

export type CellState =
  | "on"
  | "inherited"
  | "blocked"
  | "inherited-blocked"
  | "off";

const CELL_BASE =
  "inline-flex h-8 w-9 shrink-0 items-center justify-center border align-middle text-sm transition-colors focus-visible:ring-1 focus-visible:ring-ring focus-visible:outline-none";

const CELL_STYLE: Record<CellState, string> = {
  // Emerald rather than the design system's olive success green, which reads
  // as disabled at this size. Outline only, so a full grid stays calm.
  on: "border-emerald-700 text-emerald-700 bg-card dark:border-emerald-500 dark:text-emerald-500",
  inherited: "border-foreground border-dashed text-foreground bg-card",
  blocked: "border-destructive text-destructive bg-card",
  "inherited-blocked":
    "border-destructive/40 border-dashed text-destructive/70 bg-card",
  off: "border-border hover:border-foreground/40 bg-card",
};

function CellGlyph({
  state,
  small = false,
}: {
  state: CellState;
  small?: boolean;
}): JSX.Element | null {
  const size = small ? "size-3" : "size-4";
  if (state === "on" || state === "inherited") {
    return <Check aria-hidden="true" className={size} />;
  }
  if (state === "blocked" || state === "inherited-blocked") {
    return <Ban aria-hidden="true" className={size} />;
  }
  return null;
}

/** A cell as the legend shows it: the look alone, nothing to press. */
export function LegendCell({ state }: { state: CellState }): JSX.Element {
  return (
    <span
      aria-hidden="true"
      className={cn(CELL_BASE, "h-6 w-7", CELL_STYLE[state])}
    >
      <CellGlyph state={state} />
    </span>
  );
}

/**
 * One level on one row. Only what the role stores can be clicked; a tick that
 * comes from somewhere else says where in a tooltip.
 */
export function Cell({
  state,
  label,
  reason,
  onToggle,
  size = "md",
  narrowable = false,
}: {
  state: CellState;
  /** Names the cell for assistive tech, e.g. "Connect GitHub". */
  label: string;
  /** Why an inherited cell is ticked, e.g. "Comes from Manage". */
  reason?: string;
  onToggle?: () => void;
  size?: "md" | "sm";
  /**
   * An inherited tick that can still be taken away here, such as one tool of
   * a server that gets Connect from a wider row.
   */
  narrowable?: boolean;
}): JSX.Element {
  const interactive =
    !!onToggle &&
    (state === "on" ||
      state === "off" ||
      state === "blocked" ||
      (state === "inherited" && narrowable));
  const button = (
    <button
      type="button"
      aria-label={
        state === "blocked" || state === "inherited-blocked"
          ? `${label}, blocked`
          : label
      }
      aria-pressed={state === "on" || state === "blocked"}
      aria-disabled={!interactive}
      onClick={interactive ? onToggle : undefined}
      className={cn(
        CELL_BASE,
        size === "sm" && "size-5",
        CELL_STYLE[state],
        interactive ? "cursor-pointer" : "cursor-default",
      )}
    >
      <CellGlyph state={state} small={size === "sm"} />
    </button>
  );
  if (!reason) return button;
  return <SimpleTooltip tooltip={reason}>{button}</SimpleTooltip>;
}

/**
 * What a row covers, opening the editor that narrows it. Rows that cannot be
 * narrowed keep the same shape and say why.
 */
export function DropdownChip({
  children,
  label,
  onClick,
  disabledReason,
  tone = "default",
}: {
  children: ReactNode;
  /** Names the control for assistive tech. */
  label: string;
  onClick?: () => void;
  /** Set when the row cannot be narrowed here; shows plain text instead. */
  disabledReason?: string;
  tone?: "default" | "destructive";
}): JSX.Element {
  // Nothing to choose: say what it covers, without a control.
  if (disabledReason) {
    return (
      <SimpleTooltip tooltip={disabledReason}>
        <span className="text-muted-foreground cursor-default text-sm">
          {children}
        </span>
      </SimpleTooltip>
    );
  }
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      className={cn(
        "border-border hover:border-foreground/40 inline-flex h-8 max-w-full cursor-pointer items-center gap-1 border px-2 align-middle text-sm",
        tone === "destructive" && "text-destructive border-destructive/40",
      )}
    >
      <span className="truncate">{children}</span>
      <ChevronDown aria-hidden="true" className="size-3.5 shrink-0" />
    </button>
  );
}

/** Marks a row, tool or card the role denies. */
export function DeniedBadge(): JSX.Element {
  return (
    <Badge variant="destructive" size="sm">
      <Badge.LeftIcon>
        <Ban aria-hidden="true" />
      </Badge.LeftIcon>
      <Badge.Text>Denied</Badge.Text>
    </Badge>
  );
}

/** A section of the role editor: a ruled heading, a line of help, a count. */
export function EditorSection({
  title,
  description,
  count,
  children,
}: {
  title: string;
  description: string;
  /** A short tally at the far end, e.g. "3 areas granted". */
  count?: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <section className="border-foreground flex flex-col gap-3 border-t-2 pt-4">
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <h3 className="text-display-xs font-thin">{title}</h3>
        <p className="text-muted-foreground text-sm">{description}</p>
        {count && (
          <p className="text-muted-foreground ml-auto text-sm">{count}</p>
        )}
      </div>
      {children}
    </section>
  );
}
