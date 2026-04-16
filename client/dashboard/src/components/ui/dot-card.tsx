import { cn } from "@/lib/utils";

interface DotCardProps {
  children: React.ReactNode;
  icon?: React.ReactNode;
  className?: string;
  overlay?: React.ReactNode;
  onClick?: (e: React.MouseEvent<HTMLDivElement>) => void;
}

/**
 * Shared card component with dot-pattern illustration sidebar.
 * Used across catalog, MCP, and source index pages for consistent card styling.
 *
 * - `icon`: Content centered in a frosted glass container over the dot pattern
 * - `overlay`: Additional content layered on the dot sidebar (e.g. "Added" badge)
 * - `children`: Content area to the right of the sidebar
 */
export function DotCard({
  children,
  icon,
  className,
  overlay,
  onClick,
}: DotCardProps) {
  return (
    <div
      onClick={onClick}
      className={cn(
        "dot-card group bg-card text-card-foreground !border-foreground/10 flex flex-row overflow-hidden rounded-xl border",
        "hover:!border-foreground/30 h-full min-h-[156px] transition-all hover:shadow-md",
        className,
      )}
    >
      {/* Dot pattern sidebar */}
      <div className="bg-muted/30 text-muted-foreground/20 relative w-40 shrink-0 overflow-hidden border-r">
        <div
          className="scroll-dots-target absolute inset-0"
          style={{
            backgroundImage:
              "radial-gradient(circle, currentColor 1px, transparent 1px)",
            backgroundSize: "16px 16px",
          }}
        />
        {icon && (
          <div className="absolute inset-0 flex items-center justify-center">
            <div className="bg-background/90 rounded-lg p-3 shadow-lg backdrop-blur-sm dark:bg-neutral-800 dark:backdrop-blur-none">
              {icon}
            </div>
          </div>
        )}
        {overlay}
      </div>

      {/* Content area */}
      <div className="flex min-w-0 flex-1 flex-col p-4">{children}</div>
    </div>
  );
}
