import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import * as React from "react";

import { cn } from "@/lib/utils";
import { Skeleton } from "./skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "./tooltip";

const badgeVariants = cva(
  "inline-flex items-center justify-center rounded-md border px-2 py-0.5 text-xs font-medium w-fit whitespace-nowrap shrink-0 [&>svg]:size-3 gap-1 [&>svg]:pointer-events-none focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px] aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 aria-invalid:border-destructive transition-[color,box-shadow] overflow-hidden",
  {
    variants: {
      variant: {
        default:
          "border-transparent bg-primary text-primary-foreground [a&]:hover:bg-primary/90",
        secondary:
          "border-transparent bg-primary/5 text-secondary-foreground [a&]:hover:bg-secondary/90",
        destructive:
          "border-transparent bg-destructive text-white [a&]:hover:bg-destructive/90 focus-visible:ring-destructive/20 dark:focus-visible:ring-destructive/40 dark:bg-destructive/60",
        warning:
          "border-transparent bg-yellow-500 [a&]:hover:bg-yellow-500/90 focus-visible:ring-yellow-500/20 dark:focus-visible:ring-yellow-500/40 dark:bg-yellow-700",
        "urgent-warning":
          "border-transparent bg-yellow-500 [a&]:hover:bg-yellow-500/90 focus-visible:ring-yellow-500/20 dark:focus-visible:ring-yellow-500/40 dark:bg-yellow-700 ring-2 ring-orange-500 dark:ring-orange-700 dark:text-white",
        outline:
          "text-foreground [a&]:hover:bg-accent [a&]:hover:text-accent-foreground",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  },
);

type BadgeProps = React.ComponentProps<"span"> &
  VariantProps<typeof badgeVariants> & {
    asChild?: boolean;
    tooltip?: React.ReactNode;
    size?: "sm" | "md";
    isLoading?: boolean;
  };

export function Badge({
  className,
  variant,
  size = "md",
  asChild = false,
  tooltip,
  isLoading = false,
  ...props
}: BadgeProps) {
  const Comp = asChild ? Slot : "span";

  const sizeClass = {
    sm: `text-sm px-1 rounded-sm h-${6}`,
    md: `text-sm px-2 rounded-md h-${7}`,
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
      <Tooltip>
        <TooltipTrigger asChild>{base}</TooltipTrigger>
        <TooltipContent>{tooltip}</TooltipContent>
      </Tooltip>
    );
  }

  return base;
}

// Combines two badges into one. Requires two children, the first is the left part and the second is the right part
export function TwoPartBadge({
  children,
}: BadgeProps & {
  children: [React.ReactNode, React.ReactNode];
}) {
  return (
    <div className="flex items-center">
      <span className="mr-0! [&_[data-slot=badge]]:rounded-r-none">
        {children[0]}
      </span>
      <span className="ml-0! [&_[data-slot=badge]]:rounded-l-none">
        {children[1]}
      </span>
    </div>
  );
}
