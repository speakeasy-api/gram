import * as React from "react";

import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import { useLayoutEffect, useRef, useState } from "react";
import { TextArea } from "./textarea";

interface InputProps extends Omit<
  React.ComponentProps<"input">,
  "handleChange" | "onChange" | "value"
> {
  value?: string;
  onEnter?: () => void;
  validate?: (value: string) => boolean | string;
  lines?: number;
  onChange?: (value: string) => void;
  requiredPrefix?: string;
}

const DEFAULT_ERROR = "Invalid value";

function Input({
  className,
  type,
  onEnter,
  validate,
  lines,
  requiredPrefix,
  children,
  ...props
}: InputProps) {
  const v = (val: string) => {
    if (val === "") {
      return null;
    }

    const validationResult = validate?.(val);
    if (validationResult === false) {
      return DEFAULT_ERROR;
    } else if (typeof validationResult === "string") {
      return validationResult;
    } else {
      return null;
    }
  };

  const [error, setError] = useState<string | null>(
    v(props.value?.toString() ?? ""),
  );

  const handleKeyDown = (
    e: React.KeyboardEvent<HTMLInputElement | HTMLTextAreaElement>,
  ) => {
    if (e.key === "Enter" && onEnter) {
      onEnter();
    }
    // Call the original onKeyDown if it exists
    props.onKeyDown?.(e as React.KeyboardEvent<HTMLInputElement>);
  };

  const onChange = (value: string) => {
    const finalValue =
      requiredPrefix && !value.startsWith(requiredPrefix)
        ? `${requiredPrefix}${value}`
        : value;
    setError(v(finalValue));
    props.onChange?.(finalValue);
  };

  const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    onChange(e.target.value);
  };

  // --- Prefix width measurement logic ---
  const prefixRef = useRef<HTMLSpanElement>(null);
  const [prefixWidth, setPrefixWidth] = useState(0);

  useLayoutEffect(() => {
    if (prefixRef.current) {
      setPrefixWidth(prefixRef.current.offsetWidth);
    }
  }, [requiredPrefix]);

  const {
    onKeyDown: _,
    onCompositionStart: __,
    onCompositionEnd: ___,
    onPaste: ____,
    ...restProps
  } = props;
  const input =
    lines && lines > 1 ? (
      <TextArea
        data-slot="input"
        className={cn(className)}
        onKeyDown={
          handleKeyDown as React.KeyboardEventHandler<HTMLTextAreaElement>
        }
        onChange={onChange}
        rows={lines}
        {...restProps}
        defaultValue={
          typeof props.defaultValue === "string"
            ? props.defaultValue
            : undefined
        }
      />
    ) : (
      <input
        type={type}
        data-slot="input"
        className={cn(
          "file:text-foreground placeholder:text-muted-foreground selection:bg-primary selection:text-primary-foreground dark:bg-input/30 border-input flex h-9 w-full min-w-0 rounded-md border bg-transparent px-3 py-1 text-base shadow-xs transition-[color,box-shadow] outline-none file:inline-flex file:h-7 file:border-0 file:bg-transparent file:text-sm file:font-medium disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 md:text-sm",
          "focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px]",
          "aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 aria-invalid:border-destructive",
          error && "ring-destructive/50! border-destructive!",
          className,
        )}
        style={{
          ...(requiredPrefix ? { paddingLeft: prefixWidth + 12 } : {}),
          ...props.style,
        }}
        onKeyDown={handleKeyDown}
        {...props}
        onChange={handleChange}
        value={
          props.value?.startsWith(requiredPrefix ?? "")
            ? props.value.replace(requiredPrefix ?? "", "")
            : props.value
        }
      />
    );

  return (
    <div className="mb-[-8px] relative">
      {requiredPrefix && (
        <span
          ref={prefixRef}
          className="absolute left-3 top-[8px] text-muted-foreground text-sm pointer-events-none select-none"
          aria-hidden="true"
        >
          {requiredPrefix}
        </span>
      )}
      {input}
      {error && error !== DEFAULT_ERROR ? (
        <Type variant="small" className="text-destructive! h-4">
          {error}
        </Type>
      ) : (
        <div className="h-[8px]" />
      )}
      {children}
    </div>
  );
}

export { Input };
