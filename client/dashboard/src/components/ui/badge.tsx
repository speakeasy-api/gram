import * as React from "react";
import { Slot } from "@radix-ui/react-slot";
import { cva } from "class-variance-authority";

import { cn } from "@/lib/utils";
import { BadgeVariant, BadgeSize } from "@/components/ui/lib/types";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Tooltip as LocalTooltip,
  TooltipContent as LocalTooltipContent,
  TooltipTrigger as LocalTooltipTrigger,
} from "@/components/ui/tooltip";

const BadgeLeftIcon = React.forwardRef<
  HTMLSpanElement,
  React.HTMLAttributes<HTMLSpanElement>
>(({ className, ...props }, ref) => (
  <span
    ref={ref}
    className={cn(
      "inline-flex shrink-0 items-center justify-center",
      className,
    )}
    {...props}
  />
));
BadgeLeftIcon.displayName = "BadgeLeftIcon";

const BadgeRightIcon = React.forwardRef<
  HTMLSpanElement,
  React.HTMLAttributes<HTMLSpanElement>
>(({ className, ...props }, ref) => (
  <span
    ref={ref}
    className={cn(
      "inline-flex shrink-0 items-center justify-center",
      className,
    )}
    {...props}
  />
));
BadgeRightIcon.displayName = "BadgeRightIcon";

const BadgeIcon = BadgeLeftIcon;

const BadgeText = React.forwardRef<
  HTMLSpanElement,
  React.HTMLAttributes<HTMLSpanElement>
>(({ className, ...props }, ref) => (
  <span
    ref={ref}
    className={cn("flex-1 text-trim-cap", className)}
    {...props}
  />
));
BadgeText.displayName = "BadgeText";

const badgeVariants = cva(
  "inline-flex items-center justify-center whitespace-nowrap select-none font-mono uppercase tracking-[0.08em] rounded-xs border transition-colors",
  {
    variants: {
      variant: {
        neutral: "text-default border-neutral-softest",
        destructive: "text-default-destructive border-destructive-softest",
        information: "text-default-information border-information-softest",
        success: "text-default-success border-success-softest",
        warning: "text-default-warning border-warning-softest",
      },
      size: {
        sm: "h-4 px-0.75 py-0.75 text-2xs leading-none gap-0.75 [&_svg]:size-2.25",
        md: "h-5 px-1 py-1 text-xs leading-none gap-1 [&_svg]:size-3",
        lg: "h-6 px-1 py-1 text-base leading-none gap-1 [&_svg]:size-4",
      },
      background: {
        true: "",
        false: "bg-transparent",
      },
    },
    compoundVariants: [
      {
        variant: "neutral",
        background: true,
        className: "bg-surface-secondary-default",
      },
      {
        variant: "destructive",
        background: true,
        className: "bg-destructive-softest",
      },
      {
        variant: "information",
        background: true,
        className: "bg-information-softest",
      },
      {
        variant: "success",
        background: true,
        className: "bg-success-softest",
      },
      {
        variant: "warning",
        background: true,
        className: "bg-warning-softest",
      },
    ],
    defaultVariants: {
      variant: "neutral",
      background: true,
      size: "md",
    },
  },
);

type Attributes = Omit<React.HTMLAttributes<HTMLSpanElement>, "style">;

export interface BadgeProps extends Attributes {
  asChild?: boolean;
  variant?: BadgeVariant;
  size?: BadgeSize;
  background?: boolean;
  /**
   * Square identifier dot before the label (Claude Design tag treatment).
   * The dot inherits the badge's text color unless --badge-dot-color is
   * set (e.g. `className="[--badge-dot-color:var(--color-lang-go)]"`), so
   * language/identity tags keep ink text with only the dot colored.
   */
  dot?: boolean;
  /** Wraps the badge in a tooltip. */
  tooltip?: React.ReactNode;
  /** Renders a size-matched skeleton instead of the badge. */
  isLoading?: boolean;
  className?: string;
  "aria-label"?: string;
}

