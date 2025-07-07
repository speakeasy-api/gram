"use client";

import { ReactNode } from "react";
import { Button } from "../Button";

// Community Badge primitive
interface CommunityBadgeProps {
  text?: string;
  className?: string;
}

export const CommunityBadge = ({
  text = "Join the community",
  className = "",
}: CommunityBadgeProps) => (
  <div className={`inline-flex items-center gap-3 ${className}`}>
    <div className="flex -space-x-2">
      <div className="w-8 h-8 rounded border-2 border-white bg-neutral-300"></div>
      <div className="w-8 h-8 rounded border-2 border-white bg-neutral-400"></div>
      <div className="w-8 h-8 rounded border-2 border-white bg-neutral-500"></div>
    </div>
    <span className="text-sm text-neutral-600">{text}</span>
  </div>
);

// Button Group primitive
interface ButtonConfig {
  text: string;
  href: string;
  variant?: "rainbow-light" | "primary-dark" | "outline" | "primary";
  size?: "default" | "chunky";
}

interface ButtonGroupProps {
  buttons: ButtonConfig[];
  className?: string;
  direction?: "row" | "col";
}

export const ButtonGroup = ({
  buttons,
  className = "",
  direction = "row",
}: ButtonGroupProps) => (
  <div
    className={`flex ${
      direction === "col" ? "flex-col" : "flex-col sm:flex-row"
    } gap-4 ${className}`}
  >
    {buttons.map((button, index) => (
      <Button
        key={index}
        size={button.size}
        variant={button.variant}
        href={button.href}
      >
        {button.text}
      </Button>
    ))}
  </div>
);

// Spacer primitive
interface SpacerProps {
  size?: "xs" | "sm" | "md" | "lg" | "xl";
  className?: string;
}

const spacerSizes = {
  xs: "h-4",
  sm: "h-8",
  md: "h-16",
  lg: "h-24",
  xl: "h-32",
};

export const Spacer = ({ size = "md", className = "" }: SpacerProps) => (
  <div className={`${spacerSizes[size]} ${className}`} />
);

// Badge primitive
interface BadgeProps {
  children: ReactNode;
  variant?: "gradient" | "simple";
  className?: string;
}

export const Badge = ({
  children,
  variant = "simple",
  className = "",
}: BadgeProps) => (
  <div className="inline-flex items-center">
    <span
      className={`relative inline-flex items-center px-2 py-1 text-xs font-mono text-neutral-700 uppercase tracking-wider rounded-xs ${
        variant === "gradient" ? "" : "bg-neutral-100 border border-neutral-200"
      } ${className}`}
      style={
        variant === "gradient"
          ? {
              background:
                "linear-gradient(white, white) padding-box, linear-gradient(90deg, var(--gradient-brand-primary-colors)) border-box",
              border: "1px solid transparent",
            }
          : undefined
      }
    >
      {children}
    </span>
  </div>
);
