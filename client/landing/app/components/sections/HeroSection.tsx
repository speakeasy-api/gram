"use client";

import { ReactNode } from "react";
import { Button } from "../Button";

interface HeroButton {
  text: string;
  href: string;
  variant?: "rainbow-light" | "primary-dark" | "outline" | "primary";
  size?: "default" | "chunky";
}

interface HeroSectionProps {
  badge?: {
    text: string;
    style?: "gradient" | "simple";
  };
  title: string;
  description: string;
  buttons: HeroButton[];
  visual?: ReactNode;
  className?: string;
  onButtonsRef?: (ref: HTMLDivElement | null) => void;
}

export default function HeroSection({
  badge,
  title,
  description,
  buttons,
  visual,
  className = "",
  onButtonsRef,
}: HeroSectionProps) {
  return (
    <div className={`relative min-h-[80vh] flex items-center py-16 lg:py-20 ${className}`}>
      <div className="container mx-auto px-4 sm:px-6 lg:px-8">
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-12 lg:gap-16 items-center">
          {/* Left column - Text content */}
          <div className="flex flex-col gap-6 lg:gap-8 py-8 lg:py-0">
            {badge && (
              <div className="inline-flex items-center">
                <span
                  className={`relative inline-flex items-center px-2 py-1 text-xs font-mono text-neutral-700 uppercase tracking-wider rounded-xs ${
                    badge.style === "gradient" 
                      ? ""
                      : "bg-neutral-100 border border-neutral-200"
                  }`}
                  style={badge.style === "gradient" ? {
                    background:
                      "linear-gradient(white, white) padding-box, linear-gradient(90deg, var(--gradient-brand-primary-colors)) border-box",
                    border: "1px solid transparent",
                  } : undefined}
                >
                  {badge.text}
                </span>
              </div>
            )}

            <div className="space-y-2">
              <h1 className="font-display font-light text-4xl sm:text-5xl md:text-6xl lg:text-6xl xl:text-7xl leading-[1.1] tracking-tight text-neutral-900">
                {title}
              </h1>
            </div>

            <div className="space-y-10 lg:space-y-12">
              <p className="text-neutral-600 text-lg md:text-xl lg:text-2xl leading-[1.6] max-w-xl">
                {description}
              </p>

              <div
                ref={onButtonsRef}
                className="flex flex-col sm:flex-row gap-4"
              >
                {buttons.map((button, index) => (
                  <Button
                    key={index}
                    size={button.size || "chunky"}
                    variant={button.variant || "rainbow-light"}
                    href={button.href}
                  >
                    {button.text}
                  </Button>
                ))}
              </div>
            </div>
          </div>

          {/* Right column - Visual */}
          {visual && (
            <div className="lg:col-start-2 lg:row-start-1 lg:row-span-1 relative h-[400px] md:h-[500px] lg:h-[600px] xl:h-[700px] flex items-center justify-center">
              {visual}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}