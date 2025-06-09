"use client";

import * as React from "react";
import { cn } from "../lib/utils";
import { cva, type VariantProps } from "class-variance-authority";

const shareFocusAndDisabledClasses = cn(
  // Focus
  "focus-visible:ring-1 focus-visible:ring-offset-3 focus-visible:ring-[#979797] dark:focus-visible:ring-neutral-300 focus-visible:ring-offset-background",
  // Disabled
  "disabled:opacity-50"
);

const rainbowBaseClasses = cn(
  "relative border-0 bg-transparent",
  "before:absolute before:inset-0 before:-z-10 before:rounded-full before:transition-all before:p-[1px] before:content-[''] before:bg-[linear-gradient(90deg,var(--gradient-brand-primary-colors))]",
  "after:absolute after:inset-[1px] after:transition-all after:-z-10 after:rounded-full after:content-['']",
  // Active
  "active:after:inset-[2px] active:before:p-[2px]"
);

const buttonVariants = cva(
  "relative inline-flex z-[1] cursor-pointer items-center justify-center gap-2.5 rounded-full font-mono text-[15px] leading-[1.6] tracking-[0.01em] whitespace-nowrap uppercase transition-all outline-none select-none disabled:pointer-events-none [&_svg]:pointer-events-none [&_svg]:mb-px [&_svg]:size-4 [&_svg]:shrink-0",
  {
    variants: {
      variant: {
        primary: cn(
          // Base
          "bg-[#2a2a2a] text-white shadow-[0px_2px_1px_0px_#414141_inset,0px_-2px_1px_0px_rgba(0,0,0,0.05)_inset]",
          "dark:bg-neutral-100 dark:text-neutral-800 dark:shadow-[0px_2px_1px_0px_#FFF_inset,_0px_-2px_1px_100px_rgba(0,0,0,0.0)_inset,_0px_-2px_1px_0px_rgba(0,0,0,0.1)_inset]",
          // Hover
          "hover:bg-black",
          "dark:hover:shadow-[0px_2px_1px_0px_#F3F3F3_inset,_0px_-40px_10px_10px_rgba(220,220,220,1)_inset,_0px_-2px_1px_0px_rgba(0,0,0,0.05)_inset]",
          // Active
          "active:bg-neutral-900 active:shadow-[0px_5px_1px_0px_#1a1a1a_inset,0px_-2px_1px_0px_rgba(21,21,21,1)_inset]",
          "dark:active:bg-neutral-200 dark:active:shadow-[0px_2px_1px_0px_rgba(0,0,0,0.25)_inset]",
          // Focus && Disabled
          shareFocusAndDisabledClasses
        ),
        "primary-inverted": cn(
          // Base
          "bg-neutral-100 text-neutral-800 shadow-[0px_2px_1px_0px_#FFF_inset,_0px_-2px_1px_100px_rgba(0,0,0,0.0)_inset,_0px_-2px_1px_0px_rgba(0,0,0,0.1)_inset]",
          "dark:bg-[#2a2a2a] dark:text-white dark:shadow-[0px_2px_1px_0px_#414141_inset,0px_-2px_1px_0px_rgba(0,0,0,0.05)_inset]",
          // Hover
          "hover:shadow-[0px_2px_1px_0px_#F3F3F3_inset,_0px_-40px_10px_10px_rgba(220,220,220,1)_inset,_0px_-2px_1px_0px_rgba(0,0,0,0.05)_inset]",
          "dark:hover:bg-neutral-900",
          // Active
          "active:bg-neutral-200 active:shadow-[0px_2px_1px_0px_rgba(0,0,0,0.25)_inset]",
          "dark:active:bg-neutral-900 dark:active:shadow-[0px_5px_1px_0px_#1a1a1a_inset,0px_-2px_1px_0px_rgba(21,21,21,1)_inset]",
          // Focus && Disabled
          shareFocusAndDisabledClasses
        ),
        "primary-light": cn(
          // Base
          "bg-neutral-100 text-neutral-800 shadow-[0px_2px_1px_0px_#FFF_inset,_0px_-2px_1px_100px_rgba(0,0,0,0.0)_inset,_0px_-2px_1px_0px_rgba(0,0,0,0.1)_inset]",
          // Hover
          "hover:shadow-[0px_2px_1px_0px_#F3F3F3_inset,_0px_-40px_10px_10px_rgba(220,220,220,0.2)_inset,_0px_-2px_1px_0px_rgba(0,0,0,0.05)_inset]",
          // Active
          "active:bg-neutral-200 active:shadow-[0px_2px_1px_0px_rgba(0,0,0,0.25)_inset]",
          // Focus && Disabled
          shareFocusAndDisabledClasses
        ),
        "primary-dark": cn(
          // Base
          "bg-[#2a2a2a] text-white shadow-[0px_2px_1px_0px_#414141_inset,0px_-2px_1px_0px_rgba(0,0,0,0.05)_inset]",
          // Hover
          "hover:bg-neutral-900",
          // Active
          "active:bg-neutral-900 active:shadow-[0px_5px_1px_0px_#1a1a1a_inset,0px_-2px_1px_0px_rgba(21,21,21,1)_inset]"
        ),
        rainbow: cn(
          // Base
          rainbowBaseClasses,
          "text-neutral-800 dark:text-neutral-100",
          "after:bg-neutral-100 dark:after:bg-neutral-900",
          // Active
          "active:after:bg-neutral-100 dark:active:after:bg-neutral-900",
          // Hover
          "hover:after:bg-[#F4F4F4] dark:hover:after:bg-[#1a1a1a]",
          // Focus && Disabled
          shareFocusAndDisabledClasses
        ),
        "rainbow-light": cn(
          // Base
          rainbowBaseClasses,
          "text-neutral-800",
          "after:bg-neutral-100",
          // Active
          "active:after:bg-neutral-100",
          // Hover
          "hover:after:bg-[#F4F4F4]",
          // Focus && Disabled
          shareFocusAndDisabledClasses
        ),
        "rainbow-dark": cn(
          // Base
          rainbowBaseClasses,
          "text-neutral-100",
          "after:bg-neutral-900",
          // Active
          "active:after:bg-neutral-900",
          // Hover
          "hover:after:bg-[#1a1a1a]",
          // Focus && Disabled
          shareFocusAndDisabledClasses
        ),
        outline: cn(
          "relative border-0 bg-transparent text-foreground transition-all",
          "before:absolute before:inset-0 before:-z-10 before:rounded-full before:transition-all before:p-px before:content-[''] before:bg-neutral-700",
          "after:absolute after:inset-px after:transition-all after:-z-10 after:rounded-full after:bg-background after:content-['']",
          // Hover
          "hover:after:bg-neutral-200 dark:hover:after:bg-neutral-800",
          // Focus && Disabled
          shareFocusAndDisabledClasses
        ),
      },
      size: {
        default: "h-10 px-5 py-2",
        sm: "h-7.5 px-4",
        lg: "h-10 px-8",
        icon: "h-9 w-9",
        chunky: "h-[52px] px-5",
      },
    },
    defaultVariants: {
      variant: "primary",
      size: "default",
    },
  }
);

type ButtonVariant = NonNullable<
  VariantProps<typeof buttonVariants>["variant"]
>;
type ButtonSize = NonNullable<VariantProps<typeof buttonVariants>["size"]>;

type ButtonBaseProps = {
  variant?: ButtonVariant;
  size?: ButtonSize;
  className?: string;
  href: string;
};

type ButtonProps = ButtonBaseProps &
  Omit<React.AnchorHTMLAttributes<HTMLAnchorElement>, keyof ButtonBaseProps>;

const Button = React.forwardRef<HTMLAnchorElement, ButtonProps>(
  (
    { variant = "primary", size = "default", className, href, ...props },
    ref
  ) => {
    return (
      <a
        ref={ref}
        className={cn(buttonVariants({ variant, size }), className)}
        href={href}
        {...props}
      />
    );
  }
);

Button.displayName = "Button";

export { Button, buttonVariants };
export type { ButtonVariant, ButtonSize };
