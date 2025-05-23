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
}: {
  variant?: "subheading" | "body" | "small";
  className?: string;
  muted?: boolean;
  italic?: boolean;
  mono?: boolean;
  skeleton?: "word" | "phrase" | "line" | "paragraph";
  children: React.ReactNode;
}) {
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
                index !== lines - 1 ? "w-full" : "w-[200px]"
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
  }

  switch (variant) {
    case "subheading":
      return <p className={`text-md ${baseClass} ${className}`}>{children}</p>;
    case "body":
      return (
        <p className={`text-base ${baseClass} ${className}`}>{children}</p>
      );
    case "small":
      return <p className={`text-sm ${baseClass} ${className}`}>{children}</p>;
  }
}
