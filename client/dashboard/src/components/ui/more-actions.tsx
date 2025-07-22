import { cn } from "@/lib/utils";
import { Icon, IconName } from "@speakeasy-api/moonshine";
import { useRef, useState } from "react";
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
  const isOpening = useRef(false);

  const open = () => {
    isOpening.current = true;
    setIsOpen(true);
    setTimeout(() => {
      isOpening.current = false;
    }, 350); // Ensure that the menu always appears for at least a certain amount of time
  };

  const close = () => {
    setTimeout(() => {
      // If the menu is still opening, don't close it
      if (!isOpening.current) {
        setIsOpen(false);
      }
    }, 250);
  };

  return (
    <DropdownMenu open={isOpen} onOpenChange={setIsOpen}>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="sm"
          className="h-8 w-8 p-0 mx-[-4px]"
          onMouseEnter={open}
          onMouseLeave={close}
        >
          <Icon name="ellipsis-vertical" className="size-4" />
          <span className="sr-only">Open menu</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" onMouseEnter={open} onMouseLeave={close}>
        {actions.map((action, index) => (
          <DropdownMenuItem
            key={index}
            onClick={action.onClick}
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
