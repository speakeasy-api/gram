import { cn } from "@/lib/utils";
import { Stack } from "@speakeasy-api/moonshine";
import { SearchIcon } from "lucide-react";

export function SearchBar({
  value,
  onChange,
  placeholder = "Search",
  className,
}: {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  className?: string;
}) {
  return (
    <Stack
      direction="horizontal"
      gap={2}
      className={cn("border rounded-md px-2 py-1", className)}
      align="center"
    >
      <SearchIcon className="size-4 opacity-50" />
      <input
        placeholder={placeholder}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="bg-transparent outline-none"
      />
    </Stack>
  );
}
