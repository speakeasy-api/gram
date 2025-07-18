import { cn } from "@/lib/utils";
import { ReactNode } from "react";

interface XYFadeProps {
  children: ReactNode;
  className?: string;
  fadeHeight?: number;
  fadeColor?: string; // CSS custom property name (e.g., "var(--background)", "var(--primary)")
  direction?: "vertical" | "horizontal" | "both";
}

export function XYFade({
  children,
  className,
  fadeHeight = 20,
  fadeColor = "var(--background)",
  direction = "vertical",
}: XYFadeProps) {
  const fadeStyle = {
    height: fadeHeight,
    background: `linear-gradient(to bottom, ${fadeColor}, transparent)`,
  };

  const bottomFadeStyle = {
    height: fadeHeight,
    background: `linear-gradient(to top, ${fadeColor}, transparent)`,
  };

  const horizontalFadeStyle = {
    width: fadeHeight,
    background: `linear-gradient(to right, ${fadeColor}, transparent)`,
  };

  const horizontalBottomFadeStyle = {
    width: fadeHeight,
    background: `linear-gradient(to left, ${fadeColor}, transparent)`,
  };

  return (
    <div className={cn("relative overflow-hidden", className)}>
      {/* Top fade overlay */}
      {direction === "vertical" || direction === "both" ? (
        <div
          className="absolute top-0 left-0 right-0 z-10 pointer-events-none"
          style={fadeStyle}
        />
      ) : null}

      {/* Bottom fade overlay */}
      {direction === "vertical" || direction === "both" ? (
        <div
          className="absolute bottom-0 left-0 right-0 z-10 pointer-events-none"
          style={bottomFadeStyle}
        />
      ) : null}

      {/* Left fade overlay */}
      {direction === "horizontal" || direction === "both" ? (
        <div
          className="absolute top-0 bottom-0 left-0 z-10 pointer-events-none"
          style={horizontalFadeStyle}
        />
      ) : null}

      {/* Right fade overlay */}
      {direction === "horizontal" || direction === "both" ? (
        <div
          className="absolute top-0 bottom-0 right-0 z-10 pointer-events-none"
          style={horizontalBottomFadeStyle}
        />
      ) : null}

      {/* Content - centered */}
      <div className="relative z-0 flex items-center justify-center min-h-full">
        {children}
      </div>
    </div>
  );
}
