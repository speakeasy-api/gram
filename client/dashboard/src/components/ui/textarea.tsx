import { cn } from "@/lib/utils";

export function TextArea({
  value,
  onChange,
  disabled,
  placeholder,
  className,
  rows = 3,
}: {
  onChange: (value: string) => void;
  value?: string;
  disabled?: boolean;
  placeholder?: string;
  className?: string;
  rows?: number;
}) {
  return (
    <textarea
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className={cn("w-full border-2 rounded-lg py-1 px-2", className)}
      disabled={disabled}
      placeholder={placeholder}
      rows={rows}
    />
  );
}
