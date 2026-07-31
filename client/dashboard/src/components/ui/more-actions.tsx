import { cn } from "@/lib/utils";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Icon,
  IconName,
} from "@speakeasy-api/moonshine";
import { useState } from "react";
import { Button } from "./button";

export type Action = {
  icon?: IconName;
  label: string;
  onClick: () => void;
  disabled?: boolean;
  destructive?: boolean;
  /** Secondary line under the label, e.g. why a disabled action is unavailable. */
  description?: string;
  /** Render a separator above this item (context menus and custom dropdown renderers). */
  separatorBefore?: boolean;
};

export function MoreActions({
  actions,
  triggerLabel,
  triggerLoading,
}: {
  actions: Action[];
  triggerLabel?: string;
  /** Shows a spinner in place of the trigger's kebab icon and disables it —
   * for an async action (e.g. an AI suggestion) already in flight from a
   * previous click. Only meaningful alongside `triggerLabel`. */
  triggerLoading?: boolean;
}): JSX.Element {
  const [isOpen, setIsOpen] = useState(false);

  const wrapOnClick =
    (onClick: () => void) => (e: React.MouseEvent<HTMLDivElement>) => {
      e.stopPropagation();
      e.preventDefault();
      setIsOpen(false);
      onClick();
    };

  return (
    <DropdownMenu open={isOpen} onOpenChange={setIsOpen}>
      <DropdownMenuTrigger asChild>
        {triggerLabel ? (
          <Button
            variant="secondary"
            size="sm"
            disabled={triggerLoading}
            aria-busy={triggerLoading}
          >
            <Icon
              name={triggerLoading ? "loader-circle" : "ellipsis-vertical"}
              className={cn("mr-1.5 size-4", triggerLoading && "animate-spin")}
            />
            {triggerLabel}
          </Button>
        ) : (
          <Button variant="ghost" size="sm" className="mx-[-4px] h-8 w-8 p-0">
            <Icon name="ellipsis-vertical" className="size-4" />
            <span className="sr-only">Open menu</span>
          </Button>
        )}
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="end"
        onCloseAutoFocus={(e) => {
          e.preventDefault();
        }}
      >
        {actions.map((action, index) => (
          <DropdownMenuItem
            key={index}
            onClick={wrapOnClick(action.onClick)}
            disabled={action.disabled}
            className={cn(
              "group flex cursor-pointer items-center justify-between",
              action.destructive &&
                "text-destructive hover:bg-destructive! hover:text-background! trans",
            )}
          >
            {action.label}
            {action.icon && (
              <Icon
                name={action.icon}
                className={cn(
                  "size-3 opacity-0 group-hover:opacity-100",
                  action.destructive &&
                    "text-destructive group-hover:text-background",
                )}
              />
            )}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