const Badge = React.forwardRef<HTMLSpanElement, BadgeProps>(
  (
    {
      variant = "neutral",
      size = "md",
      background = true,
      dot = false,
      tooltip,
      isLoading = false,
      asChild = false,
      className,
      ...props
    },
    ref,
  ) => {
    if (process.env.NODE_ENV === "development" && !asChild) {
      const validateChildren = () => {
        if (!props.children) return;

        const childArray = React.Children.toArray(props.children);
        const hasBadgeText = childArray.some(
          (child) =>
            (typeof child === "string" && child.trim().length > 0) ||
            typeof child === "number" ||
            (React.isValidElement(child) &&
              (child.type === BadgeText ||
                (child.type as { displayName?: string })?.displayName ===
                  "BadgeText")),
        );
        const hasAriaLabel = props["aria-label"];

        if (!hasBadgeText && !hasAriaLabel) {
          console.warn(
            "Badge: Badges should either contain Badge.Text or have an aria-label for accessibility.",
          );
        }

        const invalidChildren = childArray.filter((child) => {
          if (typeof child === "string" || typeof child === "number") {
            return false; // Raw text is OK - we'll auto-wrap it
          }
          if (React.isValidElement(child)) {
            const displayName = (child.type as { displayName?: string })
              ?.displayName;
            return ![
              "BadgeText",
              "BadgeLeftIcon",
              "BadgeRightIcon",
              "BadgeIcon",
            ].includes(displayName || "");
          }
          return true; // Other types are invalid
        });

        if (invalidChildren.length > 0) {
          console.warn(
            "Badge: Only Badge.Text, Badge.LeftIcon, Badge.RightIcon, and raw text should be used as children.",
          );
        }
      };

      validateChildren();
    }

    if (process.env.NODE_ENV === "development") {
      const deprecatedVariants = {
        default: "neutral",
        secondary: "neutral",
        tertiary: "neutral",
        danger: "destructive",
        outline: "neutral",
      };

      if (variant && variant in deprecatedVariants) {
        console.warn(
          `Badge: The variant "${variant}" is deprecated. Please use "${deprecatedVariants[variant as keyof typeof deprecatedVariants]}" instead.`,
        );
      }
    }

    const Comp = asChild ? Slot : "span";

    // Auto-wrap raw text children in Badge.Text (only when not using asChild)
    const processedChildren = React.useMemo(() => {
      if (asChild) {
        // When asChild is true, return children as-is for Slot to handle
        return props.children;
      }

      return React.Children.map(props.children, (child) => {
        if (typeof child === "string" || typeof child === "number") {
          return <BadgeText>{child}</BadgeText>;
        }
        return child;
      });
    }, [props.children, asChild]);

    if (isLoading) {
      const skeletonSize = {
        sm: "h-4 w-16",
        md: "h-5 w-20",
        lg: "h-6 w-24",
      }[size];
      return <Skeleton className={cn(skeletonSize, className)} />;
    }

    const badge = (
      <Comp
        className={cn(badgeVariants({ variant, size, background }), className)}
        ref={ref}
        {...props}
      >
        {dot && !asChild && (
          <span
            aria-hidden
            className={cn(
              "shrink-0 bg-[var(--badge-dot-color,currentColor)]",
              size === "sm" ? "size-1.5" : "size-2",
            )}
          />
        )}
        {processedChildren}
      </Comp>
    );

    if (tooltip) {
      return (
        <LocalTooltip>
          <LocalTooltipTrigger asChild>{badge}</LocalTooltipTrigger>
          <LocalTooltipContent>{tooltip}</LocalTooltipContent>
        </LocalTooltip>
      );
    }

    return badge;
  },
);
Badge.displayName = "Badge";

const BadgeWithCompounds = Object.assign(Badge, {
  Icon: BadgeIcon,
  LeftIcon: BadgeLeftIcon,
  RightIcon: BadgeRightIcon,
  Text: BadgeText,
});

export { BadgeWithCompounds as Badge };
