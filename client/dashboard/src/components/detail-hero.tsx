import { cn } from "@/lib/utils";

interface DetailHeroProps {
  children: React.ReactNode;
  /** Action buttons rendered in the top-right corner of the hero */
  actions?: React.ReactNode;
  className?: string;
}

/**
 * Shared hero header for detail pages.
 * Renders a dotted-pattern background with bottom-aligned content and optional top-right actions.
 *
 * Used on MCP detail, source detail, built-in MCP detail, and external MCP detail pages.
 */
export function DetailHero({ children, actions, className }: DetailHeroProps) {
  return (
    <div
      className={cn(
        "relative w-full h-48 shrink-0 overflow-hidden border-b",
        className,
      )}
    >
      <div
        className="absolute inset-0 bg-muted/30 text-muted-foreground/20"
        style={{
          backgroundImage:
            "radial-gradient(circle, currentColor 1px, transparent 1px)",
          backgroundSize: "16px 16px",
        }}
      />

      <div className="absolute bottom-0 left-0 right-0 px-8 py-6 max-w-[1270px] mx-auto w-full">
        {children}
      </div>

      {actions && (
        <div className="absolute top-6 left-0 right-0 px-8 max-w-[1270px] mx-auto w-full">
          <div className="flex justify-end gap-2">{actions}</div>
        </div>
      )}
    </div>
  );
}
