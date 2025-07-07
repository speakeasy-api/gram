"use client";

import { ReactNode, forwardRef } from "react";
import { Button } from "../Button";

interface CommunityButton {
  text: string;
  href: string;
  variant?: "rainbow-light" | "primary-dark" | "outline" | "primary";
}

interface CommunityFooterProps {
  title: string;
  badgeText?: string;
  buttons: CommunityButton[];
  className?: string;
  children?: ReactNode;
}

const CommunityFooter = forwardRef<HTMLElement, CommunityFooterProps>(({
  title,
  badgeText = "Join the community",
  buttons,
  className = "",
  children,
}, ref) => {
  return (
    <footer
      ref={ref}
      className={`relative bg-neutral-100 w-full border-t border-neutral-200 overflow-hidden min-h-[600px] flex flex-col justify-center items-center ${className}`}
    >
      <div className="relative z-20 w-full pointer-events-none">
        <div className="flex flex-col items-center justify-center py-32 sm:py-40 lg:py-48 max-w-2xl mx-auto px-4">
          {/* Community Badge */}
          <div className="inline-flex items-center gap-3 mb-8 sm:mb-12 pointer-events-auto">
            <div className="flex -space-x-2">
              <div className="w-8 h-8 rounded border-2 border-white bg-neutral-300"></div>
              <div className="w-8 h-8 rounded border-2 border-white bg-neutral-400"></div>
              <div className="w-8 h-8 rounded border-2 border-white bg-neutral-500"></div>
            </div>
            <span className="text-sm text-neutral-600">{badgeText}</span>
          </div>

          <h3 className="text-display-sm sm:text-display-md lg:text-display-lg font-display font-light text-neutral-900 mb-10 sm:mb-12 text-center max-w-3xl pointer-events-auto">
            {title}
          </h3>
          
          <div className="flex flex-col md:flex-row gap-4 w-full md:w-auto justify-center pointer-events-auto">
            {buttons.map((button, index) => (
              <Button
                key={index}
                variant={button.variant || "rainbow-light"}
                href={button.href}
              >
                {button.text}
              </Button>
            ))}
          </div>

          {children}
        </div>
      </div>

      <div className="absolute left-0 right-0 bottom-0 h-1 w-full bg-gradient-primary z-20" />
    </footer>
  );
});

CommunityFooter.displayName = "CommunityFooter";

export default CommunityFooter;