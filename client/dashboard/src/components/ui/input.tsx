import * as React from "react";

import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import { Stack } from "@speakeasy-api/moonshine";
import { useState } from "react";
import { TextArea } from "./textarea";

interface InputProps extends Omit<React.ComponentProps<"input">, "handleChange" | "onChange" | "value"> {
  value?: string;
  onEnter?: () => void;
  validate?: (value: string) => boolean | string;
  lines?: number;
  onChange?: (value: string) => void;
}

const DEFAULT_ERROR = "Invalid value";

function Input({ className, type, onEnter, validate, lines, ...props }: InputProps) {
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
    v(props.value?.toString() ?? "")
  );

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter" && onEnter) {
      onEnter();
    }
    // Call the original onKeyDown if it exists
    props.onKeyDown?.(e);
  };

  const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    onChange(e.target.value);
  };
  const onChange = (value: string) => {
    setError(v(value));
    props.onChange?.(value);
  };

  const input = lines && lines > 1 ? (
    <TextArea
      data-slot="input"
      className={cn(className)}
      onKeyDown={handleKeyDown}
      onChange={onChange}
      rows={lines}
      {...props}
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
        className
      )}
      onKeyDown={handleKeyDown}
      {...props}
      onChange={handleChange}
    />
  );

  return (
    <Stack gap={1}>
      {input}
      {error && error !== DEFAULT_ERROR && (
        <Type variant="small" className="text-destructive!">
          {error}
        </Type>
      )}
    </Stack>
  );
}

export { Input };
