"use client";

import { ReactNode } from "react";
import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "../../lib/utils";

const containerVariants = cva(
  "mx-auto px-4 sm:px-6 lg:px-8",
  {
    variants: {
      size: {
        full: "container",
        xs: "max-w-xs",
        sm: "max-w-sm", 
        md: "max-w-md",
        lg: "max-w-lg",
        xl: "max-w-xl",
        "2xl": "max-w-2xl",
        "3xl": "max-w-3xl",
        "4xl": "max-w-4xl",
        "5xl": "max-w-5xl",
        "6xl": "max-w-6xl",
        "7xl": "max-w-7xl",
        prose: "max-w-prose",
      },
    },
    defaultVariants: {
      size: "full",
    },
  }
);

interface ContainerProps
  extends React.HTMLAttributes<HTMLDivElement>,
    VariantProps<typeof containerVariants> {
  asChild?: boolean;
  children: ReactNode;
}

const Container = ({ 
  className, 
  size, 
  asChild = false, 
  children, 
  ...props 
}: ContainerProps) => {
  const Comp = asChild ? Slot : "div";
  
  return (
    <Comp
      className={cn(containerVariants({ size }), className)}
      {...props}
    >
      {children}
    </Comp>
  );
};

export { Container, containerVariants };