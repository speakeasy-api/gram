import { useInsightsDockCta } from "@/hooks/useInsightsDockCta";
import { cn, isMacPlatform } from "@/lib/utils";
import { ReactElement } from "react";
import { useInsightsState } from "./insights-context";

/** Single source of truth for the composer's keyboard shortcut: Cmd+/ on Mac,
 *  Ctrl+/ on PC — matching the common "open assistant" convention. */
const INSIGHTS_SHORTCUT_LABEL_MAC = ["⌘", "/"];
const INSIGHTS_SHORTCUT_LABEL_PC = ["Ctrl", "/"];

/**
 * The platform-correct shortcut rendered as kbd pills (⌘ / on Mac, Ctrl / on
 * PC). Decorative: marked aria-hidden because the real shortcut is conveyed via
 * aria-keyshortcuts on the composer input. Shared so the page-header hint and
 * the dock pill render identical keys from one definition.
 */
export function InsightsShortcutKeys({
  className,
}: {
  className?: string;
}): ReactElement {
  const keys = isMacPlatform()
    ? INSIGHTS_SHORTCUT_LABEL_MAC
    : INSIGHTS_SHORTCUT_LABEL_PC;

  return (
    <span
      aria-hidden="true"
      className={cn("flex shrink-0 items-center gap-1 select-none", className)}
    >
      {keys.map((key) => (
        <kbd
          key={key}
          className="border-border bg-muted text-muted-foreground pointer-events-none inline-flex h-6 min-w-6 items-center justify-center rounded border px-1.5 font-mono text-sm leading-none font-medium select-none"
        >
          {key}
        </kbd>
      ))}
    </span>
  );
}

/**
 * Keyboard hint for the docked Project Assistant composer, shown on the
 * right-hand side of the page-header breadcrumbs. Renders nothing when no
 * InsightsProvider is mounted, the page hides the dock, or the dock is
 * dismissed to the sidebar resume button.
 *
 * Lives in its own module (not insights-dock.tsx) so page-header can
 * import it without pulling in the dock's routes import, which would
 * close an import cycle back through the page components.
 */
export function InsightsDockShortcutHint({
  className,
}: {
  className?: string;
}): ReactElement | null {
  const { available } = useInsightsState();
  const { dismissed } = useInsightsDockCta();
  if (!available || dismissed) return null;

  return (
    <span
      className={cn(
        "flex shrink-0 items-center gap-1 normal-case select-none",
        className,
      )}
    >
      <InsightsShortcutKeys />
      <span className="text-muted-foreground ml-1 text-xs font-normal">
        to launch assistant
      </span>
    </span>
  );
}
