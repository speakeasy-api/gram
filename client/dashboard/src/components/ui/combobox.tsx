"use client";

import * as React from "react";
import { Check, ChevronsUpDown } from "lucide-react";

import { cn } from "@/lib/utils";
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

export type DropdownItem = {
  value: string;
  label: string;
  icon?: React.ReactNode;
  onClick?: () => void;
};

export function Combobox({
  items,
  children,
  selected,
  onSelectionChange,
}: {
  items: DropdownItem[];
  selected: DropdownItem | undefined;
  onSelectionChange: (value: DropdownItem) => void;
  children: React.ReactNode;
}) {
  const [open, setOpen] = React.useState(false);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          className="w-full justify-between"
        >
          {children}
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[200px] p-0">
        <Command>
          {items.length > 3 && (
            <CommandInput placeholder="Search..." className="h-9" />
          )}
          <CommandList>
            <CommandEmpty>No framework found.</CommandEmpty>
            <CommandGroup>
              {items.map((item) => (
                <CommandItem
                  key={item.value}
                  value={item.value}
                  className="cursor-pointer"
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
                      selected?.value === item.value
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
