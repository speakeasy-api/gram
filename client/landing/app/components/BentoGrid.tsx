"use client";

import React, { useState, createContext, useContext } from "react";
import { motion } from "framer-motion";
import { cn } from "../lib/utils";

// Context for passing hover state to animated components
const BentoItemContext = createContext<{
  isHovered: boolean;
  isTapped: boolean;
}>({
  isHovered: false,
  isTapped: false,
});

export function useBentoItemState() {
  return useContext(BentoItemContext);
}

interface BentoGridProps {
  children: React.ReactNode;
  className?: string;
}

export function BentoGrid({ children, className }: BentoGridProps) {
  return (
    <div className={cn("border border-neutral-300", className)}>{children}</div>
  );
}

interface BentoGridRowProps {
  children: React.ReactNode;
  columns?: 2 | 3;
  className?: string;
  isLastRow?: boolean;
}

export function BentoGridRow({
  children,
  columns = 2,
  className,
  isLastRow = false,
}: BentoGridRowProps) {
  const childrenArray = React.Children.toArray(children);

  return (
    <div
      className={cn(
        "grid relative",
        columns === 2
          ? "grid-cols-1 md:grid-cols-2"
          : "grid-cols-1 md:grid-cols-3",
        !isLastRow && "border-b border-neutral-300",
        // Equal height columns
        "grid-rows-1 items-stretch",
        className
      )}
    >
      {childrenArray.map((child, index) => (
        <div
          key={index}
          className={cn(
            "flex",
            // Mobile: add bottom border for all items except last
            index < childrenArray.length - 1 && "border-b border-neutral-300 md:border-b-0",
            // Desktop: add right border for all items except last
            index < childrenArray.length - 1 && "md:border-r md:border-neutral-300"
          )}
        >
          {child}
        </div>
      ))}
    </div>
  );
}

interface BentoGridItemProps {
  title: string;
  description?: string;
  visual?: React.ReactNode;
  className?: string;
  visualSize?: "compact" | "normal";
}

export function BentoGridItem({
  title,
  description,
  visual,
  className,
  visualSize = "normal",
}: BentoGridItemProps) {
  const [isHovered, setIsHovered] = useState(false);
  const [isTapped, setIsTapped] = useState(false);

  // Fixed heights for visual containers to ensure alignment
  const visualHeightClass =
    visualSize === "compact" ? "h-[180px]" : "h-[220px]";

  const handleTap = () => {
    setIsTapped(true);
    // Reset tap state after animation
    setTimeout(() => setIsTapped(false), 2000);
  };

  return (
    <BentoItemContext.Provider
      value={{ isHovered: isHovered || isTapped, isTapped }}
    >
      <motion.div
        className={cn(
          "p-6 lg:p-8 flex flex-col h-full cursor-pointer select-none",
          className
        )}
        onMouseEnter={() => setIsHovered(true)}
        onMouseLeave={() => setIsHovered(false)}
        onTouchStart={handleTap}
        onClick={handleTap}
      >
        {/* Visual container with fixed height for alignment */}
        {visual && (
          <div
            className={cn(
              "w-full flex items-center justify-center mb-6 overflow-hidden",
              visualHeightClass
            )}
          >
            <div 
              className="w-full h-full flex items-center justify-center overflow-hidden relative"
              style={{
                maskImage: `
                  linear-gradient(to right, transparent 0%, black 3%, black 97%, transparent 100%),
                  linear-gradient(to bottom, transparent 0%, black 3%, black 97%, transparent 100%)
                `,
                maskComposite: 'intersect',
                WebkitMaskImage: `
                  linear-gradient(to right, transparent 0%, black 3%, black 97%, transparent 100%),
                  linear-gradient(to bottom, transparent 0%, black 3%, black 97%, transparent 100%)
                `,
                WebkitMaskComposite: 'source-in'
              }}
            >
              {visual}
            </div>
          </div>
        )}

        {/* Text content - aligned across columns */}
        <div className="flex-1 flex flex-col">
          {/* Title - consistent height for alignment */}
          <h3 className="text-heading-xs lg:text-heading-sm mb-3 min-h-[2.5rem] lg:min-h-[3rem]">
            {title}
          </h3>

          {/* Description - now contains all the content */}
          {description && (
            <p className="text-sm lg:text-base text-neutral-600 leading-relaxed">
              {description}
            </p>
          )}
        </div>
      </motion.div>
    </BentoItemContext.Provider>
  );
}
