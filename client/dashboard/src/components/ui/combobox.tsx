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
import { Check, ChevronsUpDown } from "lucide-react";
import { ReactNode, useState } from "react";

export type DropdownItem = {
  value: string;
  label: string;
  icon?: ReactNode;
  onClick?: () => void;
};

export function Combobox({
  items,
  children,
  selected,
  onSelectionChange,
  onOpenChange,
  variant = "outline",
  className,
}: {
  items: DropdownItem[];
  selected: DropdownItem | string | undefined;
  onSelectionChange: (value: DropdownItem) => void;
  onOpenChange?: (open: boolean) => void;
  children: ReactNode;
  className?: string;
  variant?: Parameters<typeof Button>[0]["variant"];
}) {
  const [open, setOpen] = useState(false);

  const handleOpenChange = (open: boolean) => {
    setOpen(open);
    onOpenChange?.(open);
  };

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <PopoverTrigger asChild>
        <Button
          variant={variant}
          role="combobox"
          aria-expanded={open}
          className={cn("w-full px-2", className)}
        >
          <div className="flex items-center justify-between w-full gap-2">
            <div className="truncate">{children}</div>
            <ChevronsUpDown className="opacity-50" />
          </div>
        </Button>
      </PopoverTrigger>
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
