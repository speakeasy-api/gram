"use client";

import { ReactNode } from "react";
import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "../../lib/utils";

const flexVariants = cva(
  "flex",
  {
    variants: {
      direction: {
        row: "flex-row",
        "row-reverse": "flex-row-reverse",
        col: "flex-col",
        "col-reverse": "flex-col-reverse",
      },
      align: {
        start: "items-start",
        center: "items-center",
        end: "items-end",
        stretch: "items-stretch",
        baseline: "items-baseline",
      },
      justify: {
        start: "justify-start",
        center: "justify-center",
        end: "justify-end",
        between: "justify-between",
        around: "justify-around",
        evenly: "justify-evenly",
      },
      gap: {
        0: "gap-0",
        1: "gap-1",
        2: "gap-2",
        3: "gap-3",
        4: "gap-4",
        6: "gap-6",
        8: "gap-8",
        12: "gap-12",
        16: "gap-16",
      },
      wrap: {
        nowrap: "flex-nowrap",
        wrap: "flex-wrap",
        "wrap-reverse": "flex-wrap-reverse",
      },
    },
    defaultVariants: {
      direction: "row",
      align: "stretch",
      justify: "start",
      gap: 0,
      wrap: "nowrap",
    },
  }
);

const gridVariants = cva(
  "grid",
  {
    variants: {
      cols: {
        1: "grid-cols-1",
        2: "grid-cols-2", 
        3: "grid-cols-3",
        4: "grid-cols-4",
        6: "grid-cols-6",
        12: "grid-cols-12",
        auto: "grid-cols-[repeat(auto-fit,minmax(300px,1fr))]",
        responsive: "grid-cols-1 md:grid-cols-2 lg:grid-cols-3",
        hero: "grid-cols-1 lg:grid-cols-2",
      },
      gap: {
        0: "gap-0",
        1: "gap-1",
        2: "gap-2",
        3: "gap-3",
        4: "gap-4",
        6: "gap-6",
        8: "gap-8",
        12: "gap-12",
        16: "gap-16",
      },
      align: {
        start: "items-start",
        center: "items-center",
        end: "items-end",
        stretch: "items-stretch",
      },
    },
    defaultVariants: {
      cols: 1,
      gap: 0,
      align: "stretch",
    },
  }
);

interface FlexProps
  extends React.HTMLAttributes<HTMLDivElement>,
    VariantProps<typeof flexVariants> {
  asChild?: boolean;
  children: ReactNode;
}

interface GridProps
  extends React.HTMLAttributes<HTMLDivElement>,
    VariantProps<typeof gridVariants> {
  asChild?: boolean;
  children: ReactNode;
}

const Flex = ({ 
  className, 
  direction, 
  align, 
  justify, 
  gap, 
  wrap, 
  asChild = false, 
  children, 
  ...props 
}: FlexProps) => {
  const Comp = asChild ? Slot : "div";
  
  return (
    <Comp
      className={cn(flexVariants({ direction, align, justify, gap, wrap }), className)}
      {...props}
    >
      {children}
    </Comp>
  );
};

const Grid = ({ 
  className, 
  cols, 
  gap, 
  align, 
  asChild = false, 
  children, 
  ...props 
}: GridProps) => {
  const Comp = asChild ? Slot : "div";
  
  return (
    <Comp
      className={cn(gridVariants({ cols, gap, align }), className)}
      {...props}
    >
      {children}
    </Comp>
  );
};

export { Flex, Grid, flexVariants, gridVariants };