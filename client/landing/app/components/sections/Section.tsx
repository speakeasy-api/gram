"use client";

import { ReactNode } from "react";
import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "../../lib/utils";

const sectionVariants = cva(
  "w-full relative",
  {
    variants: {
      background: {
        white: "bg-white",
        black: "bg-black",
        neutral: "bg-neutral-100",
        transparent: "bg-transparent",
      },
      size: {
        none: "py-0",
        xs: "py-6 sm:py-8 lg:py-12",
        sm: "py-8 sm:py-12 lg:py-16", 
        md: "py-12 sm:py-16 lg:py-20",
        lg: "py-16 sm:py-20 lg:py-24",
        xl: "py-20 sm:py-24 lg:py-28",
        hero: "pt-16 sm:pt-20 lg:pt-24 pb-4 sm:pb-6 lg:pb-8",
      },
      container: {
        true: "",
        false: "",
      },
    },
    defaultVariants: {
      background: "white",
      size: "none",
      container: true,
    },
  }
);

interface SectionProps
  extends React.HTMLAttributes<HTMLElement>,
    VariantProps<typeof sectionVariants> {
  asChild?: boolean;
  children: ReactNode;
}

const Section = ({ 
  className, 
  background, 
  size, 
  container, 
  asChild = false, 
  children, 
  ...props 
}: SectionProps) => {
  const Comp = asChild ? Slot : "section";
  
  const content = container ? (
    <div className="container mx-auto px-4 sm:px-6 lg:px-8">
      {children}
    </div>
  ) : children;

  return (
    <Comp
      className={cn(sectionVariants({ background, size, container }), className)}
      {...props}
    >
      {content}
    </Comp>
  );
};

export { Section, sectionVariants };