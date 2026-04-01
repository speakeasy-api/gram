import { cn } from "@/lib/utils";
import { Icon, Stack } from "@speakeasy-api/moonshine";
import { SearchIcon, X } from "lucide-react";

interface FilterChip {
  display: string;
  value: string; // Unique identifier for the chip
}

export function MultiSearch({
  value,
  onChange,
  placeholder = "Search",
  className,
  disabled,
  chips = [],
  onRemoveChip,
}: {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  className?: string;
  disabled?: boolean;
  chips?: FilterChip[];
  onRemoveChip?: (chipValue: string) => void;
}) {
  return (
    <Stack
      direction="horizontal"
      gap={2}
      className={cn(
        "min-h-[42px] border border-border rounded-md px-3 py-2",
        disabled && "opacity-50 cursor-not-allowed",
        className,
      )}
      align="center"
    >
      <SearchIcon className="size-4 opacity-50 shrink-0" />

      {/* Filter chips */}
      {chips.length > 0 && (
        <div className="flex items-center gap-1.5 flex-wrap">
          {chips.map((chip) => (
            <div
              key={chip.value}
              className="inline-flex items-center gap-1 px-2 py-0.5 rounded-md bg-primary/10 text-primary border border-primary/20 text-xs"
            >
              <span className="font-medium">{chip.display}</span>
              {onRemoveChip && !disabled && (
                <button
                  onClick={() => onRemoveChip(chip.value)}
                  className="hover:bg-primary/20 rounded-full p-0.5 transition-colors"
                  aria-label={`Remove ${chip.display} filter`}
                >
                  <Icon name="x" className="size-2.5" />
                </button>
              )}
            </div>
          ))}
        </div>
      )}

      {/* Input */}
      <input
        placeholder={placeholder}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        disabled={disabled}
        className="flex-1 bg-transparent outline-none min-w-0 disabled:cursor-not-allowed"
      />

      {/* Clear button */}
      {value && !disabled && (
        <button
          onClick={() => onChange("")}
          className="opacity-50 hover:opacity-100 transition-opacity shrink-0"
          aria-label="Clear search"
        >
          <X className="size-4" />
        </button>
      )}
    </Stack>
  );
}
