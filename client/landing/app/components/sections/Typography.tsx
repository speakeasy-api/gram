"use client";

import { ReactNode } from "react";
import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "../../lib/utils";

const headingVariants = cva(
  "font-display tracking-tight",
  {
    variants: {
      size: {
        hero: "text-4xl sm:text-5xl md:text-6xl lg:text-6xl xl:text-7xl leading-[1.1]",
        display: "text-3xl sm:text-display-sm md:text-display-md lg:text-display-lg",
        h1: "text-3xl sm:text-4xl lg:text-5xl",
        h2: "text-2xl sm:text-3xl lg:text-4xl", 
        h3: "text-xl sm:text-2xl lg:text-3xl",
        h4: "text-lg sm:text-xl lg:text-2xl",
      },
      weight: {
        light: "font-light",
        normal: "font-normal",
        medium: "font-medium",
        semibold: "font-semibold",
        bold: "font-bold",
        thin: "font-thin",
      },
      color: {
        default: "text-neutral-900",
        white: "text-white",
        muted: "text-neutral-600",
      },
      align: {
        left: "text-left",
        center: "text-center", 
        right: "text-right",
      },
    },
    defaultVariants: {
      size: "display",
      weight: "light",
      color: "default",
      align: "left",
    },
  }
);

const textVariants = cva(
  "",
  {
    variants: {
      size: {
        xs: "text-xs",
        sm: "text-sm",
        base: "text-base",
        lg: "text-lg",
        xl: "text-xl",
        "2xl": "text-2xl",
        description: "text-base sm:text-lg",
        hero: "text-lg md:text-xl lg:text-2xl",
      },
      color: {
        default: "text-neutral-900",
        muted: "text-neutral-600",
        light: "text-neutral-400",
        white: "text-white",
      },
      align: {
        left: "text-left",
        center: "text-center",
        right: "text-right",
      },
      leading: {
        tight: "leading-tight",
        normal: "leading-normal",
        relaxed: "leading-relaxed",
        loose: "leading-loose",
      },
    },
    defaultVariants: {
      size: "base",
      color: "default",
      align: "left",
      leading: "normal",
    },
  }
);

interface HeadingProps
  extends Omit<React.HTMLAttributes<HTMLHeadingElement>, 'color'>,
    VariantProps<typeof headingVariants> {
  asChild?: boolean;
  children: ReactNode;
}

interface TextProps
  extends Omit<React.HTMLAttributes<HTMLParagraphElement>, 'color'>,
    VariantProps<typeof textVariants> {
  asChild?: boolean;
  children: ReactNode;
}

const Heading = ({ 
  className, 
  size, 
  weight, 
  color, 
  align, 
  asChild = false, 
  children, 
  ...props 
}: HeadingProps) => {
  const Comp = asChild ? Slot : "h2";
  
  return (
    <Comp
      className={cn(headingVariants({ size, weight, color, align }), className)}
      {...props}
    >
      {children}
    </Comp>
  );
};

const Text = ({ 
  className, 
  size, 
  color, 
  align, 
  leading, 
  asChild = false, 
  children, 
  ...props 
}: TextProps) => {
  const Comp = asChild ? Slot : "p";
  
  return (
    <Comp
      className={cn(textVariants({ size, color, align, leading }), className)}
      {...props}
    >
      {children}
    </Comp>
  );
};

export { Heading, Text, headingVariants, textVariants };