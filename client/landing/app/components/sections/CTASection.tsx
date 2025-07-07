"use client";

import { ReactNode } from "react";
import { Button } from "../Button";

interface CTAButton {
  text: string;
  href: string;
  variant?: "rainbow-light" | "primary-dark" | "outline" | "primary";
  size?: "default" | "chunky";
}

interface CTASectionProps {
  title: string;
  description?: string;
  buttons: CTAButton[];
  background?: "white" | "black" | "neutral";
  titleColor?: "white" | "black";
  maxWidth?: string;
  children?: ReactNode; // For custom content like community badge
}

export default function CTASection({
  title,
  description,
  buttons,
  background = "white",
  titleColor = "black", 
  maxWidth = "max-w-4xl",
  children,
}: CTASectionProps) {
  const backgroundClasses = {
    white: "bg-white",
    black: "bg-black", 
    neutral: "bg-neutral-100",
  };

  const titleColorClasses = {
    white: "text-white",
    black: "text-neutral-900",
  };

  const descriptionColorClasses = {
    white: "text-neutral-200",
    black: "text-neutral-600",
  };

  return (
    <section className={`w-full py-28 sm:py-36 lg:py-44 ${backgroundClasses[background]} relative overflow-hidden`}>
      <div className="container mx-auto px-4 sm:px-6 lg:px-8">
        <div className="flex flex-col items-center text-center">
          {children}
          
          <h2 className={`font-display font-light text-display-sm sm:text-display-md lg:text-display-lg mb-8 sm:mb-12 ${maxWidth} mx-auto ${titleColorClasses[titleColor]}`}>
            {title}
          </h2>
          
          {description && (
            <p className={`text-base sm:text-lg mb-8 sm:mb-12 ${maxWidth} mx-auto ${descriptionColorClasses[titleColor]}`}>
              {description}
            </p>
          )}

          <div className="flex flex-col sm:flex-row gap-4 items-center">
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
    </section>
  );
}