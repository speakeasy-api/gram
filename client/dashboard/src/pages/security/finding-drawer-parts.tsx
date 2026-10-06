import { Button } from "@/components/ui/Button";
import { cn } from "@/lib/utils";
import { Eye, EyeOff, Loader2, Lock } from "lucide-react";
import type { ReactNode } from "react";
import { REVEAL_DENIED_REASON } from "./unmask";

/** Mono 11px muted fine print under a drawer block. */
export const DRAWER_FOOTNOTE = "text-muted-foreground font-mono text-[11px]";

/** Mono 10px uppercase label inside drawer cells and rows. */
export const DRAWER_CELL_LABEL =
  "text-muted-foreground font-mono text-[10px] tracking-[0.1em] uppercase";

export function DrawerSection({
  label,
  aside,
  children,
}: {
  label: ReactNode;
  aside?: ReactNode;
  children: ReactNode;
}): JSX.Element {
  return (
    <section className="flex flex-col gap-2">
      <div className="flex items-center justify-between gap-3">
        <span className="text-eyebrow">{label}</span>
        {aside}
      </div>
      {children}
    </section>
  );
}

export function RevealToggleButton({
  revealed,
  onToggle,
  revealLabel = "Reveal matches",
}: {
  revealed: boolean;
  onToggle: () => void;
  revealLabel?: string;
}): JSX.Element {
  return (
    <Button variant="secondary" size="sm" onClick={onToggle}>
      <Button.LeftIcon>
        {revealed ? (
          <EyeOff className="size-3.5" />
        ) : (
          <Eye className="size-3.5" />
        )}
      </Button.LeftIcon>
      <Button.Text>{revealed ? "Hide matches" : revealLabel}</Button.Text>
    </Button>
  );
}

export function NoRevealAccessNote(): JSX.Element {
  return (
    <div className="text-muted-foreground flex items-center gap-1.5 text-xs">
      <Lock className="size-3 shrink-0" />
      <span>
        {REVEAL_DENIED_REASON} Structure and surrounding context stay visible.
      </span>
    </div>
  );
}

export function RevealingNote({
  className,
}: {
  className?: string;
}): JSX.Element {
  return (
    <div
      role="status"
      className={cn("flex items-center gap-2 text-sm", className)}
    >
      <Loader2 className="size-4 animate-spin" />
      <span>Revealing…</span>
    </div>
  );
}

export function RevealFailedNote({
  onRetry,
}: {
  onRetry: () => void;
}): JSX.Element {
  return (
    <div className="flex items-center gap-2">
      <span className="text-muted-foreground text-sm">
        Failed to load evidence.
      </span>
      <Button variant="tertiary" size="sm" onClick={onRetry}>
        <Button.Text>Retry</Button.Text>
      </Button>
    </div>
  );
}

/** Auto-fill grid of hairline label/value cells. */
export function FactGrid({
  facts,
  minWidth = "220px",
  bordered = true,
}: {
  facts: { label: string; value: ReactNode; title?: string }[];
  minWidth?: "160px" | "220px";
  bordered?: boolean;
}): JSX.Element {
  return (
    <div
      className={cn(
        "bg-card grid",
        minWidth === "220px"
          ? "grid-cols-[repeat(auto-fill,minmax(220px,1fr))]"
          : "grid-cols-[repeat(auto-fill,minmax(160px,1fr))]",
        bordered && "border-t border-l",
      )}
    >
      {facts.map((fact) => (
        <div
          key={fact.label}
          className={cn(
            "flex min-w-0 flex-col gap-1 px-3 py-2.5",
            bordered && "border-r border-b",
          )}
        >
          <span className={DRAWER_CELL_LABEL}>{fact.label}</span>
          <span className="truncate font-mono text-xs" title={fact.title}>
            {fact.value}
          </span>
        </div>
      ))}
    </div>
  );
}
