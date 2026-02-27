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
import { Op } from "@gram/client/models/components/attributefilter";
import { Command as CmdkRoot } from "cmdk";
import { Search, X } from "lucide-react";
import { useCallback, useRef, useState } from "react";
import {
  type ActiveAttributeFilter,
  OP_LABELS,
  VALUELESS_OPS,
} from "./attribute-filter-types";

type Step = "key" | "operator" | "value";

const OP_OPTIONS: { value: Op; label: string; description: string }[] = [
  { value: Op.Eq, label: "equals", description: "Exact match" },
  {
    value: Op.NotEq,
    label: "not equals",
    description: "Exclude exact match",
  },
  { value: Op.Contains, label: "contains", description: "Partial match" },
  { value: Op.Exists, label: "exists", description: "Attribute is present" },
  {
    value: Op.NotExists,
    label: "not exists",
    description: "Attribute is absent",
  },
];

interface AttributeFilterBarProps {
  filters: ActiveAttributeFilter[];
  onChange: (filters: ActiveAttributeFilter[]) => void;
  attributeKeys: string[];
  isLoadingKeys?: boolean;
  searchInput: string;
  onSearchInputChange: (value: string) => void;
}

export function AttributeFilterBar({
  filters,
  onChange,
  attributeKeys,
  isLoadingKeys,
  searchInput,
  onSearchInputChange,
}: AttributeFilterBarProps) {
  const [step, setStep] = useState<Step>("key");
  const [selectedKey, setSelectedKey] = useState("");
  const [selectedOp, setSelectedOp] = useState<Op | null>(null);
  const [filterValue, setFilterValue] = useState("");
  const [inputFocused, setInputFocused] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  // Filter attribute keys by typed text (only relevant in key step)
  const filteredKeys =
    step === "key" && searchInput.length > 0
      ? attributeKeys.filter((k) =>
          k.toLowerCase().includes(searchInput.toLowerCase()),
        )
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
      onChange([...filters, { id: crypto.randomUUID(), path, op, value }]);
      onSearchInputChange("");
      resetFlow();
    },
    [filters, onChange, onSearchInputChange, resetFlow],
  );

  const removeFilter = (id: string) => {
    onChange(filters.filter((f) => f.id !== id));
  };

  const clearAll = () => onChange([]);

  const handleKeySelect = (key: string) => {
    setSelectedKey(key);
    setStep("operator");
    onSearchInputChange("");
  };

  const handleOpSelect = (op: Op) => {
    if (VALUELESS_OPS.includes(op)) {
      addFilter(selectedKey, op);
    } else {
      setSelectedOp(op);
      setStep("value");
      setFilterValue("");
      // Value input renders inline with autoFocus
    }
  };

  const handleValueSubmit = () => {
    if (!selectedOp || !filterValue.trim()) return;
    addFilter(selectedKey, selectedOp, filterValue.trim());
  };

  const handleInputKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Escape" && popoverOpen) {
      e.preventDefault();
      onSearchInputChange("");
      resetFlow();
    }
    if (
      e.key === "Backspace" &&
      searchInput === "" &&
      step === "key" &&
      filters.length > 0
    ) {
      removeFilter(filters[filters.length - 1].id);
    }
  };

  return (
    <CmdkRoot
      shouldFilter={false}
      className="relative overflow-visible bg-transparent"
    >
      <Popover open={popoverOpen}>
        <PopoverAnchor asChild>
          <div
            className={cn(
              "flex flex-wrap items-center gap-1.5 min-h-[42px] border border-border rounded-md px-3 py-1.5 transition-[border-color,box-shadow]",
              inputFocused && "border-ring ring-[3px] ring-ring/50",
            )}
          >
            <Search className="size-4 text-muted-foreground shrink-0" />
            {filters.map((filter) => (
              <button
                key={filter.id}
                onClick={() => removeFilter(filter.id)}
                className="inline-flex items-center gap-1 px-2 py-0.5 rounded-md border border-border bg-accent text-accent-foreground text-xs font-mono hover:bg-accent/80 transition-colors group shrink-0"
              >
                <span>
                  {filter.path} {OP_LABELS[filter.op]}
                  {filter.value !== undefined ? ` ${filter.value}` : ""}
                </span>
                <X className="size-3 text-muted-foreground group-hover:text-foreground" />
              </button>
            ))}
            {step === "operator" && (
              <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-md border border-ring bg-accent text-accent-foreground text-xs font-mono shrink-0">
                {selectedKey}
              </span>
            )}
            {step === "value" ? (
              <span className="inline-flex items-center gap-0.5 px-2 py-0.5 rounded-md border border-ring bg-accent text-accent-foreground text-xs font-mono shrink-0">
                <span>{selectedKey} {selectedOp && OP_LABELS[selectedOp]}</span>
                <input
                  type="text"
                  value={filterValue}
                  onChange={(e) => setFilterValue(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") handleValueSubmit();
                    if (e.key === "Escape") {
                      e.preventDefault();
                      resetFlow();
                    }
                    if (e.key === "Backspace" && filterValue === "") {
                      resetFlow();
                    }
                  }}
                  placeholder="value"
                  className="bg-transparent outline-none text-xs font-mono w-[80px]"
                  style={{ width: `${Math.max(filterValue.length, 5)}ch` }}
                  autoFocus
                />
              </span>
            ) : (
              <CmdkRoot.Input
                ref={inputRef}
                value={searchInput}
                onValueChange={onSearchInputChange}
                onKeyDown={handleInputKeyDown}
                onFocus={() => setInputFocused(true)}
                onBlur={() => {
                  // Delay so click events on popover items fire first
                  setTimeout(() => setInputFocused(false), 150);
                }}
                placeholder={
                  step === "operator"
                    ? "Select operator..."
                    : filters.length > 0
                      ? "Add filter..."
                      : "Search by tool URN or filter by attribute"
                }
                className="flex-1 min-w-[120px] bg-transparent outline-none min-h-[24px] text-sm"
              />
            )}
            {searchInput && (
              <button
                onClick={() => {
                  onSearchInputChange("");
                  resetFlow();
                }}
                className="text-muted-foreground hover:text-foreground transition-opacity shrink-0"
                aria-label="Clear search"
              >
                <X className="size-4" />
              </button>
            )}
            {filters.length > 0 && !searchInput && (
              <button
                onClick={clearAll}
                className="text-muted-foreground hover:text-foreground transition-opacity shrink-0"
                aria-label="Clear all filters"
              >
                <X className="size-4" />
              </button>
            )}
          </div>
        </PopoverAnchor>

        <PopoverContent
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
              <CommandGroup heading="Filter by attribute">
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
                    <div className="flex items-center justify-between w-full">
                      <span className="font-mono text-xs">
                        {OP_LABELS[op.value]}
                      </span>
                      <span className="text-xs text-muted-foreground">
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
