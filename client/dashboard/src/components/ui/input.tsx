import * as React from "react";

import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import { Stack } from "@speakeasy-api/moonshine";
import { useState } from "react";

interface InputProps extends React.ComponentProps<"input"> {
  onEnter?: () => void;
  validate?: (value: string) => boolean | string;
}

const DEFAULT_ERROR = "Invalid value";

function Input({ className, type, onEnter, validate, ...props }: InputProps) {
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
    const value = e.target.value;
    setError(v(value));
    props.onChange?.(e);
  };

  const input = (
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
