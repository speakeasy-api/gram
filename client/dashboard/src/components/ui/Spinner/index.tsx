import { cn } from "@/lib/utils";
import { Loader2 } from "lucide-react";

export function Spinner({ className }: { className?: string }): JSX.Element {
  return <Loader2 className={cn("mr-2 h-4 w-4 animate-spin", className)} />;
}

const RAINBOW_GRADIENT =
  "conic-gradient(from 0deg, #320f1e, #c83228, #fb873f, #d2dc91, #5a8250, #002314, #00143c, #2873d7, #9bc3ff, #320f1e)";

export function RainbowSpinner({
  className,
}: {
  className?: string;
}): JSX.Element {
  return (
    <span
      aria-hidden="true"
      className={cn(
        "inline-block shrink-0 animate-spin rounded-full motion-reduce:animate-none",
        className,
      )}
      style={{
        background: RAINBOW_GRADIENT,
        mask: "linear-gradient(#fff 0 0) content-box, linear-gradient(#fff 0 0)",
        maskComposite: "exclude",
        padding: 2.5,
        WebkitMask:
          "linear-gradient(#fff 0 0) content-box, linear-gradient(#fff 0 0)",
        WebkitMaskComposite: "xor",
      }}
    />
  );
}
