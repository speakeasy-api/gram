import { cn } from "@/lib/utils";
import { useState } from "react";

export const ButtonRainbow = ({
  children,
  href,
  onClick,
}: {
  children: React.ReactNode;
  href?: string;
  onClick?: () => Promise<void>;
}) => {
  const Comp = href ? "a" : "button";

  const [inProgress, setInProgress] = useState(false);

  return (
    <div
      className={cn(
        "inline-block rounded-md p-[1px]",
        "bg-gradient-primary",
        inProgress && "opacity-50"
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
          "font-mono text-sm uppercase text-foreground",
          "rounded-md cursor-pointer",
          "transition-all outline-none",
          "w-full rounded-[7px] bg-background border-0",
          "hover:bg-background/95",
          "focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:ring-neutral-500"
        )}
      >
        {children}
      </Comp>
    </div>
  );
};
