import { cn } from "@/lib/utils";

// Mirrors the visual contract of `Input` (see components/ui/moonshine/components/Input):
// a 1px hairline on the inset surface, squared corners, no shadow.
const textAreaClassNames = cn(
  "w-full resize-y border border-neutral-default bg-inset px-4 py-3",
  "font-sans text-sm font-light text-default shadow-none outline-none transition-colors",
  "placeholder:text-placeholder focus-visible:border-neutral-active",
  "disabled:cursor-not-allowed disabled:opacity-50",
  "aria-invalid:border-destructive-default",
);

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
  onKeyDown,
  onCompositionStart,
  onCompositionEnd,
  onPaste,
  ...rest
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
  onKeyDown?: React.KeyboardEventHandler<HTMLTextAreaElement>;
  onCompositionStart?: React.CompositionEventHandler<HTMLTextAreaElement>;
  onCompositionEnd?: React.CompositionEventHandler<HTMLTextAreaElement>;
  onPaste?: React.ClipboardEventHandler<HTMLTextAreaElement>;
}): JSX.Element {
  const handleChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    onChange?.(e.target.value);
  };

  return (
    <textarea
      id={id}
      name={name}
      value={value}
      onChange={handleChange}
      className={cn(textAreaClassNames, className)}
      disabled={disabled}
      placeholder={placeholder}
      rows={rows}
      required={required}
      defaultValue={defaultValue}
      onKeyDown={onKeyDown}
      onCompositionStart={onCompositionStart}
      onCompositionEnd={onCompositionEnd}
      onPaste={onPaste}
      {...rest}
    />
  );
}
