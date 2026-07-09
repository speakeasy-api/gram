import { cn } from "@/lib/utils";
import { Tooltip, TooltipTrigger, TooltipContent } from "./tooltip";
import { Skeleton } from "./skeleton";

export function Heading({
  variant,
  children,
  className,
  tooltip,
}: {
  variant: "h1" | "h2" | "h3" | "h4" | "h5" | "h6";
  children: React.ReactNode;
  className?: string;
  tooltip?: string;
}): JSX.Element {
  if (!children) {
    const variantHeight = {
      h1: "h-12",
      h2: "h-10",
      h3: "h-8",
      h4: "h-6",
      h5: "h-4",
      h6: "h-2",
    }[variant];

    return <Skeleton className={cn("w-[200px]", variantHeight)} />;
  }

  let base = null;

  const baseClasses = cn("font-normal capitalize", className);
  // Page and section titles set in the display serif (Tobias Thin), per the
  // Claude Design product reference; h4-h6 stay in the interface sans.
  const displayClasses = cn("font-display font-thin capitalize", className);

  switch (variant) {
    case "h1":
      base = (
        <h1
          className={cn(
            "text-[2.5rem] leading-[0.95] tracking-[-0.02em]",
            displayClasses,
          )}
        >
          {children}
        </h1>
      );
      break;
    case "h2":
      base = (
        <h2
          className={cn(
            "text-3xl leading-[0.98] tracking-[-0.02em]",
            displayClasses,
          )}
        >
          {children}
        </h2>
      );
      break;
    case "h3":
      base = (
        <h3
          className={cn(
            "text-2xl leading-none tracking-[-0.015em]",
            displayClasses,
          )}
        >
          {children}
        </h3>
      );
      break;
    case "h4":
      base = <h4 className={cn("text-lg", baseClasses)}>{children}</h4>;
      break;
    case "h5":
      base = <h5 className={cn("text-base", baseClasses)}>{children}</h5>;
      break;
    case "h6":
      base = <h6 className={cn("text-sm", baseClasses)}>{children}</h6>;
      break;
  }

  if (tooltip) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>{base}</TooltipTrigger>
        <TooltipContent>{tooltip}</TooltipContent>
      </Tooltip>
    );
  }

  return base;
}
