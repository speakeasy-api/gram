import { cn } from "@/lib/utils";

// Small inline segment toggle shared by the billing views (chart granularity,
// usage-card average unit). Mono uppercase segment; the active option fills as
// a solid ink block, matching the SegmentedControl idiom.
export function ToggleButton({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={cn(
        "px-2 py-0.5 font-mono text-xs tracking-[0.08em] uppercase transition-colors",
        active
          ? "bg-primary text-primary-foreground"
          : "text-muted-foreground hover:text-foreground",
      )}
    >
      {children}
    </button>
  );
}
