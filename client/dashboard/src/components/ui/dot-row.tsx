import { cn } from "@/lib/utils";

interface DotRowProps {
  children: React.ReactNode;
  icon?: React.ReactNode;
  className?: string;
  onClick?: (e: React.MouseEvent<HTMLTableRowElement>) => void;
}

/**
 * Table row variant of DotCard. The first cell contains the animated dot pattern
 * with an icon overlay, matching the card sidebar aesthetic. Remaining content
 * is rendered as additional cells via `children`.
 *
 * Must be used inside a `<TableBody>`.
 */
export function DotRow({ children, icon, className, onClick }: DotRowProps) {
  return (
    <tr
      onClick={onClick}
      className={cn(
        "dot-card group border-b border-foreground/10 transition-all",
        "hover:bg-muted/30",
        onClick && "cursor-pointer",
        className,
      )}
    >
      {/* Dot pattern cell */}
      <td className="size-17 p-0 relative overflow-hidden">
        <div className="absolute inset-0 bg-muted/30 text-muted-foreground/20">
          <div
            className="absolute inset-0 scroll-dots-target"
            style={{
              backgroundImage:
                "radial-gradient(circle, currentColor 1px, transparent 1px)",
              backgroundSize: "16px 16px",
            }}
          />
          {icon && (
            <div className="absolute inset-0 flex items-center justify-center">
              <div className="bg-background/90 backdrop-blur-sm dark:bg-neutral-800 dark:backdrop-blur-none rounded-md p-1.5 shadow-sm">
                {icon}
              </div>
            </div>
          )}
        </div>
      </td>
      {children}
    </tr>
  );
}
