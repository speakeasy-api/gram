import { QuerySamplesPopover } from "@/components/QuerySamplesPopover";
import {
  CommandEmpty,
  CommandGroup,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import {
  Popover,
  PopoverAnchor,
  PopoverContent,
} from "@/components/ui/popover";
import { cn } from "@/lib/utils";
import { Operator as Op } from "@gram/client/models/components/logfilter";
import { Command as CmdkRoot } from "cmdk";
import { Search, X } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import {
  type ActiveLogFilter,
  OP_LABELS,
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

interface LogFilterBarProps {
  filters: ActiveLogFilter[];
  onChange: (filters: ActiveLogFilter[]) => void;
  attributeKeys: string[];
  isLoadingKeys?: boolean;
  searchInput: string;
  onSearchInputChange: (value: string) => void;
  /** Called with the current search text when user explicitly submits (Enter/Tab/clear). */
  onSearchSubmit: (query: string) => void;
}

export function LogFilterBar({
  filters,
  onChange,
  attributeKeys,
  isLoadingKeys,
  searchInput,
  onSearchInputChange,
  onSearchSubmit,
}: LogFilterBarProps) {
  const [step, setStep] = useState<Step>("key");
  const [selectedKey, setSelectedKey] = useState("");
  const [selectedOp, setSelectedOp] = useState<Op | null>(null);
  const [filterValue, setFilterValue] = useState("");
  const [inputFocused, setInputFocused] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const popoverContentRef = useRef<HTMLDivElement>(null);
  const blurTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    return () => {
      if (blurTimeoutRef.current) clearTimeout(blurTimeoutRef.current);
    };
  }, []);

  // Filter attribute keys by typed text (only relevant in key step)
  const filteredKeys =
    step === "key"
      ? searchInput.length > 0
        ? attributeKeys.filter((k) =>
            k.toLowerCase().includes(searchInput.toLowerCase()),
          )
        : attributeKeys
      : [];

  // Show popover when there are matching suggestions or picking operator
  const popoverOpen =
    (step === "key" && inputFocused && filteredKeys.length > 0) ||
    step === "operator";

  const focusMainInput = useCallback(() => {
    // Use rAF to wait for React to commit the render (CmdkRoot.Input may remount)
    requestAnimationFrame(() => inputRef.current?.focus());
  }, []);

  const resetFlow = useCallback(() => {
    setStep("key");
    setSelectedKey("");
    setSelectedOp(null);
    setFilterValue("");
    focusMainInput();
  }, [focusMainInput]);

  const addFilter = useCallback(
    (path: string, op: Op, value?: string) => {
      const newFilter = { id: crypto.randomUUID(), path, op, value };
      // Replace any existing filter on the same path+op for equality to avoid
      // impossible AND conditions (e.g. status_code = 404 AND status_code = 500).
      // For != and ~ multiple filters on the same path are valid.
      const rest =
        op === Op.Eq || op === Op.In
          ? filters.filter((f) => !(f.path === path && f.op === op))
          : filters;
      onChange([...rest, newFilter]);
      onSearchInputChange("");
      resetFlow();
    },
    [filters, onChange, onSearchInputChange, resetFlow],
  );

  const removeFilter = (id: string) => {
    onChange(filters.filter((f) => f.id !== id));
  };

  const clearAll = () => {
    onChange([]);
    resetFlow();
  };

  const handleKeySelect = (key: string) => {
    setSelectedKey(key);
    setStep("operator");
    onSearchInputChange("");
    focusMainInput();
  };

  const handleOpSelect = (op: Op) => {
    setSelectedOp(op);
    setStep("value");
    setFilterValue("");
    onSearchInputChange("");
  };

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
      onSearchInputChange(value);
    },
    [step, onSearchInputChange, handleOpSelect],
  );

  const handleValueSubmit = () => {
    if (!selectedOp || !filterValue.trim()) return;
    addFilter(selectedKey, selectedOp, filterValue.trim());
  };

  const submitOrParse = useCallback(
    (raw: string) => {
      const parsed = tryParseFilterExpression(raw);
      if (parsed) {
        addFilter(parsed.key, parsed.op, parsed.value);
      } else {
        onSearchSubmit(raw);
      }
    },
    [addFilter, onSearchSubmit],
  );

  // During the operator step the user may type the operator + value directly
  // (e.g. "!= 200"). We prepend the already-selected key and parse the full
  // expression so it creates a chip without going through the popover.
  const trySubmitOperatorInput = useCallback(
    (raw: string): boolean => {
      if (!selectedKey || !raw) return false;
      const parsed = tryParseFilterExpression(`${selectedKey} ${raw}`);
      if (parsed) {
        addFilter(parsed.key, parsed.op, parsed.value);
        return true;
      }
      return false;
    },
    [selectedKey, addFilter],
  );

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
        if (popoverOpen) {
          e.preventDefault();
          onSearchInputChange("");
          resetFlow();
        }
        break;
      case "Backspace":
        if (searchInput === "" && step === "key" && filters.length > 0) {
          removeFilter(filters[filters.length - 1].id);
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
        }}
      >
        <PopoverAnchor asChild>
          <div
            className={cn(
              "border-border flex min-h-[42px] flex-wrap items-center gap-1.5 rounded-md border px-3 py-1.5 transition-[border-color,box-shadow]",
              inputFocused && "border-ring ring-ring/50 ring-[3px]",
            )}
          >
            <Search className="text-muted-foreground size-4 shrink-0" />
            {filters.map((filter) => (
              <button
                key={filter.id}
                onClick={() => removeFilter(filter.id)}
                className="border-border bg-accent text-accent-foreground hover:bg-accent/80 group inline-flex shrink-0 items-center gap-1 rounded-md border px-2 py-0.5 font-mono text-xs transition-colors"
              >
                <span>
                  {filter.path} {OP_LABELS[filter.op]}
                  {filter.value !== undefined ? ` ${filter.value}` : ""}
                </span>
                <X className="text-muted-foreground group-hover:text-foreground size-3" />
              </button>
            ))}
            {step === "operator" && (
              <span className="border-ring bg-accent text-accent-foreground inline-flex shrink-0 items-center gap-1 rounded-md border px-2 py-0.5 font-mono text-xs">
                {selectedKey}
              </span>
            )}
            {step === "value" ? (
              <span className="border-ring bg-accent text-accent-foreground inline-flex shrink-0 items-center rounded-md border px-2 py-0.5 font-mono text-xs">
                <span>
                  {selectedKey} {selectedOp && OP_LABELS[selectedOp]}
                  {"\u00A0"}
                </span>
                <input
                  type="text"
                  value={filterValue}
                  onChange={(e) => setFilterValue(e.target.value)}
                  onKeyDown={(e) => {
                    switch (e.key) {
                      case "Enter":
                      case "Tab":
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
                onBlur={() => {
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
                      : "Search by URN or filter (e.g. http.response.status_code != 200)"
                }
                className="min-h-[24px] min-w-[120px] flex-1 bg-transparent text-sm outline-none"
              />
            )}
            <QuerySamplesPopover
              title="Sample filter queries"
              ariaLabel="Show sample filter queries"
              samples={FILTER_QUERY_SAMPLES}
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
                  onSearchSubmit("");
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
                {filteredKeys.map((key) => (
                  <CommandItem
                    key={key}
                    value={key}
                    onSelect={() => handleKeySelect(key)}
                    className="cursor-pointer"
                  >
                    <span className="font-mono text-xs">{key}</span>
                  </CommandItem>
                ))}
              </CommandGroup>
            </CommandList>
          )}

          {step === "operator" && (
            <CommandList>
              <CommandGroup heading="Operator">
                {OP_OPTIONS.map((op) => (
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
    </CmdkRoot>
  );
}
