import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { DynamicIcon, type IconName } from "@/components/ui/dynamic-icon";
import { ChevronDown, EllipsisVertical } from "lucide-react";
import { useState } from "react";

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
          <Button size="sm">
            <Button.Text>{triggerLabel}</Button.Text>
            <Button.RightIcon>
              <ChevronDown />
            </Button.RightIcon>
          </Button>
        ) : (
          <Button
            variant="tertiary"
            size="sm"
            aria-label="Open menu"
            className="mx-[-4px] h-8 w-8 p-0"
          >
            <EllipsisVertical className="size-4" />
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
              <DynamicIcon
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
