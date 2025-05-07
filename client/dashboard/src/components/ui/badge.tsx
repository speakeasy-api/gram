import * as React from "react";
import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";
import {
  TooltipProvider,
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "./tooltip";
import { Skeleton } from "./skeleton";

const badgeVariants = cva(
  "inline-flex items-center justify-center rounded-md border px-2 py-0.5 text-xs font-medium w-fit whitespace-nowrap shrink-0 [&>svg]:size-3 gap-1 [&>svg]:pointer-events-none focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px] aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 aria-invalid:border-destructive transition-[color,box-shadow] overflow-hidden",
  {
    variants: {
      variant: {
        default:
          "border-transparent bg-primary text-primary-foreground [a&]:hover:bg-primary/90",
        secondary:
          "border-transparent bg-secondary text-secondary-foreground [a&]:hover:bg-secondary/90",
        destructive:
          "border-transparent bg-destructive text-white [a&]:hover:bg-destructive/90 focus-visible:ring-destructive/20 dark:focus-visible:ring-destructive/40 dark:bg-destructive/60",
        outline:
          "text-foreground [a&]:hover:bg-accent [a&]:hover:text-accent-foreground",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
);

export function Badge({
  className,
  variant,
  size = "md",
  asChild = false,
  tooltip,
  isLoading = false,
  ...props
}: React.ComponentProps<"span"> &
  VariantProps<typeof badgeVariants> & {
    asChild?: boolean;
    tooltip?: string;
    size?: "sm" | "md";
    isLoading?: boolean;
  }) {
  const Comp = asChild ? Slot : "span";

  const heightModifier = {
    outline: 2, // Outline badges look smaller than they really are
    default: 0,
    secondary: 0,
    destructive: 0,
  }[variant ?? "default"];

  const sizeClass = {
    sm: `text-xs px-1 rounded-sm h-${5 + heightModifier}`,
    md: `text-sm px-2 rounded-md h-${6 + heightModifier}`,
  }[size];

  if (isLoading || props.children === undefined) {
    return <Skeleton className={cn(sizeClass, "w-24")} />;
  }

  const base = (
    <Comp
      data-slot="badge"
      className={cn(badgeVariants({ variant }), sizeClass, className)}
      {...props}
    />
  );

  if (tooltip) {
    return (
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger asChild>{base}</TooltipTrigger>
          <TooltipContent>{tooltip}</TooltipContent>
        </Tooltip>
      </TooltipProvider>
    );
  }

  return base;
}
