import { Badge } from "@/components/ui/Badge";
import {
  Command,
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
import { Check, ChevronsUpDown, Plus, XIcon } from "lucide-react";
import { useState, type JSX } from "react";
import {
  MAX_FILTER_VALUES,
  type FilterOperator,
  type WindowPreset,
} from "./exploreModel";
import {
  DIMENSION_VALUES_LIMIT,
  useDimensionValues,
} from "./useDimensionValues";

/**
 * The values a filter compares against, picked from what the dimension
 * holds inside the window rather than typed blind. Every listed value is one
 * the query would match, shown with how many rows carry it. A typed value
 * that is not listed can still be added, set apart from the listed ones,
 * because the list stops at the endpoint's cap and a busy dimension can hold
 * more.
 */
export function FilterValuePicker({
  dataset,
  dimension,
  window,
  operator,
  values,
  onChange,
}: {
  dataset: string;
  dimension: string;
  window: WindowPreset;
  operator: FilterOperator;
  values: string[];
  onChange: (values: string[]) => void;
}): JSX.Element {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const single = operator === "equals";
  const listed = useDimensionValues(dataset, dimension, window, open);

  const all = listed.data?.values ?? [];
  const needle = search.trim().toLowerCase();
  const shown = needle
    ? all.filter((entry) => entry.value.toLowerCase().includes(needle))
    : all;
  const typed = search.trim();
  const canAddTyped =
    typed !== "" &&
    !all.some((entry) => entry.value === typed) &&
    !values.includes(typed);

  const pick = (value: string) => {
    if (single) {
      onChange([value]);
      setOpen(false);
    } else if (values.includes(value)) {
      onChange(values.filter((current) => current !== value));
    } else if (values.length < MAX_FILTER_VALUES) {
      onChange([...values, value]);
    }
    setSearch("");
  };
  const remove = (value: string) =>
    onChange(values.filter((current) => current !== value));

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) setSearch("");
      }}
    >
      <PopoverTrigger asChild>
        <button
          type="button"
          role="combobox"
          aria-expanded={open}
          aria-label={single ? "Filter value" : "Filter values"}
          className="border-input bg-surface-primary-default focus-visible:border-focus flex min-h-9 min-w-64 max-w-3xl flex-wrap items-center gap-1.5 border px-3 py-1.5 text-left text-sm focus-visible:outline-none"
        >
          {values.length === 0 ? (
            <span className="text-muted-foreground">
              {single ? "Pick a value" : "Pick values"}
            </span>
          ) : single ? (
            <span className="truncate font-mono">{values[0]}</span>
          ) : (
            values.map((value) => (
              <Badge
                key={value}
                variant="neutral"
                className="max-w-full normal-case"
              >
                <Badge.Text className="min-w-0 truncate font-mono [text-box-trim:none]">
                  {value}
                </Badge.Text>
                <Badge.RightIcon>
                  <span
                    role="button"
                    tabIndex={0}
                    aria-label={`Remove ${value}`}
                    onClick={(event) => {
                      event.stopPropagation();
                      remove(value);
                    }}
                    onKeyDown={(event) => {
                      if (event.key === "Enter" || event.key === " ") {
                        event.preventDefault();
                        event.stopPropagation();
                        remove(value);
                      }
                    }}
                    className="flex h-3 w-3 cursor-pointer items-center justify-center hover:opacity-70"
                  >
                    <XIcon className="h-3 w-3" />
                  </span>
                </Badge.RightIcon>
              </Badge>
            ))
          )}
          <ChevronsUpDown className="text-muted-foreground ml-auto size-4 shrink-0" />
        </button>
      </PopoverTrigger>
      <PopoverContent className="w-80 p-0" align="start">
        <Command shouldFilter={false} label="Filter values">
          <CommandInput
            placeholder="Search or type a value"
            value={search}
            onValueChange={setSearch}
            className="h-9"
          />
          <CommandList>
            {canAddTyped ? (
              <CommandGroup>
                <CommandItem
                  value={`__typed__${typed}`}
                  onSelect={() => pick(typed)}
                  className="text-muted-foreground cursor-pointer italic"
                >
                  <Plus className="size-3.5" />
                  <span className="truncate">
                    Use &ldquo;{typed}&rdquo; (not in the list)
                  </span>
                </CommandItem>
              </CommandGroup>
            ) : null}
            <ValueList
              state={
                listed.isError
                  ? "error"
                  : listed.isPending
                    ? "loading"
                    : all.length === 0
                      ? "empty"
                      : shown.length === 0
                        ? "no-match"
                        : "ready"
              }
              shown={shown}
              selected={values}
              onPick={pick}
              capped={all.length >= DIMENSION_VALUES_LIMIT}
            />
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

function ValueList({
  state,
  shown,
  selected,
  onPick,
  capped,
}: {
  state: "loading" | "error" | "empty" | "no-match" | "ready";
  shown: { value: string; count: number }[];
  selected: string[];
  onPick: (value: string) => void;
  capped: boolean;
}): JSX.Element {
  // cmdk's own empty state is off with its filtering, so the states are
  // rendered by hand, each as plain text rather than an item.
  if (state === "loading") {
    return <Notice>Loading values…</Notice>;
  }
  if (state === "error") {
    return <Notice>Values did not load. You can still type one.</Notice>;
  }
  if (state === "empty") {
    return <Notice>No values in this window.</Notice>;
  }
  if (state === "no-match") {
    return <Notice>No listed value matches.</Notice>;
  }
  return (
    <CommandGroup>
      {shown.map((entry) => {
        const picked = selected.includes(entry.value);
        return (
          <CommandItem
            key={entry.value}
            value={entry.value}
            onSelect={() => onPick(entry.value)}
            className="cursor-pointer"
          >
            <Check
              className={cn("size-4 shrink-0", picked ? "" : "opacity-0")}
            />
            <span className="min-w-0 flex-1 truncate font-mono">
              {entry.value}
            </span>
            <span className="text-muted-foreground ml-auto shrink-0 font-mono text-xs">
              {entry.count.toLocaleString()}
            </span>
          </CommandItem>
        );
      })}
      {capped ? (
        <Notice>
          Showing the {DIMENSION_VALUES_LIMIT} most frequent. Type a value to
          add one that is not listed.
        </Notice>
      ) : null}
    </CommandGroup>
  );
}

function Notice({ children }: { children: React.ReactNode }): JSX.Element {
  return (
    <div className="text-muted-foreground px-2 py-2 text-xs">{children}</div>
  );
}
