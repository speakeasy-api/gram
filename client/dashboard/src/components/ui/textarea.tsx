import { cn } from "@/lib/utils";

export const textAreaClassNames =
  "w-full border-2 rounded-lg py-1 px-2 resize-y";

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
