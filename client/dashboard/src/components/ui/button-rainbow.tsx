import { cn } from "@/lib/utils";
import { useState } from "react";

export const ButtonRainbow = ({
  children,
  href,
  onClick,
  className,
}: {
  children: React.ReactNode;
  href?: string;
  onClick?: () => Promise<void>;
  className?: string;
}) => {
  const Comp = href ? "a" : "button";

  const [inProgress, setInProgress] = useState(false);

  return (
    <div
      className={cn(
        "inline-block rounded-md p-[1px]",
        "bg-gradient-primary",
        inProgress && "opacity-50",
        className,
      )}
    >
      <Comp
        href={href}
        target={href && href.startsWith("http") ? "_blank" : undefined}
        rel="noopener noreferrer"
        onClick={async () => {
          setInProgress(true);
          await onClick?.();
          setInProgress(false);
        }}
        disabled={inProgress}
        className={cn(
          "relative inline-flex items-center justify-center gap-2 px-4 py-2",
          "text-foreground font-mono text-sm uppercase",
          "cursor-pointer rounded-md",
          "transition-all outline-none",
          "bg-background w-full rounded-[7px] border-0",
          "hover:bg-background/95",
          "focus-visible:ring-2 focus-visible:ring-neutral-500 focus-visible:ring-offset-2",
        )}
      >
        {children}
      </Comp>
    </div>
  );
};
