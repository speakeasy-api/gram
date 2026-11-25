import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/Command";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/Popover";
import { cn } from "@/lib/utils";
import { Check, ChevronDown } from "lucide-react";
import { useState } from "react";
import { type MeterBreakdownOption } from "./meter-breakdown-options";

export function BreakdownPicker({
  value,
  groups,
  label,
  onChange,
}: {
  value: string;
  groups: { heading: string; options: MeterBreakdownOption[] }[];
  label: string;
  onChange: (value: string) => void;
}): JSX.Element {
  const [open, setOpen] = useState(false);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          role="combobox"
          aria-expanded={open}
          className="border-border hover:bg-muted data-[state=open]:bg-muted inline-flex items-center gap-1 border bg-transparent px-2 py-0.5 text-xs transition-colors"
        >
          By {label.toLowerCase()}
          <ChevronDown className="!size-3 opacity-50" />
        </button>
      </PopoverTrigger>
      <PopoverContent className="w-56 p-0" align="end">
        <Command>
          <CommandInput placeholder="Search breakdowns…" className="h-9" />
          <CommandList>
            <CommandEmpty>No breakdowns found.</CommandEmpty>
            {groups.map((group) => {
              const options = group.options;
              if (options.length === 0) return null;
              return (
                <CommandGroup
                  key={group.heading}
                  heading={group.heading || undefined}
                >
                  {options.map((o) => (
                    <CommandItem
                      key={o.value}
                      value={o.label}
                      className="cursor-pointer"
                      onSelect={() => {
                        onChange(o.value);
                        setOpen(false);
                      }}
                    >
                      <o.icon className="text-muted-foreground" />
                      {o.label}
                      <Check
                        className={cn(
                          "ml-auto",
                          value === o.value ? "opacity-100" : "opacity-0",
                        )}
                      />
                    </CommandItem>
                  ))}
                </CommandGroup>
              );
            })}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
