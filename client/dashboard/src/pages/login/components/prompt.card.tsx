"use client";

import type { HTMLAttributes } from "react";
import { cn } from "@/lib/utils";

interface PromptCardProps extends HTMLAttributes<HTMLDivElement> {
  service: string;
  prompt: string;
  borderColor?: string;
}

export function PromptCard({
  service,
  prompt,
  borderColor = "#D1D1D1",
  className,
  ...props
}: PromptCardProps) {
  return (
    <div
      className={cn(
        "relative w-64 flex-shrink-0 rounded-full border border-[#D1D1D1] bg-white px-6 py-2 shadow-sm",
        "transition-all duration-300 ease-in-out",
        className,
      )}
      data-border-color={borderColor}
      {...props}
    >
      {/* Content */}
      <div className="relative z-10 flex items-center gap-2 truncate text-sm md:text-base">
        <span className="font-semibold whitespace-nowrap">@{service}</span>
        <span className="truncate text-gray-700">{prompt}</span>
      </div>
    </div>
  );
}
