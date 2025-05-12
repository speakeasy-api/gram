import { cn } from "@/lib/utils";

export function Dot({ className }: { className?: string }) {
  return (
    <span className={cn("text-muted-foreground/50 self-center", className)}>
      •
    </span>
  );
}
