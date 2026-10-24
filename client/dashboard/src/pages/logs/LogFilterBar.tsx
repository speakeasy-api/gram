import { QuerySamplesPopover } from "@/components/QuerySamplesPopover";
import { Button } from "@/components/ui/Button";
import {
  CommandEmpty,
  CommandGroup,
  CommandItem,
  CommandList,
} from "@/components/ui/Command";
import {
  Popover,
  PopoverAnchor,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/Popover";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { cn } from "@/lib/utils";
import { Operator as Op } from "@gram/client/models/components/logfilter";
import { Command as CmdkRoot } from "cmdk";
import { Search, X } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import {
  type ActiveLogFilter,
  OP_LABELS,
  applyFilterAdd,
  applyFilterEdit,
  parseOperatorSymbol,
  tryParseFilterExpression,
} from "./log-filter-types";

const FILTER_QUERY_SAMPLES = [
  {
    value: "http.response.status_code != 200",
    label: "Non-2xx responses",
  },
  {
    value: "http.response.status_code = 500",
    label: "Server errors only",
  },
  {
    value: "severity_text = ERROR",
    label: "Error-level logs",
  },
  {
    value: "http.request.method = POST",
    label: "POST requests only",
  },
  {
    value: "gram.tool.name ~ search",
    label: "Tool names containing 'search'",
  },
];

type Step = "key" | "operator" | "value";

const OP_OPTIONS: { value: Op; label: string; description: string }[] = [
  { value: Op.Eq, label: "equals", description: "Exact match" },
  { value: Op.NotEq, label: "not equals", description: "Exclude exact match" },
  { value: Op.Contains, label: "contains", description: "Partial match" },
  {
    value: Op.In,
    label: "in",
    description: "Match any of a comma-separated list",
  },
];

export interface FilterKeyOption {
  key: string;
  label?: string;
  description?: string;
  count?: number;
}

export interface FilterValueOption {
  value: string;
  label: string;
  count?: number;
}

interface LogFilterBarProps {
  filters: ActiveLogFilter[];
  onChange: (filters: ActiveLogFilter[]) => void;
  attributeKeys: string[];
  keyOptions?: FilterKeyOption[];
  /** A field with a single operator skips choosing it. */
  operatorsFor?: (key: string) => Op[];
  /** Reads typed filter expressions the built-in syntax cannot express. */
  parseExpression?: (
    input: string,
  ) => { key: string; op: Op; value: string } | null;
  valueOptions?: FilterValueOption[];
  onValueInputChange?: (key: string, text: string) => void;
  formatValue?: (path: string, value: string, op: Op) => string;
  /** A message refuses the filter; `next` is the list the change would leave. */
  validateFilter?: (
    path: string,
    op: Op,
    value: string | undefined,
    next: ActiveLogFilter[],
  ) => string | null;
  valueNotice?: string;
  /** Encodes an offered value as a single `in` item. */
  encodeInValue?: (value: string) => string;
  /** Fields whose values are opaque and never trimmed. */
  exactValue?: (path: string) => boolean;
  samples?: { value: string; label: string }[];
  placeholder?: string;
  isLoadingKeys?: boolean;
  searchInput: string;
  onSearchInputChange: (value: string) => void;
  /** Called with the current search text when user explicitly submits (Enter/Tab/clear). */
  onSearchSubmit: (query: string) => void;
  /** Shown as a removable chip whenever the box holds a different draft. */
  appliedSearch?: string;
}

export function LogFilterBar({
  filters,
  onChange,
  attributeKeys,
  isLoadingKeys,
  searchInput,
  onSearchInputChange,
  onSearchSubmit,
  appliedSearch,
  parseExpression,
  keyOptions,
  operatorsFor,
  valueOptions,
  onValueInputChange,
  formatValue,
  validateFilter,
  valueNotice,
  encodeInValue,
  exactValue,
  samples = FILTER_QUERY_SAMPLES,
  placeholder = "Search by URN or filter (e.g. http.response.status_code != 200)",
}: LogFilterBarProps): JSX.Element {
  const [step, setStep] = useState<Step>("key");
  const [selectedKey, setSelectedKey] = useState("");
  const [selectedOp, setSelectedOp] = useState<Op | null>(null);
  const [filterValue, setFilterValue] = useState("");
  const [inputFocused, setInputFocused] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const popoverContentRef = useRef<HTMLDivElement>(null);
  const barRef = useRef<HTMLDivElement>(null);
  const blurTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    return () => {
      if (blurTimeoutRef.current) clearTimeout(blurTimeoutRef.current);
    };
  }, []);

  const keys = keyOptions?.map((option) => option.key) ?? attributeKeys;
  const keyLabel = (key: string) =>
    keyOptions?.find((option) => option.key === key)?.label ?? key;
  // Clearing a draft leaves a separately applied search in place.
  const clearsAppliedSearch =
    appliedSearch === undefined || searchInput.trim() === appliedSearch;
  // Filter attribute keys by typed text (only relevant in key step)
  const filteredKeys =
    step === "key"
      ? searchInput.length > 0
        ? keys.filter((k) =>
            [k, keyLabel(k)].some((text) =>
              text.toLowerCase().includes(searchInput.toLowerCase()),
            ),
          )
        : keys
      : [];
  const operatorOptions = operatorsFor
    ? OP_OPTIONS.filter((option) =>
        operatorsFor(selectedKey).includes(option.value),
      )
    : OP_OPTIONS;
  const valueSuggestions = step === "value" ? (valueOptions ?? []) : [];

  // Show popover when there are matching suggestions or picking operator
  const popoverOpen =
    (step === "key" && inputFocused && filteredKeys.length > 0) ||
    step === "operator" ||
    valueSuggestions.length > 0;

  const focusMainInput = useCallback(() => {
    // Use rAF to wait for React to commit the render (CmdkRoot.Input may remount)
    requestAnimationFrame(() => inputRef.current?.focus());
  }, []);

  const resetFlow = useCallback(() => {
    if (selectedKey) onValueInputChange?.(selectedKey, "");
    setProblem(null);
    setStep("key");
    setSelectedKey("");
    setSelectedOp(null);
    setFilterValue("");
    focusMainInput();
  }, [focusMainInput, onValueInputChange, selectedKey]);

  const addFilter = useCallback(
    (path: string, op: Op, value?: string) => {
      const next = applyFilterAdd(filters, { path, op, value });
      const refused = validateFilter?.(path, op, value, next) ?? null;
      if (refused) {
        setProblem(refused);
        return;
      }
      onChange(next);
      onSearchInputChange("");
      resetFlow();
    },
    [filters, onChange, onSearchInputChange, resetFlow, validateFilter],
  );

  const removeFilter = useCallback(
    (id: string) => {
      onChange(filters.filter((f) => f.id !== id));
    },
    [filters, onChange],
  );

  const editFilter = useCallback(
    (id: string, op: Op, value?: string) => {
      onChange(applyFilterEdit(filters, id, { op, value }));
    },
    [filters, onChange],
  );

  const clearAll = () => {
    onChange([]);
    resetFlow();
  };

  const handleOpSelect = useCallback(
    (op: Op, key = selectedKey) => {
      setSelectedOp(op);
      setStep("value");
      setFilterValue("");
      onSearchInputChange("");
      onValueInputChange?.(key, "");
    },
    [onSearchInputChange, onValueInputChange, selectedKey],
  );

  const handleKeySelect = (key: string) => {
    setSelectedKey(key);
    const only = operatorsFor?.(key);
    if (only?.length === 1) {
      handleOpSelect(only[0]!, key);
    } else {
      setStep("operator");
      onSearchInputChange("");
    }
    focusMainInput();
  };

  const parse = useCallback(
    (input: string) =>
      parseExpression?.(input) ?? tryParseFilterExpression(input),
    [parseExpression],
  );

  // Wraps the parent onSearchInputChange. During the operator step, if the
  // user types a recognised operator followed by a space (e.g. "!= ") we
  // immediately transition to the value step instead of waiting for Enter.
  const handleSearchInputChange = useCallback(
    (value: string) => {
      if (step === "operator" && value.endsWith(" ")) {
        const symbol = value.trimEnd();
        const op = parseOperatorSymbol(symbol);
        if (op) {
          handleOpSelect(op);
          onSearchInputChange("");
          return;
        }
      }
      setProblem(null);
      onSearchInputChange(value);
      // Emptying the box clears the applied search without needing the ×
      // button, so results refresh as soon as the query is gone.
      if (step === "key" && value === "" && clearsAppliedSearch) {
        onSearchSubmit("");
      }
    },
    [
      step,
      onSearchInputChange,
      handleOpSelect,
      onSearchSubmit,
      clearsAppliedSearch,
    ],
  );

  const handleValueSubmit = () => {
    if (!selectedOp || !filterValue.trim()) return;
    addFilter(
      selectedKey,
      selectedOp,
      exactValue?.(selectedKey) ? filterValue : filterValue.trim(),
    );
  };

  const submitOrParse = useCallback(
    (raw: string) => {
      const parsed = parse(raw);
      if (parsed) {
        addFilter(parsed.key, parsed.op, parsed.value);
      } else {
        onSearchSubmit(raw);
      }
    },
    [addFilter, onSearchSubmit, parse],
  );

  // During the operator step the user may type the operator + value directly
  // (e.g. "!= 200"). We prepend the already-selected key and parse the full
  // expression so it creates a chip without going through the popover.
  const trySubmitOperatorInput = useCallback(
    (raw: string): boolean => {
      if (!selectedKey || !raw) return false;
      const parsed = parse(`${selectedKey} ${raw}`);
      if (parsed) {
        addFilter(parsed.key, parsed.op, parsed.value);
        return true;
      }
      return false;
    },
    [selectedKey, addFilter, parse],
  );

  // True when focus is moving to something outside the filter bar (e.g. the
  // sheet's "Done" button) rather than to one of the bar's own controls or a
  // popover item. Popover items live in a portal, so they read as "outside"
  // too — that's fine, since the pending-input commit below is a no-op for the
  // partial text present while navigating the popover.
  const isFocusLeavingBar = useCallback((e: React.FocusEvent) => {
    const next = e.relatedTarget as Node | null;
    return !next || !barRef.current?.contains(next);
  }, []);

  // Commit whatever the user has typed but not yet turned into a chip. Used on
  // blur so clicking "Done" (or anywhere outside the bar) saves the filter
  // instead of silently discarding it. Returns true when a chip was added.
  const commitPendingInput = useCallback((): boolean => {
    const raw = searchInput.trim();
    if (!raw) return false;
    if (step === "key") {
      const parsed = parse(raw);
      if (parsed) {
        addFilter(parsed.key, parsed.op, parsed.value);
        return true;
      }
      return false;
    }
    if (step === "operator") {
      return trySubmitOperatorInput(raw);
    }
    return false;
  }, [searchInput, step, addFilter, trySubmitOperatorInput, parse]);

  const handleInputKeyDown = (e: React.KeyboardEvent) => {
    switch (e.key) {
      case "Tab":
        e.preventDefault();
        if (step === "operator" && searchInput.trim()) {
          // Prefer typed expression over popover selection
          if (trySubmitOperatorInput(searchInput.trim())) break;
        }
        if (popoverOpen) {
          // Select the highlighted item in the popover. Scope the query to the
          // popover content element (rendered in a portal) to avoid accidentally
          // clicking an item in a different cmdk instance on the page.
          (popoverContentRef.current ?? document)
            .querySelector<HTMLElement>("[cmdk-item][aria-selected='true']")
            ?.click();
        } else if (step === "key" && searchInput.trim()) {
          submitOrParse(searchInput.trim());
        }
        break;
      case "Enter":
        if (step === "operator" && searchInput.trim()) {
          if (trySubmitOperatorInput(searchInput.trim())) {
            // Stop the event from bubbling to cmdk's root handler, which would
            // otherwise select the highlighted operator item after we already
            // created the filter via resetFlow.
            e.preventDefault();
            e.stopPropagation();
            break;
          }
        }
        if (step === "key" && !popoverOpen) {
          submitOrParse(searchInput.trim());
        }
        break;
      case "Escape":
        if (step === "operator") {
          // Cancel an in-progress filter build without touching the applied
          // search. Consume the key so it doesn't also close a surrounding
          // popover.
          e.preventDefault();
          e.stopPropagation();
          resetFlow();
        } else if (searchInput) {
          // Key step: mirror the × button — clear the box and the applied
          // search so results refresh without forcing a click. Consume the
          // key so this first Escape only clears; once the box is empty the
          // next Escape bubbles (e.g. to close a surrounding popover).
          e.preventDefault();
          e.stopPropagation();
          onSearchInputChange("");
          resetFlow();
          if (clearsAppliedSearch) onSearchSubmit("");
        }
        break;
      case "Backspace":
        if (searchInput === "" && step === "key" && filters.length > 0) {
          removeFilter(filters[filters.length - 1]!.id!);
        }
        if (searchInput === "" && step === "operator") {
          resetFlow();
        }
        break;
    }
  };

  const valuePlaceholder =
    selectedOp === Op.In ? "value1, value2, ..." : "value";

  return (
    <CmdkRoot
      shouldFilter={false}
      className="relative overflow-visible bg-transparent"
    >
      <Popover
        open={popoverOpen}
        onOpenChange={(open) => {
          // Allow clicking outside to dismiss the operator step
          if (!open && step === "operator") resetFlow();
          // A typed value is saved by the value input's blur.
          if (!open && step === "value" && !filterValue.trim()) resetFlow();
        }}
      >
        <PopoverAnchor asChild>
          <div
            ref={barRef}
            className={cn(
              "border-border flex min-h-[42px] flex-wrap items-center gap-1.5 border px-3 py-1.5 transition-[border-color,box-shadow]",
              inputFocused && "border-ring ring-ring/50 ring-[3px]",
            )}
          >
            <Search className="text-muted-foreground size-4 shrink-0" />
            {filters.map((filter) => (
              <EditableFilterChip
                key={filter.id}
                filter={filter}
                onEdit={editFilter}
                onRemove={removeFilter}
                formatValue={formatValue}
                formatKey={keyLabel}
                operatorsFor={operatorsFor}
                validateFilter={
                  validateFilter &&
                  ((path, op, value) =>
                    validateFilter(
                      path,
                      op,
                      value,
                      applyFilterEdit(filters, filter.id, { op, value }),
                    ))
                }
                exactValue={exactValue}
              />
            ))}
            {appliedSearch && searchInput.trim() !== appliedSearch && (
              <span className="border-border bg-background text-foreground inline-flex shrink-0 items-center border font-mono text-xs">
                <span className="max-w-60 truncate py-0.5 pr-1 pl-2">
                  search: {appliedSearch}
                </span>
                <button
                  type="button"
                  aria-label="Remove search"
                  onClick={() => onSearchSubmit("")}
                  className="text-muted-foreground hover:text-foreground hover:bg-muted px-1.5 py-0.5 transition-colors"
                >
                  <X className="size-3" />
                </button>
              </span>
            )}
            {step === "operator" && (
              <span className="border-ring bg-accent text-accent-foreground inline-flex shrink-0 items-center gap-1 border px-2 py-0.5 font-mono text-xs">
                {keyLabel(selectedKey)}
              </span>
            )}
            {step === "value" ? (
              <span className="border-ring bg-accent text-accent-foreground inline-flex shrink-0 items-center border px-2 py-0.5 font-mono text-xs">
                <span>
                  {keyLabel(selectedKey)}
                  {operatorsFor?.(selectedKey).length === 1
                    ? ""
                    : ` ${selectedOp ? OP_LABELS[selectedOp] : ""}`}
                  {"\u00A0"}
                </span>
                <input
                  type="text"
                  value={filterValue}
                  onChange={(e) => {
                    setProblem(null);
                    setFilterValue(e.target.value);
                    onValueInputChange?.(selectedKey, e.target.value);
                  }}
                  onKeyDown={(e) => {
                    switch (e.key) {
                      case "Tab":
                        e.preventDefault();
                        if (valueSuggestions.length > 0) {
                          (popoverContentRef.current ?? document)
                            .querySelector<HTMLElement>(
                              "[cmdk-item][aria-selected='true']",
                            )
                            ?.click();
                        } else {
                          handleValueSubmit();
                        }
                        break;
                      case "Enter":
                        e.preventDefault();
                        handleValueSubmit();
                        break;
                      case "Escape":
                        e.preventDefault();
                        resetFlow();
                        break;
                      case "Backspace":
                        if (filterValue === "") resetFlow();
                        break;
                    }
                  }}
                  onBlur={(e) => {
                    // Clicking "Done" (or away) with a value typed but not yet
                    // submitted should save it rather than discard it.
                    if (isFocusLeavingBar(e)) handleValueSubmit();
                  }}
                  placeholder={valuePlaceholder}
                  className="w-[80px] bg-transparent font-mono text-xs outline-none"
                  style={{
                    width: `${Math.max(filterValue.length, valuePlaceholder.length)}ch`,
                  }}
                  autoFocus
                />
              </span>
            ) : (
              <CmdkRoot.Input
                ref={inputRef}
                value={searchInput}
                onValueChange={handleSearchInputChange}
                onKeyDown={handleInputKeyDown}
                onFocus={() => setInputFocused(true)}
                onBlur={(e) => {
                  if (blurTimeoutRef.current)
                    clearTimeout(blurTimeoutRef.current);
                  // When focus leaves the bar entirely (e.g. clicking "Done"),
                  // commit a fully-typed expression so it isn't lost. This runs
                  // synchronously before the sheet unmounts the input, unlike
                  // the timeout below. A no-op while navigating the popover,
                  // where the text is still partial.
                  if (isFocusLeavingBar(e) && commitPendingInput()) return;
                  // Delay so click events on popover items fire first
                  blurTimeoutRef.current = setTimeout(
                    () => setInputFocused(false),
                    150,
                  );
                }}
                placeholder={
                  step === "operator"
                    ? "Type operator + value (e.g. != 200) or select below..."
                    : filters.length > 0
                      ? "Add filter..."
                      : placeholder
                }
                className="min-h-[24px] min-w-[120px] flex-1 bg-transparent text-sm outline-none"
              />
            )}
            <QuerySamplesPopover
              title="Sample filter queries"
              ariaLabel="Show sample filter queries"
              samples={samples}
              onSelect={(sample) => {
                onSearchInputChange(sample.value);
                requestAnimationFrame(() => inputRef.current?.focus());
              }}
            />
            {searchInput && (
              <button
                onClick={() => {
                  onSearchInputChange("");
                  resetFlow();
                  if (clearsAppliedSearch) onSearchSubmit("");
                }}
                className="text-muted-foreground hover:text-foreground shrink-0 transition-opacity"
                aria-label="Clear search"
              >
                <X className="size-4" />
              </button>
            )}
            {filters.length > 0 && !searchInput && (
              <button
                onClick={clearAll}
                className="text-muted-foreground hover:text-foreground shrink-0 transition-opacity"
                aria-label="Clear all filters"
              >
                <X className="size-4" />
              </button>
            )}
          </div>
        </PopoverAnchor>

        <PopoverContent
          ref={popoverContentRef}
          className="p-0"
          align="start"
          style={{ width: "var(--radix-popover-trigger-width)" }}
          onOpenAutoFocus={(e) => e.preventDefault()}
          onCloseAutoFocus={(e) => e.preventDefault()}
          onInteractOutside={(e) => {
            if (barRef.current?.contains(e.target as Node)) e.preventDefault();
          }}
        >
          {step === "key" && (
            <CommandList>
              <CommandEmpty>
                {isLoadingKeys
                  ? "Loading attributes..."
                  : "No matching attributes."}
              </CommandEmpty>
              <CommandGroup
                heading={
                  searchInput.length > 0
                    ? "Filter by attribute"
                    : "Available filters"
                }
              >
                {filteredKeys.map((key) => {
                  const option = keyOptions?.find((o) => o.key === key);
                  return (
                    <CommandItem
                      key={key}
                      value={key}
                      onSelect={() => handleKeySelect(key)}
                      className="cursor-pointer"
                    >
                      <span className="w-40 shrink-0 font-mono text-xs">
                        {keyLabel(key)}
                      </span>
                      {option?.description && (
                        <span className="text-muted-foreground min-w-0 flex-1 truncate text-xs">
                          {option.description}
                        </span>
                      )}
                      {option?.count !== undefined && (
                        <span className="text-muted-foreground ml-auto font-mono text-xs tabular-nums">
                          {option.count.toLocaleString()}
                        </span>
                      )}
                    </CommandItem>
                  );
                })}
              </CommandGroup>
            </CommandList>
          )}

          {valueSuggestions.length > 0 && (
            <CommandList>
              <CommandGroup heading={keyLabel(selectedKey)}>
                {valueSuggestions.map((option) => (
                  <CommandItem
                    key={option.value}
                    value={option.value}
                    // Keeps the value input focused, so its blur commit cannot
                    // add the typed text before this selection.
                    onMouseDown={(e) => e.preventDefault()}
                    onSelect={() => {
                      if (!selectedOp) return;
                      addFilter(
                        selectedKey,
                        selectedOp,
                        selectedOp === Op.In && encodeInValue
                          ? encodeInValue(option.value)
                          : option.value,
                      );
                    }}
                    className="cursor-pointer"
                  >
                    <span className="min-w-0 flex-1 truncate text-xs">
                      {option.label}
                    </span>
                    {option.count !== undefined && (
                      <span className="text-muted-foreground font-mono text-xs tabular-nums">
                        {option.count.toLocaleString()}
                      </span>
                    )}
                  </CommandItem>
                ))}
              </CommandGroup>
            </CommandList>
          )}

          {step === "operator" && (
            <CommandList>
              <CommandGroup heading="Operator">
                {operatorOptions.map((op) => (
                  <CommandItem
                    key={op.value}
                    value={op.label}
                    onSelect={() => handleOpSelect(op.value)}
                    className="cursor-pointer"
                  >
                    <div className="flex w-full items-center justify-between">
                      <span className="font-mono text-xs">
                        {OP_LABELS[op.value]}
                      </span>
                      <span className="text-muted-foreground text-xs">
                        {op.description}
                      </span>
                    </div>
                  </CommandItem>
                ))}
              </CommandGroup>
            </CommandList>
          )}
        </PopoverContent>
      </Popover>
      {(problem ?? (step === "value" ? valueNotice : undefined)) && (
        <p role="status" className="text-muted-foreground mt-1 text-xs">
          {problem ?? valueNotice}
        </p>
      )}
    </CmdkRoot>
  );
}

