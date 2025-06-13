import { Button } from "@/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { cn } from "@/lib/utils";
import { Stack } from "@speakeasy-api/moonshine";
import { Check, ChevronsUpDown } from "lucide-react";
import { ReactNode, useState } from "react";
import { Type } from "./type";

export type DropdownItem = {
  value: string;
  label: string;
  icon?: ReactNode;
  onClick?: () => void;
};

export function Combobox<T extends DropdownItem>({
  items,
  children,
  selected,
  onSelectionChange,
  onOpenChange,
  variant = "outline",
  className,
  label,
  disabledMessage,
}: {
  items: T[];
  selected: T | string | undefined;
  onSelectionChange: (value: T) => void;
  onOpenChange?: (open: boolean) => void;
  children: ReactNode;
  className?: string;
  variant?: Parameters<typeof Button>[0]["variant"];
  label?: string;
  disabledMessage?: string;
}) {
  const [open, setOpen] = useState(false);

  const handleOpenChange = (open: boolean) => {
    setOpen(open);
    onOpenChange?.(open);
  };

  let trigger = (
    <PopoverTrigger asChild>
      <Button
        variant={variant}
        role="combobox"
        aria-expanded={open}
        className={cn("px-2", className)}
        disabled={!!disabledMessage}
        tooltip={disabledMessage}
      >
        <div className="flex items-center justify-between w-full gap-2">
          <div className="truncate font-medium">{children}</div>
          <ChevronsUpDown className="opacity-50" />
        </div>
      </Button>
    </PopoverTrigger>
  );

  if (label) {
    trigger = (
      <Stack
        direction="horizontal"
        align="center"
        className="bg-stone-200 dark:bg-stone-800 rounded-md w-fit"
      >
        <Type variant="small" className="px-2">
          {label}
        </Type>
        {trigger}
      </Stack>
    );
  }

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      {trigger}
      <PopoverContent className="w-[200px] p-0">
        <Command>
          {items.length > 4 && (
            <CommandInput placeholder="Search..." className="h-9" />
          )}
          <CommandList>
            <CommandEmpty>No items found.</CommandEmpty>
            <CommandGroup>
              {items.map((item) => (
                <CommandItem
                  key={item.value}
                  value={item.value}
                  className="cursor-pointer truncate"
                  onSelect={(v) => {
                    onSelectionChange(items.find((item) => item.value === v)!);
                    setOpen(false);
                  }}
                >
                  {item.icon}
                  {item.label}
                  <Check
                    className={cn(
                      "ml-auto",
                      (
                        typeof selected === "string"
                          ? selected === item.value
                          : selected?.value === item.value
                      )
                        ? "opacity-100"
                        : "opacity-0"
                    )}
                  />
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
