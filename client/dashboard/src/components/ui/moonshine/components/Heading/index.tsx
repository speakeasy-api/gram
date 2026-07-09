import React from "react";
import { cn } from "../../lib/utils";

type HeadingVariant = "xl" | "lg" | "md" | "sm" | "xs";
type HeadingElement = "h1" | "h2" | "h3" | "h4" | "h5" | "h6";

export interface HeadingProps extends React.HTMLAttributes<HTMLHeadingElement> {
  children: React.ReactNode;
  variant?: HeadingVariant;
  as?: HeadingElement;
  className?: string;
  viewTransitionName?: string;
}

const variantStyles: Record<HeadingVariant, string> = {
  xl: "text-heading-xl",
  lg: "text-heading-lg",
  md: "text-heading-md",
  sm: "text-heading-sm",
  xs: "text-heading-xs",
};

const variantToElement: Record<HeadingVariant, HeadingElement> = {
  xl: "h1",
  lg: "h2",
  md: "h3",
  sm: "h4",
  xs: "h5",
};

export const Heading = React.forwardRef<HTMLElement, HeadingProps>(
  function Heading(
    {
      children,
      variant = "md",
      as: Component = variantToElement[variant],
      className,
      viewTransitionName,
      ...props
    }: HeadingProps,
    ref,
  ) {
    return (
      <Component
        {...props}
        ref={ref as React.Ref<HTMLHeadingElement>}
        className={cn(variantStyles[variant], className)}
        style={{ viewTransitionName }}
      >
        {children}
      </Component>
    );
  },
);
