import { Stack } from "@speakeasy-api/moonshine";
import { Skeleton } from "./skeleton";

import { cn } from "@/lib/utils";

export function Type({
  variant = "body",
  muted,
  children,
  skeleton = "word",
  className,
}: {
  variant?: "subheading" | "body" | "small";
  muted?: boolean;
  children: React.ReactNode;
  className?: string;
  skeleton?: "word" | "phrase" | "line" | "paragraph";
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
                index !== lines - 1 ? "w-full" : "w-[200px]",
              )}
            />
          ))}
        </Stack>
      );
    }

    return <Skeleton className={cn(variantWidth, variantHeight)} />;
  }

  switch (variant) {
    case "subheading":
      return (
        <p
          className={`text-md font-light ${
            muted ? "text-muted-foreground" : "text-foreground"
          } ${className}`}
        >
          {children}
        </p>
      );
    case "body":
      return (
        <p
          className={`text-base font-light ${
            muted ? "text-muted-foreground" : "text-foreground"
          } ${className}`}
        >
          {children}
        </p>
      );
    case "small":
      return (
        <p
          className={`text-sm font-light ${
            muted ? "text-muted-foreground" : "text-foreground"
          } ${className}`}
        >
          {children}
        </p>
      );
  }
}
