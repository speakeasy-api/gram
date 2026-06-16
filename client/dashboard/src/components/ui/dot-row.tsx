import { cn } from "@/lib/utils";
import { Link } from "react-router";

interface DotRowProps {
  children: React.ReactNode;
  icon?: React.ReactNode;
  className?: string;
  /**
   * When set, the whole row becomes a real navigation link. A stretched anchor
   * covers the row so browser semantics (open-in-new-tab, copy link, the
   * right-click menu) and keyboard/screen-reader navigation work — prefer this
   * over an `onClick` navigation handler. Interactive controls inside
   * `children` (buttons, dropdowns) must sit above the overlay; wrap them in a
   * `relative z-20` element so they stay clickable.
   */
  href?: string;
  /** Accessible label for the row link. Set this whenever `href` is set. */
  ariaLabel?: string;
  onClick?: (e: React.MouseEvent<HTMLTableRowElement>) => void;
}

/**
 * Table row variant of DotCard. The first cell contains the animated dot pattern
 * with an icon overlay, matching the card sidebar aesthetic. Remaining content
 * is rendered as additional cells via `children`.
 *
 * Must be used inside a table body.
 */
export function DotRow({
  children,
  icon,
  className,
  href,
  ariaLabel,
  onClick,
}: DotRowProps): JSX.Element {
  return (
    <tr
      onClick={onClick}
      className={cn(
        "dot-card group border-foreground/10 border-b transition-all",
        "hover:bg-muted/30",
        (onClick || href) && "cursor-pointer",
        href && "relative",
        className,
      )}
    >
      {/* Dot pattern cell */}
      <td className="size-17 overflow-hidden p-0">
        {href && (
          <Link
            to={href}
            aria-label={ariaLabel}
            className="absolute inset-0 z-10"
          />
        )}
        <div className="bg-muted/30 text-muted-foreground/20 relative size-17 overflow-hidden">
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
