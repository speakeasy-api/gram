import { CheckIcon, ChevronsUpDownIcon } from "lucide-react";
import { useId, useRef, useState, type JSX, type Ref } from "react";

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
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import {
  FILTER_GROUPS,
  filterSummary,
  NO_FILTERS,
  optionsFor,
  toggleFilter,
  type FilterGroup,
  type FilterGroupKey,
  type FilterOption,
  type FilterSelection,
} from "@/lib/organizationFilters";
import { cn } from "@/lib/utils";

/**
 * Every filter this list carries, in one sheet.
 *
 * The edit is a draft: nothing reaches the URL, and so nothing reaches the
 * request, until the operator applies it. An admin narrowing a view by three
 * clicks would otherwise send three requests and read two lists they never
 * asked for on the way to the one they did.
 */
export function FilterSheet({
  value,
  openGroup,
  onOpenChange,
  onApply,
  onReturnFocus,
}: {
  value: FilterSelection;
  // Which group the operator asked for, and null when the sheet is closed. The
  // sheet opens with that group's picker focused, so the trigger they pressed
  // is the control they land on.
  openGroup: FilterGroupKey | null;
  onOpenChange: (open: boolean) => void;
  onApply: (next: FilterSelection) => void;
  // Called instead of Radix's own restore, which returns the keyboard to
  // whatever held it when the sheet opened. A click does not focus a button in
  // every browser, so that is not reliably the trigger.
  onReturnFocus: () => void;
}): JSX.Element {
  const open = openGroup !== null;
  const [draft, setDraft] = useState(value);
  const [lastOpened, setLastOpened] = useState(openGroup);

  // Seeded while rendering rather than in an effect, so a picker never paints
  // the previous edit for a frame. Opening is the only moment the draft follows
  // the URL: while the sheet is open the draft belongs to the operator, and a
  // navigation landing underneath must not move it.
  if (openGroup !== lastOpened) {
    setLastOpened(openGroup);
    if (open) setDraft(value);
  }

  const pickers = useRef<Partial<Record<FilterGroupKey, HTMLButtonElement>>>(
    {},
  );

  const apply = (next: FilterSelection): void => {
    onApply(next);
    onOpenChange(false);
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        className="w-full gap-0 sm:max-w-md"
        onOpenAutoFocus={(event) => {
          if (!openGroup) return;
          const picker = pickers.current[openGroup];
          if (!picker) return;
          event.preventDefault();
          picker.focus();
        }}
        onCloseAutoFocus={(event) => {
          event.preventDefault();
          onReturnFocus();
        }}
      >
        <SheetHeader>
          <SheetTitle>Filters</SheetTitle>
          <SheetDescription>
            Nothing in the table changes until you apply.
          </SheetDescription>
        </SheetHeader>

        <div className="grid gap-4 p-4">
          {FILTER_GROUPS.map((group) => (
            <FilterPicker
              key={group.key}
              group={group}
              chosen={draft[group.key]}
              // Taken from the value the sheet opened on, not from the draft:
              // an unrecognised type unchecked mid-edit has to stay on screen,
              // or the operator cannot change their mind.
              options={optionsFor(group, value[group.key])}
              onChange={(next) =>
                setDraft((previous) => ({ ...previous, [group.key]: next }))
              }
              ref={(node) => {
                if (node) pickers.current[group.key] = node;
              }}
            />
          ))}
        </div>

        <SheetFooter className="flex-row justify-end">
          {/* Clears the filters and nothing else. The search term is not a
              filter this sheet holds, and an operator who reset the filters has
              not asked to type their term again. */}
          <Button variant="ghost" onClick={() => apply(NO_FILTERS)}>
            Clear all
          </Button>
          <Button onClick={() => apply(draft)}>Apply</Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}

function FilterPicker({
  group,
  chosen,
  options,
  onChange,
  ref,
}: {
  group: FilterGroup;
  chosen: string[];
  options: FilterOption[];
  onChange: (next: string[]) => void;
  ref: Ref<HTMLButtonElement>;
}): JSX.Element {
  const [open, setOpen] = useState(false);
  const id = useId();

  return (
    <div className="grid gap-1.5">
      <span id={`${id}-label`} className="font-medium text-sm">
        {group.label}
      </span>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <Button
            ref={ref}
            id={id}
            variant="outline"
            role="combobox"
            aria-expanded={open}
            // The group's name and the trigger's own text, which is the state
            // of the group. A screen reader then announces both, the way a
            // select announces its label and its value.
            aria-labelledby={`${id}-label ${id}`}
            className="w-full justify-between font-normal"
          >
            <span
              className={cn(chosen.length === 0 && "text-muted-foreground")}
            >
              {filterSummary(group, chosen, options)}
            </span>
            <ChevronsUpDownIcon className="opacity-50" />
          </Button>
        </PopoverTrigger>
        <PopoverContent
          className="w-(--radix-popover-trigger-width) p-0"
          align="start"
        >
          <Command>
            <CommandInput placeholder={`Filter ${group.label.toLowerCase()}`} />
            <CommandList aria-multiselectable="true">
              <CommandEmpty>No match.</CommandEmpty>
              <CommandGroup>
                {options.map((option) => {
                  const selected = chosen.includes(option.value);
                  return (
                    <CommandItem
                      key={option.value}
                      value={option.value}
                      // The label as well, so typing what is on screen finds
                      // the option: "ending" has to reach `ending_soon`.
                      keywords={[option.label]}
                      aria-checked={selected}
                      onSelect={() =>
                        onChange(toggleFilter(chosen, option.value, options))
                      }
                    >
                      <CheckIcon
                        className={cn(selected ? "opacity-100" : "opacity-0")}
                      />
                      {option.label}
                    </CommandItem>
                  );
                })}
              </CommandGroup>
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>
    </div>
  );
}