/** A single-operator field omits it: `Last used > 7d`, `Last used: No recorded use`. */
function chipText(key: string, op: Op | null, value?: string): string {
  if (value === undefined) return op ? `${key} ${OP_LABELS[op]}` : key;
  if (op) return `${key} ${OP_LABELS[op]} ${value}`;
  return /^[<>]/.test(value) ? `${key} ${value}` : `${key}: ${value}`;
}

interface EditableFilterChipProps {
  filter: ActiveLogFilter;
  onEdit: (id: string, op: Op, value?: string) => void;
  onRemove: (id: string) => void;
  formatValue?: (path: string, value: string, op: Op) => string;
  formatKey: (path: string) => string;
  operatorsFor?: (key: string) => Op[];
  validateFilter?: (path: string, op: Op, value?: string) => string | null;
  exactValue?: (path: string) => boolean;
}

function EditableFilterChip({
  filter,
  onEdit,
  onRemove,
  formatValue,
  formatKey,
  operatorsFor,
  validateFilter,
  exactValue,
}: EditableFilterChipProps) {
  const [problem, setProblem] = useState<string | null>(null);
  const ops = operatorsFor
    ? OP_OPTIONS.filter((option) =>
        operatorsFor(filter.path).includes(option.value),
      )
    : OP_OPTIONS;
  const [open, setOpen] = useState(false);
  const [op, setOp] = useState<Op>(filter.op);
  const [value, setValue] = useState(filter.value ?? "");

  useEffect(() => {
    if (open) {
      setOp(filter.op);
      setValue(filter.value ?? "");
      setProblem(null);
    }
  }, [open, filter.op, filter.value]);

  const save = () => {
    if (!value.trim()) return;
    const trimmed = exactValue?.(filter.path) ? value : value.trim();
    const refused = validateFilter?.(filter.path, op, trimmed) ?? null;
    setProblem(refused);
    if (refused) return;
    onEdit(filter.id, op, trimmed);
    setOpen(false);
  };

  const valuePlaceholder = op === Op.In ? "value1, value2, ..." : "value";

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <span className="border-border bg-background text-foreground inline-flex shrink-0 items-center border font-mono text-xs">
        <PopoverTrigger asChild>
          <button
            type="button"
            aria-label={`Edit filter ${formatKey(filter.path)}`}
            className="hover:bg-muted cursor-pointer py-0.5 pr-1 pl-2 transition-colors"
          >
            {chipText(
              formatKey(filter.path),
              ops.length === 1 ? null : filter.op,
              filter.value === undefined
                ? undefined
                : formatValue
                  ? formatValue(filter.path, filter.value, filter.op)
                  : filter.value,
            )}
          </button>
        </PopoverTrigger>
        <button
          type="button"
          aria-label={`Remove filter ${formatKey(filter.path)}`}
          onClick={(e) => {
            e.stopPropagation();
            onRemove(filter.id);
          }}
          className="text-muted-foreground hover:text-foreground hover:bg-muted px-1.5 py-0.5 transition-colors"
        >
          <X className="size-3" />
        </button>
      </span>
      <PopoverContent
        align="start"
        className="w-[320px] p-3"
        onOpenAutoFocus={(e) => e.preventDefault()}
      >
        <div
          className="text-muted-foreground mb-2 font-mono text-xs break-all"
          title={filter.path}
        >
          {formatKey(filter.path)}
        </div>
        <div className="mb-3 flex gap-2">
          <Select value={op} onValueChange={(v) => setOp(v as Op)}>
            <SelectTrigger className="!h-8 w-[120px]">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {ops.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  <span className="font-mono text-xs">
                    {OP_LABELS[option.value]}
                  </span>
                  <span className="text-muted-foreground ml-2 text-xs">
                    {option.label}
                  </span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <input
            type="text"
            value={value}
            onChange={(e) => setValue(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                save();
              } else if (e.key === "Escape") {
                e.preventDefault();
                setOpen(false);
              }
            }}
            placeholder={valuePlaceholder}
            className="border-border focus-visible:border-ring focus-visible:ring-ring/50 h-8 min-w-0 flex-1 border bg-transparent px-2 font-mono text-xs outline-none focus-visible:ring-[3px]"
            autoFocus
          />
        </div>
        {problem && (
          <p role="status" className="text-muted-foreground mb-3 text-xs">
            {problem}
          </p>
        )}
        <div className="flex items-center justify-between">
          <Button
            type="button"
            variant="destructive-secondary"
            size="sm"
            onClick={() => {
              onRemove(filter.id);
              setOpen(false);
            }}
          >
            Remove
          </Button>
          <div className="flex gap-2">
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={() => setOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="button"
              size="sm"
              onClick={save}
              disabled={!value.trim()}
            >
              Save
            </Button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}
