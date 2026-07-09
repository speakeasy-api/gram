import { cn } from "@/lib/utils";
import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Link } from "react-router";

const ACTION_CLASS =
  "flex min-w-0 flex-1 items-center gap-2 hover:no-underline group-data-[collapsible=icon]:flex-none group-data-[collapsible=icon]:justify-center";

/**
 * A raised, bordered link card that stands out from the sidebar surface — used
 * to stack standout actions (e.g. "Finish setup", "Organization settings") just
 * above the user bar in the sidebar footer. Collapses to an icon when the
 * sidebar does. Pass `to` for a navigation action or `onClick` for an in-place
 * action (e.g. restoring a dismissed surface). `trailing` renders a secondary
 * control (e.g. a restore button) on the card's right edge, outside the main
 * action's click area.
 */
type SidebarFooterActionProps = {
  icon: LucideIcon;
  label: string;
  className?: string;
  contentClassName?: string;
  trailing?: ReactNode;
} &
  // Exactly one of `to` (navigation) or `onClick` (in-place action) — both
  // optional would let a caller render a card that does nothing.
  ({ to: string; onClick?: never } | { to?: never; onClick: () => void });

export function SidebarFooterAction({
  to,
  onClick,
  icon: Icon,
  label,
  className,
  contentClassName,
  trailing,
}: SidebarFooterActionProps): JSX.Element {
  const actionContent = (
    <>
      <Icon className="size-4 shrink-0" strokeWidth={1.75} />
      <span className="truncate group-data-[collapsible=icon]:hidden">
        {label}
      </span>
    </>
  );

  let action: ReactNode;
  if (to !== undefined) {
    action = (
      <Link to={to} title={label} className={ACTION_CLASS}>
        {actionContent}
      </Link>
    );
  } else {
    action = (
      <button
        type="button"
        onClick={onClick}
        title={label}
        className={cn(ACTION_CLASS, "text-left")}
      >
        {actionContent}
      </button>
    );
  }

  return (
    <div
      className={cn(
        "hover:bg-accent border-border/60 text-foreground bg-card flex items-center gap-2 rounded-lg border px-2.5 py-1.5 text-sm transition-colors group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0",
        className,
      )}
    >
      <div
        className={cn(
          "flex w-full min-w-0 items-center gap-2 group-data-[collapsible=icon]:w-auto",
          contentClassName,
        )}
      >
        {action}
        {trailing}
      </div>
    </div>
  );
}
