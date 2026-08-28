import { cn } from "@/lib/utils";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { Icon } from "@/components/ui/Icon";
import { IconName } from "@/components/ui/Icon/names";
import { useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/Button";

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
  triggerDisabled,
  triggerStyle,
}: {
  actions: Action[];
  triggerLabel?: string;
  /** Shows a spinner in place of the trigger icon, disables it, and restores
   * trigger focus when the async action completes. */
  triggerLoading?: boolean;
  /** Disables the trigger without presenting it as the active async action. */
  triggerDisabled?: boolean;
  /** Inline style for the trigger button. Every `Button` carries a 200ms
   * `transition-all` (`button.tsx`'s `.trans`), which per the CSS
   * Transitions spec holds a `visible → hidden` element at `visible` for
   * the whole transition before flipping — so a trigger inheriting
   * `visibility` from an ancestor that toggles it visually lingers ~200ms
   * after the rest of that ancestor's subtree has already vanished. Pass
   * `{ transitionProperty: "none" }` when this trigger's own visibility is
   * driven by an ancestor's `visible`/`invisible` toggle. */
  triggerStyle?: React.CSSProperties;
}): JSX.Element {
  const [isOpen, setIsOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const wasTriggerLoading = useRef(false);

  useEffect(() => {
    if (wasTriggerLoading.current && !triggerLoading) {
      triggerRef.current?.focus();
    }
    wasTriggerLoading.current = triggerLoading === true;
  }, [triggerLoading]);

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
            ref={triggerRef}
            variant="tertiary"
            size="sm"
            disabled={triggerLoading || triggerDisabled}
            aria-busy={triggerLoading === true}
            style={triggerStyle}
          >
            <Icon
              name={triggerLoading ? "loader-circle" : "ellipsis-vertical"}
              className={cn("mr-1.5 size-4", triggerLoading && "animate-spin")}
            />
            {triggerLabel}
          </Button>
        ) : (
          <Button
            ref={triggerRef}
            variant="tertiary"
            size="sm"
            className="mx-[-4px] h-8 w-8 p-0"
            disabled={triggerLoading || triggerDisabled}
            aria-busy={triggerLoading === true}
            style={triggerStyle}
          >
            <Icon
              name={triggerLoading ? "loader-circle" : "ellipsis-vertical"}
              className={cn("size-4", triggerLoading && "animate-spin")}
            />
            <span className="sr-only">
              {triggerLoading ? "Action in progress" : "Open menu"}
            </span>
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
