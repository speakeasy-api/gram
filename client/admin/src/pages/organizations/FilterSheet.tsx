import { CheckIcon, ChevronsUpDownIcon } from "lucide-react";
import { useId, useRef, useState, type JSX, type Ref } from "react";

import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
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
  memberRange,
  memberRangeErrors,
  filterSummary,
  NO_FILTERS,
  optionsFor,
  toggleFilter,
  type FilterGroup,
  type FilterControlKey,
  type FilterOption,
  type FilterSelection,
} from "@/lib/organizationFilters";
import {
  CREATED_PRESETS,
  createdPresetRange,
  createdRange,
  createdRangeErrors,
  recognizeCreatedPreset,
  type CreatedPreset,
} from "@/lib/createdRange";
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
  openGroup: FilterControlKey | null;
  onOpenChange: (open: boolean) => void;
  onApply: (next: FilterSelection) => void;
  // Called instead of Radix's own restore, which returns the keyboard to
  // whatever held it when the sheet opened. A click does not focus a button in
  // every browser, so that is not reliably the trigger.
  onReturnFocus: () => void;
}): JSX.Element {
  const disabledId = useId();
  const membersId = useId();
  const createdId = useId();
  const [createdPreset, setCreatedPreset] = useState<CreatedPreset>(() =>
    recognizeCreatedPreset(value),
  );
  const focusCustom = useRef(false);
  const open = openGroup !== null;
  const [draft, setDraft] = useState(value);
  const [lastOpened, setLastOpened] = useState(openGroup);
  const signature = JSON.stringify(value);
  const [lastValue, setLastValue] = useState(signature);

  // Rehydrate on opening and on URL navigation, including Back/Forward.
  if (openGroup !== lastOpened || signature !== lastValue) {
    setLastValue(signature);
    setLastOpened(openGroup);
    if (open) {
      setDraft(value);
      setCreatedPreset(recognizeCreatedPreset(value));
    }
  }

  const pickers = useRef<
    Partial<Record<FilterControlKey, HTMLButtonElement | HTMLInputElement>>
  >({});

  const errors = {
    ...memberRangeErrors(draft),
    ...(createdPreset === "custom" ? createdRangeErrors(draft) : {}),
  };
  const invalid = Object.keys(errors).length > 0;

  const apply = (next: FilterSelection, preset = createdPreset): void => {
    if (Object.keys(memberRangeErrors(next)).length > 0) return;
    if (preset === "custom" && Object.keys(createdRangeErrors(next)).length > 0)
      return;
    const dates =
      preset === "custom" ? createdRange(next) : createdPresetRange(preset);
    onApply({ ...next, ...memberRange(next), ...dates });
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
          {FILTER_GROUPS.map((group) =>
            group.key === "disabled" ? (
              <div key={group.key} className="space-y-2">
                <label htmlFor={disabledId} className="text-sm font-medium">
                  Organization Status
                </label>
                <Select
                  value={
                    draft.disabled.length === 1 ? draft.disabled[0] : "all"
                  }
                  onValueChange={(status) =>
                    setDraft((previous) => ({
                      ...previous,
                      disabled:
                        status === "active" || status === "disabled"
                          ? [status]
                          : [],
                    }))
                  }
                >
                  <SelectTrigger
                    id={disabledId}
                    className="w-full"
                    ref={(node) => {
                      if (node) pickers.current.disabled = node;
                    }}
                  >
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="all">All</SelectItem>
                    <SelectItem value="active">Active</SelectItem>
                    <SelectItem value="disabled">Disabled</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            ) : (
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
            ),
          )}
          <fieldset className="grid gap-2">
            <legend className="mb-2 text-sm font-medium">
              Created date (UTC)
            </legend>
            <Select
              value={createdPreset}
              onValueChange={(value) => {
                const preset = CREATED_PRESETS.find(
                  (option) => option.value === value,
                )?.value;
                if (!preset) return;
                if (preset === "custom" && createdPreset !== "custom") {
                  setDraft((previous) => ({
                    ...previous,
                    ...createdPresetRange(createdPreset),
                  }));
                  focusCustom.current = true;
                } else if (preset === "all") {
                  setDraft((previous) => ({
                    ...previous,
                    ...createdPresetRange("all"),
                  }));
                }
                setCreatedPreset(preset);
              }}
            >
              <SelectTrigger
                id={createdId}
                aria-label="Created date (UTC)"
                className="w-full"
                ref={(node) => {
                  if (node) pickers.current.created = node;
                }}
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent
                onCloseAutoFocus={(event) => {
                  if (focusCustom.current) {
                    event.preventDefault();
                    pickers.current.createdFrom?.focus();
                    focusCustom.current = false;
                  }
                }}
              >
                {CREATED_PRESETS.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {createdPreset === "custom" && (
              <div className="grid grid-cols-2 gap-3">
                {(["createdFrom", "createdTo"] as const).map((key) => (
                  <div key={key} className="grid content-start gap-1.5">
                    <label
                      htmlFor={`${createdId}-${key}`}
                      className="text-sm font-medium"
                    >
                      {key === "createdFrom" ? "From (UTC)" : "To (UTC)"}
                    </label>
                    {/* Native date inputs erase malformed/incomplete values to an
                    empty string, indistinguishable from an unrestricted bound. */}
                    <Input
                      id={`${createdId}-${key}`}
                      type="text"
                      placeholder="YYYY-MM-DD"
                      autoComplete="off"
                      value={draft[key] ?? ""}
                      aria-invalid={Boolean(errors[key])}
                      aria-describedby={
                        errors[key] ? `${createdId}-${key}-error` : undefined
                      }
                      onChange={(event) =>
                        setDraft((previous) => ({
                          ...previous,
                          [key]: event.target.value,
                        }))
                      }
                      ref={(node) => {
                        if (node) pickers.current[key] = node;
                      }}
                    />
                    {errors[key] && (
                      <p
                        id={`${createdId}-${key}-error`}
                        role="alert"
                        className="text-destructive text-sm"
                      >
                        {errors[key]}
                      </p>
                    )}
                  </div>
                ))}
              </div>
            )}
          </fieldset>
          <fieldset className="grid gap-2">
            <legend className="mb-2 text-sm font-medium">Member count</legend>
            <div className="grid grid-cols-2 gap-3">
              {(["minMembers", "maxMembers"] as const).map((key) => (
                <div key={key} className="grid content-start gap-1.5">
                  <label
                    htmlFor={`${membersId}-${key}`}
                    className="text-sm font-medium"
                  >
                    {key === "minMembers" ? "Min" : "Max"}
                  </label>
                  <Input
                    id={`${membersId}-${key}`}
                    type="text"
                    inputMode="numeric"
                    value={draft[key] ?? ""}
                    aria-invalid={Boolean(errors[key])}
                    aria-describedby={
                      errors[key] ? `${membersId}-${key}-error` : undefined
                    }
                    onChange={(event) =>
                      setDraft((previous) => ({
                        ...previous,
                        [key]: event.target.value,
                      }))
                    }
                    ref={(node) => {
                      if (node) pickers.current[key] = node;
                    }}
                  />
                  {errors[key] && (
                    <p
                      id={`${membersId}-${key}-error`}
                      role="alert"
                      className="text-destructive text-sm"
                    >
                      {errors[key]}
                    </p>
                  )}
                </div>
              ))}
            </div>
          </fieldset>
        </div>

        <SheetFooter className="flex-row justify-end">
          {/* Clears the filters and nothing else. The search term is not a
              filter this sheet holds, and an operator who reset the filters has
              not asked to type their term again. */}
          <Button variant="ghost" onClick={() => apply(NO_FILTERS, "all")}>
            Clear all
          </Button>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button disabled={invalid} onClick={() => apply(draft)}>
            Apply
          </Button>
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
