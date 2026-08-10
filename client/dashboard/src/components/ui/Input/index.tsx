import { cn } from "@/lib/utils";
import * as React from "react";
import { useLayoutEffect, useRef, useState } from "react";
import { Icon } from "../Icon";
import { IconName } from "../Icon/names";

export interface InputProps extends Omit<
  React.ComponentProps<"input">,
  "onChange" | "value"
> {
  value?: string;
  /** Called with the field's value, not the change event. */
  onChange?: (value: string) => void;
  /** Optional leading icon, rendered inside the field's border. */
  icon?: IconName;
  /** Renders a textarea. Also implied by `lines > 1`. */
  multiline?: boolean;
  /** Number of visible rows; anything above 1 renders a textarea. */
  lines?: number;
  /** Fires on Enter. */
  onEnter?: () => void;
  /**
   * Returns `true`/`undefined` when valid, `false` for a generic message, or a
   * string to show that message under the field.
   */
  validate?: (value: string) => boolean | string | undefined;
  /** Forces the error styling regardless of `validate`. */
  error?: boolean;
  /** A prefix the value must carry; shown as static text inside the field. */
  requiredPrefix?: string;
  className?: string;
}

const DEFAULT_ERROR = "Invalid value";

export function Input({
  value,
  onChange,
  onEnter,
  validate,
  icon,
  multiline,
  lines,
  error,
  requiredPrefix,
  className,
  children,
  disabled,
  placeholder,
  ...props
}: InputProps): React.JSX.Element {
  const runValidation = (val: string) => {
    if (val === "") return null;

    const result = validate?.(val);
    if (result === false) return DEFAULT_ERROR;
    if (typeof result === "string") return result;
    return null;
  };

  const [validationError, setValidationError] = useState<string | null>(() =>
    runValidation(value ?? ""),
  );
  const [isFocused, setIsFocused] = useState(false);

  const handleFocus = (
    event: React.FocusEvent<HTMLInputElement | HTMLTextAreaElement>,
  ) => {
    props.onFocus?.(event as React.FocusEvent<HTMLInputElement>);
    setIsFocused(true);
  };

  const handleBlur = (
    event: React.FocusEvent<HTMLInputElement | HTMLTextAreaElement>,
  ) => {
    props.onBlur?.(event as React.FocusEvent<HTMLInputElement>);
    setIsFocused(false);
  };

  const handleChange = (
    event: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>,
  ) => {
    const raw = event.target.value;
    const next =
      requiredPrefix && !raw.startsWith(requiredPrefix)
        ? `${requiredPrefix}${raw}`
        : raw;
    setValidationError(runValidation(next));
    onChange?.(next);
  };

  const handleKeyDown = (
    event: React.KeyboardEvent<HTMLInputElement | HTMLTextAreaElement>,
  ) => {
    if (event.key === "Enter") onEnter?.();
    props.onKeyDown?.(event as React.KeyboardEvent<HTMLInputElement>);
  };

  // Scrolling while hovering a focused number input natively increments or
  // decrements the value — an easy way to silently corrupt a form (e.g. a
  // billing limit) while scrolling the page. Blur on wheel so the scroll
  // passes through without editing the number.
  const handleWheel = (event: React.WheelEvent<HTMLInputElement>) => {
    if (props.type === "number") event.currentTarget.blur();
    props.onWheel?.(event);
  };

  // The prefix is painted over the field, so the text has to start after it.
  const prefixRef = useRef<HTMLSpanElement>(null);
  const [prefixWidth, setPrefixWidth] = useState(0);
  useLayoutEffect(() => {
    if (prefixRef.current) setPrefixWidth(prefixRef.current.offsetWidth);
  }, [requiredPrefix]);

  const fieldClassName = cn(
    "h-full w-full bg-transparent text-sm text-default shadow-none outline-none placeholder:text-placeholder disabled:cursor-not-allowed disabled:opacity-50",
    isFocused && "placeholder:text-default",
  );

  const displayValue =
    requiredPrefix && value?.startsWith(requiredPrefix)
      ? value.slice(requiredPrefix.length)
      : value;

  const asTextarea = multiline || (lines ?? 0) > 1;

  const field = asTextarea ? (
    <textarea
      {...(props as React.ComponentProps<"textarea">)}
      value={displayValue}
      placeholder={placeholder}
      disabled={disabled}
      rows={lines ?? 10}
      onChange={handleChange}
      onKeyDown={handleKeyDown}
      onFocus={handleFocus}
      onBlur={handleBlur}
      className={cn(fieldClassName, "max-h-60 min-h-16 resize-y py-1")}
    />
  ) : (
    <input
      {...props}
      value={displayValue}
      placeholder={placeholder}
      disabled={disabled}
      onChange={handleChange}
      onKeyDown={handleKeyDown}
      onFocus={handleFocus}
      onBlur={handleBlur}
      onWheel={handleWheel}
      style={{
        ...(requiredPrefix ? { paddingLeft: prefixWidth } : {}),
        ...props.style,
      }}
      className={fieldClassName}
    />
  );

  const hasError = Boolean(error) || Boolean(validationError);

  return (
    <div className="relative">
      <div
        className={cn(
          "flex items-center gap-3 border border-input bg-surface-primary-default px-4 py-2 text-muted-foreground",
          icon && "px-3",
          isFocused && "border-focus text-default",
          hasError && "border-destructive-default",
          className,
        )}
      >
        {icon && <Icon name={icon} size="small" />}
        {requiredPrefix && (
          <span
            ref={prefixRef}
            className="pointer-events-none absolute text-sm text-muted-foreground select-none"
            aria-hidden="true"
          >
            {requiredPrefix}
          </span>
        )}
        {field}
      </div>
      {validationError && validationError !== DEFAULT_ERROR && (
        <span className="text-xs text-default-destructive">
          {validationError}
        </span>
      )}
      {children}
    </div>
  );
}
