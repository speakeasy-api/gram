// TODO: https://linear.app/speakeasy/issue/SXF-171/input-component
import { cn } from "@/lib/utils";
import { Icon } from "./icon";
import { IconName } from "./icon/names";
import { useCallback, useState } from "react";

export interface InputProps extends React.InputHTMLAttributes<
  HTMLInputElement | HTMLTextAreaElement
> {
  icon?: IconName;
  multiline?: boolean;
  error?: boolean;
  className?: string;
}

export function Input({
  value,
  onChange,
  placeholder,
  disabled,
  icon,
  multiline,
  error,
  className,
  onFocus,
  onBlur,
  ...props
}: InputProps): React.JSX.Element {
  const [isFocused, setIsFocused] = useState(false);

  const handleFocus = useCallback(
    (event: React.FocusEvent<HTMLInputElement | HTMLTextAreaElement>) => {
      if (onFocus) {
        onFocus(event);
      }
      setIsFocused(true);
    },
    [onFocus],
  );
  const handleBlur = useCallback(
    (event: React.FocusEvent<HTMLInputElement | HTMLTextAreaElement>) => {
      if (onBlur) {
        onBlur(event);
      }
      setIsFocused(false);
    },
    [onBlur],
  );

  const commonProps = {
    value,
    onChange,
    placeholder,
    disabled,
  } as const;

  let element: React.ReactNode = (
    <input
      {...commonProps}
      {...props}
      onFocus={handleFocus}
      onBlur={handleBlur}
      className={cn(
        "h-full w-full bg-inset font-sans text-sm font-light text-default shadow-none outline-none placeholder:text-placeholder disabled:cursor-not-allowed disabled:opacity-50",
        isFocused && "placeholder:text-default",
      )}
    />
  );

  if (multiline) {
    element = (
      <textarea
        {...commonProps}
        {...props}
        onFocus={handleFocus}
        onBlur={handleBlur}
        cols={30}
        rows={10}
        className={cn(
          "my-2 h-full max-h-60 min-h-16 w-full bg-inset px-3 py-3 font-sans text-sm font-light text-default shadow-none outline-none placeholder:text-placeholder disabled:cursor-not-allowed disabled:opacity-50",
          isFocused && "placeholder:text-default",
        )}
      />
    );
  }

  return (
    <div
      className={cn(
        "flex items-center gap-3 border border-neutral-default bg-inset px-4 py-3 text-muted-foreground shadow-none transition-colors",
        icon && "px-3",
        isFocused && "border-neutral-active text-default",
        error && "border-destructive-default",
        className,
      )}
    >
      {icon && <Icon name={icon} size="small" />}
      {element}
    </div>
  );
}
