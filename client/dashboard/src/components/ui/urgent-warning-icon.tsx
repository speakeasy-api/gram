import { cn } from "@/lib/utils";
import { AlertTriangle } from "lucide-react";

export function UrgentWarningIcon({ className }: { className?: string }) {
  return (
    <AlertTriangle
      className={cn("h-4 w-4 text-orange-700 dark:text-orange-300", className)}
    />
  );
}
