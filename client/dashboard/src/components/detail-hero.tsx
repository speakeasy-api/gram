import { cn } from "@/lib/utils";

interface DetailHeroProps {
  children: React.ReactNode;
  /** Action buttons rendered in the top-right corner of the hero */
  actions?: React.ReactNode;
  className?: string;
}

/**
 * Shared hero header for detail pages.
 * Renders a flat muted band with a hairline bottom border, bottom-aligned
 * content, and optional top-right actions.
 *
 * Used on MCP detail, source detail, built-in MCP detail, and external MCP detail pages.
 */
export function DetailHero({
  children,
  actions,
  className,
}: DetailHeroProps): JSX.Element {
  return (
    <div
      className={cn(
        "bg-muted/30 relative h-48 w-full shrink-0 overflow-hidden border-b",
        className,
      )}
    >
      <div className="absolute right-0 bottom-0 left-0 mx-auto w-full max-w-[1270px] px-8 py-6">
        {children}
      </div>

      {actions && (
        <div className="absolute top-6 right-0 left-0 mx-auto w-full max-w-[1270px] px-8">
          <div className="flex justify-end gap-2">{actions}</div>
        </div>
      )}
    </div>
  );
}
