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
};

export function MoreActions({
  actions,
  triggerLabel,
}: {
  actions: Action[];
  triggerLabel?: string;
}) {
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
          <Button size="sm">
            {triggerLabel}
            <Icon name="chevron-down" className="size-4 ml-1" />
          </Button>
        ) : (
          <Button variant="tertiary" size="sm" className="h-8 w-8 p-0 mx-[-4px]">
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
              "cursor-pointer justify-between flex items-center group",
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
