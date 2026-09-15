import { cn } from "@/lib/utils";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { Icon } from "@/components/ui/Icon";
import { IconName } from "@/components/ui/Icon/names";
import { Fragment, useEffect, useRef, useState } from "react";
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
  triggerAriaLabel,
  triggerLoading,
  triggerDisabled,
  triggerStyle,
  size = "default",
  align = "default",
}: {
  actions: Action[];
  triggerLabel?: string;
  /** Icon-only trigger target: 32px by default, or 24px when compact. */
  size?: "default" | "compact";
  /** Align icon ink with the trailing content edge; the hover target extends
   * into the parent padding. Applies only to icon-only triggers. */
  align?: "default" | "end";
  /** Accessible name for an icon-only trigger. */
  triggerAriaLabel?: string;
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
  const pendingFocusRestore = useRef(false);

  useEffect(() => {
    if (wasTriggerLoading.current && !triggerLoading) {
      pendingFocusRestore.current = true;
    }
    wasTriggerLoading.current = triggerLoading === true;

    const trigger = triggerRef.current;
    if (
      pendingFocusRestore.current &&
      !triggerLoading &&
      !triggerDisabled &&
      trigger
    ) {
      trigger.focus();
      pendingFocusRestore.current = false;
    }
  }, [triggerDisabled, triggerLoading]);

  // Button centers its contents in a full-width inner span. Its sm variant
  // makes the icon 14px; Lucide ellipsis-vertical ink extends 2 viewBox units
  // from its center (a radius-1 circle plus a 1-unit half-stroke, on a 24 grid).
  // Move the entire target, not the icon, so the dots stay centered on hover.
  const edgeOffset = (size === "compact" ? 24 : 32) / 2 - (2 * 14) / 24;

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
            className={cn(
              "p-0",
              size === "compact" ? "h-6 w-6" : "h-8 w-8",
              align === "default" && "mx-[-4px]",
            )}
            disabled={triggerLoading || triggerDisabled}
            aria-busy={triggerLoading === true}
            style={{
              ...(align === "end" && { marginInlineEnd: -edgeOffset }),
              ...triggerStyle,
            }}
          >
            <Icon
              name={triggerLoading ? "loader-circle" : "ellipsis-vertical"}
              className={cn("size-4", triggerLoading && "animate-spin")}
            />
            <Button.Text className="sr-only">
              {triggerLoading
                ? "Action in progress"
                : (triggerAriaLabel ?? "Open menu")}
            </Button.Text>
          </Button>
        )}
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="end"
        onCloseAutoFocus={(e) => {
          if (triggerLoading) e.preventDefault();
        }}
      >
        {actions.map((action, index) => (
          <Fragment key={index}>
            {action.separatorBefore ? <DropdownMenuSeparator /> : null}
            <DropdownMenuItem
              onClick={wrapOnClick(action.onClick)}
              disabled={action.disabled}
              className={cn(
                "group flex cursor-pointer items-center justify-between",
                action.destructive &&
                  "text-destructive hover:bg-destructive! hover:text-background! trans",
              )}
            >
              <div className="min-w-0">
                <div>{action.label}</div>
                {action.description ? (
                  <div className="text-muted-foreground mt-0.5 text-xs font-normal">
                    {action.description}
                  </div>
                ) : null}
              </div>
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
          </Fragment>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
