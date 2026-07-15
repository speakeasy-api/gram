import { Icon } from "@speakeasy-api/moonshine";
import * as React from "react";
import { cn } from "@/lib/utils";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "./ui/context-menu";
import type { Action } from "./ui/more-actions";

/**
 * Wraps a card (or any element) so right-clicking it opens a context menu of the
 * same `Action[]` the card already feeds to its visible `MoreActions` (⋯) button —
 * keeping the two menus in sync. Renders children unwrapped when there are no
 * actions, so it's a safe no-op to apply broadly.
 */
export function CardContextMenu({
  actions,
  children,
  className,
}: {
  actions: Action[];
  children: React.ReactNode;
  className?: string;
}): React.JSX.Element {
  if (actions.length === 0) {
    return <>{children}</>;
  }

  return (
    <ContextMenu>
      {/* Own the trigger element (a full-height wrapper) rather than asChild on
          the card — card components don't all forward refs/props, so the
          contextmenu handler must land on an element we control. Callers should
          place CardContextMenu around the card's outermost focusable element
          (e.g. a <Link>) so that keyboard Shift+F10 dispatches contextmenu on
          that element and the event bubbles up to this trigger. */}
      <ContextMenuTrigger asChild>
        <div className={cn("h-full", className)}>{children}</div>
      </ContextMenuTrigger>
      <ActionContextMenuContent actions={actions} />
    </ContextMenu>
  );
}

/**
 * The `ContextMenuContent` for a right-click menu built from an `Action[]`.
 * Shared by CardContextMenu and TableRowContextMenu so every context menu in
 * the app maps actions to items the same way.
 */
export function ActionContextMenuContent({
  actions,
}: {
  actions: Action[];
}): React.JSX.Element {
  return (
    <ContextMenuContent className="min-w-[10rem]">
      {actions.map((action, index) => (
        <React.Fragment key={index}>
          {action.separatorBefore && index > 0 && <ContextMenuSeparator />}
          <ContextMenuItem
            disabled={action.disabled}
            variant={action.destructive ? "destructive" : "default"}
            onSelect={() => action.onClick()}
          >
            {action.description ? (
              <span className="flex min-w-0 flex-col">
                <span>{action.label}</span>
                <span className="text-muted-foreground text-xs">
                  {action.description}
                </span>
              </span>
            ) : (
              action.label
            )}
            {action.icon && (
              <Icon name={action.icon} className="size-3 shrink-0" />
            )}
          </ContextMenuItem>
        </React.Fragment>
      ))}
    </ContextMenuContent>
  );
}
