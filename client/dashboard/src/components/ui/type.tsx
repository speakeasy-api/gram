import { Stack } from "@speakeasy-api/moonshine";
import { Skeleton } from "./skeleton";

import { cn } from "@/lib/utils";

export function Type({
  variant = "body",
  muted,
  children,
  skeleton = "word",
  className,
  italic,
  mono,
  small,
  destructive,
  as: Component = "p",
  ...props
}: {
  variant?: "subheading" | "body" | "small";
  muted?: boolean;
  italic?: boolean;
  mono?: boolean;
  small?: boolean;
  skeleton?: "word" | "phrase" | "line" | "paragraph";
  destructive?: boolean;
  as?: React.ElementType;
  children?: React.ReactNode;
} & Omit<React.ComponentProps<"p">, "children">) {
  if (children === undefined) {
    const variantHeight = {
      subheading: "h-6",
      body: "h-5",
      small: "h-4",
    }[variant];

    const variantWidth = {
      word: "w-[100px]",
      phrase: "w-[300px]",
      line: "w-full",
      paragraph: "w-full",
    }[skeleton];

    if (className?.includes("line-clamp")) {
      skeleton = "paragraph";
    }

    if (skeleton === "paragraph") {
      let lines = 3;
      if (className?.includes("line-clamp")) {
        lines = parseInt(className.split("line-clamp-")[1] ?? "3");
      }

      return (
        <Stack gap={1}>
          {Array.from({ length: lines }).map((_, index) => (
            <Skeleton
              key={index}
              className={cn(
                variantHeight,
                index !== lines - 1 ? "w-full" : "w-[200px]",
              )}
            />
          ))}
        </Stack>
      );
    }

    return <Skeleton className={cn(variantWidth, variantHeight)} />;
  }

  let baseClass = "font-light";

  if (mono) {
    baseClass += " font-mono";
  }

  if (italic) {
    baseClass += " italic";
  }

  if (muted) {
    baseClass += " text-muted-foreground";
  } else if (destructive) {
    baseClass += " text-default-destructive";
  } else {
    baseClass += " text-stone-800 dark:text-stone-200";
  }

  if (small) {
    baseClass += mono ? " text-xs" : " text-sm";
  }

  const El = Component as React.ComponentType<
    React.HTMLAttributes<HTMLElement> & { children?: React.ReactNode }
  >;

  switch (variant) {
    case "subheading":
      return (
        <El
          {...props}
          className={`text-md font-medium ${baseClass} ${className}`}
        >
          {children}
        </El>
      );
    case "body":
      return (
        <El {...props} className={`text-base ${baseClass} ${className}`}>
          {children}
        </El>
      );
    case "small":
      return (
        <El {...props} className={`text-sm ${baseClass} ${className}`}>
          {children}
        </El>
      );
  }
}
