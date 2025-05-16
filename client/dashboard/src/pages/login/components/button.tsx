/* TO BE MOVED TO MOONSHINE IF YOU WANT TO USE BE PREPARED TO REFACTOR */
"use client";

// TODO: Move this to Moonshine 🥃
import * as React from "react";
import { cn } from "@/lib/utils";
import { Slot } from "@radix-ui/react-slot";
import { cva } from "class-variance-authority";

type ButtonVariant = "primary";
type ButtonSize = "lg";

const shareFocusAndHoverClasses = cn(
  // Focus
  "focus-visible:ring-1 focus-visible:ring-offset-3 focus-visible:ring-[#979797] dark:focus-visible:ring-[#DCDCDC] focus-visible:ring-offset-background",
  // Disabled
  "disabled:opacity-50"
);

const buttonVariants = cva(
  "relative inline-flex cursor-pointer items-center justify-center gap-2.5 rounded-full font-mono text-[15px] leading-[1.6] tracking-[0.01em] whitespace-nowrap uppercase transition-all outline-none select-none disabled:pointer-events-none [&_svg]:pointer-events-none [&_svg]:mb-px [&_svg]:size-4 [&_svg]:shrink-0",
  {
    variants: {
      variant: {
        primary: cn(
          // Base
          "bg-[#2a2a2a] text-white shadow-[0px_2px_1px_0px_#414141_inset,0px_-2px_1px_0px_rgba(0,0,0,0.05)_inset]",
          // Hover
          "hover:bg-black",
          // Active
          "active:bg-[#242424] active:shadow-[0px_5px_1px_0px_#1a1a1a_inset,0px_-2px_1px_0px_rgba(21,21,21,1)_inset]",
          // Focus && Disabled
          shareFocusAndHoverClasses
        ),
      },
      size: {
        lg: "h-10 px-8 py-2",
      },
    },
    defaultVariants: {
      variant: "primary",
      size: "lg",
    },
  }
);

type Attributes = Pick<
  React.ButtonHTMLAttributes<HTMLButtonElement>,
  "disabled" | "onClick" | "type" | "children" | "role"
>;

export interface ButtonProps extends Attributes {
  asChild?: boolean;
  variant?: ButtonVariant;
  size?: ButtonSize;
  className?: string;
  ref?: React.Ref<HTMLButtonElement>;
}

const Button = ({ asChild = false, className, ...props }: ButtonProps) => {
  const Comp = asChild ? Slot : "button";
  return (
    <Comp
      className={cn(
        buttonVariants({ variant: "primary", size: "lg" }),
        className
      )}
      {...props}
    />
  );
};

Button.displayName = "Button";

export { Button as Button };
