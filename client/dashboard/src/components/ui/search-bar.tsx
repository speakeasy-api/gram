import { cn } from "@/lib/utils";
import { Stack } from "@speakeasy-api/moonshine";
import { SearchIcon, X } from "lucide-react";

export function SearchBar({
  value,
  onChange,
  placeholder = "Search",
  className,
  disabled,
}: {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  className?: string;
  disabled?: boolean;
}) {
  return (
    <Stack
      direction="horizontal"
      gap={2}
      className={cn(
        "border-border h-[42px] rounded-md border px-3",
        disabled && "cursor-not-allowed opacity-50",
        className,
      )}
      align="center"
    >
      <SearchIcon className="size-4 opacity-50" />
      <input
        placeholder={placeholder}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        disabled={disabled}
        className="min-w-0 flex-1 bg-transparent outline-none disabled:cursor-not-allowed"
      />
      {value && !disabled && (
        <button
          onClick={() => onChange("")}
          className="opacity-50 transition-opacity hover:opacity-100"
          aria-label="Clear search"
        >
          <X className="size-4" />
        </button>
      )}
    </Stack>
  );
}
