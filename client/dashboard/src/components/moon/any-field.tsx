import React, { useId } from "react";
import { Label } from "./label";
import { cn } from "@/lib/utils";

export interface AnyFieldProps {
  id?: string;
  label: React.ReactNode;
  hint?: React.ReactNode;
  error?: React.ReactNode;
  optionality?: "visible" | "hidden";
}

export function AnyField({
  label,
  hint,
  error,
  render,
  optionality = "visible",
  ...rest
}: AnyFieldProps & {
  render: (props: {
    id: string;
    "aria-describedby": string;
  }) => React.ReactNode;
}) {
  const genid = useId();
  const id = rest.id ?? genid;
  const hintId = `${genid}-hint`;
  const errorId = `${genid}-error`;
  const descriptors = `${hintId} ${errorId}`;

  return (
    <div className="group flex flex-col gap-2">
      <Label
        htmlFor={id}
        className={cn(
          optionality === "visible"
            ? "after:text-muted-foreground after:ms-2 after:inline-block after:text-sm after:content-(--optional-label) group-has-[[readonly],[disabled],[required]]:after:content-['']"
            : null,
        )}
      >
        {label}
      </Label>
      {render({ id, "aria-describedby": descriptors })}
      {hint ? (
        <p
          id={hintId}
          className={cn("text-muted-foreground text-sm", error && "sr-only")}
        >
          {hint}
        </p>
      ) : null}
      {error ? (
        <p id={errorId} className="text-destructive text-sm">
          {error}
        </p>
      ) : null}
    </div>
  );
}
