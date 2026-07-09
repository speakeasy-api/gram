import * as React from "react";

import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

function FieldGroup({
  className,
  ...props
}: React.ComponentProps<"div">): React.JSX.Element {
  return (
    <div
      data-slot="field-group"
      className={cn("flex flex-col gap-6", className)}
      {...props}
    />
  );
}

function Field({
  className,
  orientation = "vertical",
  ...props
}: React.ComponentProps<"div"> & {
  orientation?: "vertical" | "horizontal" | "responsive";
}): React.JSX.Element {
  return (
    <div
      role="group"
      data-slot="field"
      data-orientation={orientation}
      className={cn(
        "group/field flex w-full gap-2 data-[invalid=true]:text-destructive",
        orientation === "vertical" && "flex-col",
        orientation === "horizontal" && "flex-row items-center",
        orientation === "responsive" && "flex-col sm:flex-row sm:items-center",
        className,
      )}
      {...props}
    />
  );
}

function FieldLabel({
  className,
  ...props
}: React.ComponentProps<typeof Label>): React.JSX.Element {
  return (
    <Label
      data-slot="field-label"
      className={cn(
        // Claude Design brandbook: field labels are mono, uppercase, and
        // tracked — distinct from the base Label default (see label.tsx for
        // why that default was left sans/normal-case). `text-[var(--text-muted)]`
        // (not the `text-muted` utility) is intentional: `text-muted` carries
        // `!important` and would permanently win over the destructive variant
        // below, breaking the invalid-field color swap.
        "font-mono text-xs uppercase tracking-[0.08em] text-[var(--text-muted)]",
        "group-data-[invalid=true]/field:text-destructive",
        className,
      )}
      {...props}
    />
  );
}

function FieldDescription({
  className,
  ...props
}: React.ComponentProps<"p">): React.JSX.Element {
  return (
    <p
      data-slot="field-description"
      className={cn("text-muted-foreground text-sm", className)}
      {...props}
    />
  );
}

function FieldError({
  className,
  children,
  errors,
  ...props
}: React.ComponentProps<"div"> & {
  errors?: Array<{ message?: string } | undefined>;
}): React.JSX.Element | null {
  const messages = errors?.flatMap((error) => {
    if (!error?.message) return [];
    return [error.message];
  });

  if (!children && (!messages || messages.length === 0)) return null;

  return (
    <div
      data-slot="field-error"
      role="alert"
      className={cn("text-destructive text-sm", className)}
      {...props}
    >
      {children ??
        (messages && messages.length > 1 ? (
          <ul className="list-disc space-y-1 pl-4">
            {messages.map((message) => (
              <li key={message}>{message}</li>
            ))}
          </ul>
        ) : (
          messages?.[0]
        ))}
    </div>
  );
}

export { Field, FieldDescription, FieldError, FieldGroup, FieldLabel };
