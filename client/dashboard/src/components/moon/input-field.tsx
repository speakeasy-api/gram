import React from "react";
import { Input } from "./input";
import { AnyField, AnyFieldProps } from "./any-field";

export interface InputFieldProps
  extends AnyFieldProps,
    React.ComponentProps<"input"> {}

export function InputField({
  id,
  label,
  hint,
  error,
  ...props
}: InputFieldProps) {
  return (
    <AnyField
      id={id}
      label={label}
      hint={hint}
      error={error}
      render={(extraProps) => (
        <Input
          {...props}
          {...extraProps}
          aria-describedby={[
            props["aria-describedby"],
            extraProps["aria-describedby"],
          ]
            .filter(Boolean)
            .join(" ")}
        />
      )}
    />
  );
}
