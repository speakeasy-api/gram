"use client";

import { ReactNode } from "react";

interface SectionContainerProps {
  children: ReactNode;
  className?: string;
  background?: "white" | "black" | "neutral";
  size?: "default" | "large" | "small";
}

export default function SectionContainer({
  children,
  className = "",
  background = "white",
  size = "default",
}: SectionContainerProps) {
  const backgroundClasses = {
    white: "bg-white",
    black: "bg-black",
    neutral: "bg-neutral-100",
  };

  const sizeClasses = {
    default: "py-24 sm:py-32 lg:py-40",
    large: "py-28 sm:py-36 lg:py-44", 
    small: "py-16 sm:py-20 lg:py-24",
  };

  return (
    <section 
      className={`w-full ${backgroundClasses[background]} ${sizeClasses[size]} relative ${className}`}
    >
      <div className="container mx-auto px-4 sm:px-6 lg:px-8">
        {children}
      </div>
    </section>
  );
}