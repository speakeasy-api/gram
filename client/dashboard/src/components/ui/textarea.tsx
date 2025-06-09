import { cn } from "@/lib/utils";

export function TextArea({
  id,
  name,
  value,
  onChange,
  disabled,
  placeholder,
  className,
  rows = 3,
  required,
  defaultValue,
}: {
  id?: string;
  name?: string;
  onChange?: (value: string) => void;
  value?: string;
  disabled?: boolean;
  placeholder?: string;
  className?: string;
  rows?: number;
  required?: boolean;
  defaultValue?: string | undefined;
}) {
  const handleChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    onChange?.(e.target.value);
  };

  return (
    <textarea
      id={id}
      name={name}
      value={value}
      onChange={handleChange}
      className={cn("w-full border-2 rounded-lg py-1 px-2 resize-y", className)}
      disabled={disabled}
      placeholder={placeholder}
      rows={rows}
      required={required}
      defaultValue={defaultValue}
    />
  );
}
