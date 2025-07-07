"use client";

interface SectionHeaderProps {
  title: string;
  description?: string;
  alignment?: "left" | "center";
  titleSize?: "default" | "large";
  maxWidth?: string;
}

export default function SectionHeader({
  title,
  description,
  alignment = "center",
  titleSize = "default",
  maxWidth = "max-w-2xl",
}: SectionHeaderProps) {
  const alignmentClasses = {
    left: "text-left",
    center: "text-center",
  };

  const titleSizeClasses = {
    default: "text-display-sm sm:text-display-md lg:text-display-lg",
    large: "text-4xl sm:text-5xl md:text-6xl lg:text-6xl xl:text-7xl",
  };

  const containerClasses = alignment === "center" 
    ? `${alignmentClasses[alignment]} mb-12 sm:mb-16`
    : `${alignmentClasses[alignment]} mb-8 sm:mb-12`;

  return (
    <div className={containerClasses}>
      <h2 className={`font-display font-light ${titleSizeClasses[titleSize]} text-neutral-900 mb-4 sm:mb-6`}>
        {title}
      </h2>
      {description && (
        <p className={`text-base sm:text-lg text-neutral-600 ${alignment === "center" ? `${maxWidth} mx-auto` : maxWidth}`}>
          {description}
        </p>
      )}
    </div>
  );
}