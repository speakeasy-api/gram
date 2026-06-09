import * as React from "react";

import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

function FieldSet({
  className,
  ...props
}: React.ComponentProps<"fieldset">): React.JSX.Element {
  return (
    <fieldset
      data-slot="field-set"
      className={cn("flex flex-col gap-6", className)}
      {...props}
    />
  );
}

function FieldLegend({
  className,
  variant = "legend",
  ...props
}: React.ComponentProps<"legend"> & {
  variant?: "legend" | "label";
}): React.JSX.Element {
  return (
    <legend
      data-slot="field-legend"
      data-variant={variant}
      className={cn(
        "mb-3 font-medium",
        variant === "legend" ? "text-base" : "text-sm",
        className,
      )}
      {...props}
    />
  );
}

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

function FieldContent({
  className,
  ...props
}: React.ComponentProps<"div">): React.JSX.Element {
  return (
    <div
      data-slot="field-content"
      className={cn("flex flex-col gap-1.5", className)}
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
        "group-data-[invalid=true]/field:text-destructive",
        className,
      )}
      {...props}
    />
  );
}

function FieldTitle({
  className,
  ...props
}: React.ComponentProps<"div">): React.JSX.Element {
  return (
    <div
      data-slot="field-title"
      className={cn("text-sm font-medium", className)}
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

function FieldSeparator({
  className,
  ...props
}: React.ComponentProps<"div">): React.JSX.Element {
  return (
    <div
      data-slot="field-separator"
      className={cn("bg-border h-px w-full", className)}
      {...props}
    />
  );
}

export {
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSeparator,
  FieldSet,
  FieldTitle,
};
