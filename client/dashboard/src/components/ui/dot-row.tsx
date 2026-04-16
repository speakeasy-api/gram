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
        "dot-card group border-foreground/10 border-b transition-all",
        "hover:bg-muted/30",
        onClick && "cursor-pointer",
        className,
      )}
    >
      {/* Dot pattern cell */}
      <td className="relative size-17 overflow-hidden p-0">
        <div className="bg-muted/30 text-muted-foreground/20 absolute inset-0">
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
              <div className="bg-background/90 rounded-md p-1.5 shadow-sm backdrop-blur-sm dark:bg-neutral-800 dark:backdrop-blur-none">
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
