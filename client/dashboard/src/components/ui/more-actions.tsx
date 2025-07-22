import { cn } from "@/lib/utils";
import { Icon, IconName } from "@speakeasy-api/moonshine";
import { useState } from "react";
import { Button } from "./button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "./dropdown-menu";

export type Action = {
  icon?: IconName;
  label: string;
  onClick: () => void;
  disabled?: boolean;
  destructive?: boolean;
};

export function MoreActions({ actions }: { actions: Action[] }) {
  const [isOpen, setIsOpen] = useState(false);

  const wrapOnClick = (onClick: () => void) => (e: React.MouseEvent<HTMLDivElement>) => {
    e.stopPropagation();
    e.preventDefault();
    onClick();
  };

  return (
    <DropdownMenu open={isOpen} onOpenChange={setIsOpen}>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="sm"
          className="h-8 w-8 p-0 mx-[-4px]"
        >
          <Icon name="ellipsis-vertical" className="size-4" />
          <span className="sr-only">Open menu</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {actions.map((action, index) => (
          <DropdownMenuItem
            key={index}
            onClick={wrapOnClick(action.onClick)}
            disabled={action.disabled}
            className={cn(
              "cursor-pointer justify-between flex items-center group",
              action.destructive &&
                "text-destructive hover:bg-destructive! hover:text-background! trans"
            )}
          >
            {action.label}
            {action.icon && (
              <Icon
                name={action.icon}
                className={cn(
                  "size-3 opacity-0 group-hover:opacity-100",
                  action.destructive &&
                    "text-destructive group-hover:text-background"
                )}
              />
            )}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
