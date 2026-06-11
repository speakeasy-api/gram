import { cn } from "@/lib/utils";
import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Link } from "react-router";

/**
 * A raised, bordered link card that stands out from the sidebar surface — used
 * to stack standout actions (e.g. "Finish setup", "Organization settings") just
 * above the user bar in the sidebar footer. Collapses to an icon when the
 * sidebar does. `trailing` renders a secondary control (e.g. a restore button)
 * on the card's right edge, outside the link's click area.
 */
export function SidebarFooterAction({
  to,
  icon: Icon,
  label,
  className,
  contentClassName,
  trailing,
}: {
  to: string;
  icon: LucideIcon;
  label: string;
  className?: string;
  contentClassName?: string;
  trailing?: ReactNode;
}): JSX.Element {
  return (
    <div
      className={cn(
        "hover:bg-accent border-border/60 text-foreground flex items-center gap-2 rounded-lg border bg-white px-2.5 py-1.5 text-sm shadow-sm transition-colors group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0 dark:bg-zinc-900",
        className,
      )}
    >
      <div
        className={cn(
          "flex w-full min-w-0 items-center gap-2 group-data-[collapsible=icon]:w-auto",
          contentClassName,
        )}
      >
        <Link
          to={to}
          title={label}
          className="flex min-w-0 flex-1 items-center gap-2 hover:no-underline group-data-[collapsible=icon]:flex-none group-data-[collapsible=icon]:justify-center"
        >
          <Icon className="size-4 shrink-0" strokeWidth={1.75} />
          <span className="truncate group-data-[collapsible=icon]:hidden">
            {label}
          </span>
        </Link>
        {trailing}
      </div>
    </div>
  );
}
