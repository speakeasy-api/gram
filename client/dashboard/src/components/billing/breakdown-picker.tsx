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
import { Check, ChevronDown } from "lucide-react";
import { useState } from "react";
import { BREAKDOWN_GROUPS, breakdownLabel } from "./breakdown-options";

/**
 * Compact, searchable picker for the token-usage panel's breakdown: a small
 * "By …" trigger opening a grouped command palette (Usage / Organization /
 * People / …) instead of one very long flat dropdown.
 */
export function BreakdownPicker({
  value,
  onChange,
}: {
  // The selected breakdown: a Dimension value or a special-mode sentinel.
  value: string;
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
          className="border-border hover:bg-muted data-[state=open]:bg-muted inline-flex items-center gap-1 rounded border bg-transparent px-2 py-0.5 text-xs transition-colors"
        >
          By {breakdownLabel(value).toLowerCase()}
          <ChevronDown className="!size-3 opacity-50" />
        </button>
      </PopoverTrigger>
      <PopoverContent className="w-56 p-0" align="end">
        <Command>
          <CommandInput placeholder="Search breakdowns…" className="h-9" />
          <CommandList>
            <CommandEmpty>No breakdowns found.</CommandEmpty>
            {BREAKDOWN_GROUPS.map((group) => {
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
